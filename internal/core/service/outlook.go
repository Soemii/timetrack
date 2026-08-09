package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/ports"
)

// ponytail: bewusst hartkodiert — Config-Knöpfe erst wenn jemand sie braucht.
const (
	outlookURLKey   = "outlook_ics_url"
	outlookRulesKey = "outlook_rules"      // JSON: [{"contains":"...","project":"..."}]
	outlookSyncKey  = "outlook_sync_ts"    // Watermark: höchstes verarbeitetes Termin-Ende, Unix-Sekunden
	outlookErrKey   = "outlook_sync_error" // letzter Fetch-Fehler, leer = ok
	outlookThrottle = 5 * time.Minute
)

// OutlookRule ordnet Termine per Betreff-Schlüsselwort einem Projekt zu.
type OutlookRule struct {
	Contains string `json:"contains"`
	Project  string `json:"project"`
}

// OutlookSettings ist der Konfigurationsstand für die Web-UI.
type OutlookSettings struct {
	URL     string
	Rules   []OutlookRule
	LastErr string
}

func (s *Service) OutlookSettings() (OutlookSettings, error) {
	url, err := s.repo.GetConfig(outlookURLKey)
	if err != nil {
		return OutlookSettings{}, err
	}
	lastErr, err := s.repo.GetConfig(outlookErrKey)
	if err != nil {
		return OutlookSettings{}, err
	}
	return OutlookSettings{URL: url, Rules: s.outlookRules(), LastErr: lastErr}, nil
}

func (s *Service) SaveOutlookSettings(url string, rules []OutlookRule) error {
	url = strings.TrimSpace(url)
	if url != "" && !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("%w: ICS-URL muss mit http:// oder https:// beginnen", ErrConflict)
	}
	for _, r := range rules {
		if strings.TrimSpace(r.Contains) == "" {
			return fmt.Errorf("%w: Regel ohne Schlüsselwort", ErrConflict)
		}
	}
	js, err := json.Marshal(rules)
	if err != nil {
		return err
	}
	if err := s.repo.SetConfig(outlookURLKey, url); err != nil {
		return err
	}
	return s.repo.SetConfig(outlookRulesKey, string(js))
}

// syncOutlook importiert vergangene Outlook-Termine als geschlossene
// work-Einträge. Idempotenz rein über die Watermark: nur Termine mit
// wm < Ende <= now werden verarbeitet — einmal Verarbeitetes wird nie wieder
// angefasst, vom Nutzer gelöschte/editierte Importe bleiben also unberührt.
// ponytail: Termine, die in Outlook nachträglich (nach ihrem Ende) angelegt
// oder verschoben werden, verpasst das — Lookback-Slack erst wenn's beißt.
func (s *Service) syncOutlook(force bool) error {
	if s.Meetings == nil {
		return nil
	}
	url, err := s.repo.GetConfig(outlookURLKey)
	if err != nil || url == "" {
		return err
	}
	now := s.now()
	if !force && now.Sub(s.lastOutlookFetch) < outlookThrottle {
		return nil
	}
	// Vor dem Fetch stempeln: ein kaputter Feed retried im Throttle-Takt,
	// nicht bei jedem 2s-Poll der Web-UI.
	s.lastOutlookFetch = now

	wm := s.outlookWatermark(now)
	meetings, err := s.Meetings(url, wm, now)
	if err != nil {
		return s.repo.SetConfig(outlookErrKey, err.Error()) // best effort, Watermark unangetastet
	}

	kept := meetings[:0]
	for _, m := range meetings {
		m.Start, m.End = m.Start.Truncate(time.Second), m.End.Truncate(time.Second)
		if m.End.After(wm) && !m.End.After(now) && m.End.After(m.Start) {
			kept = append(kept, m)
		}
	}
	meetings = kept
	sort.Slice(meetings, func(i, j int) bool { return meetings[i].Start.Before(meetings[j].Start) })

	open, err := s.repo.OpenEntry()
	if err != nil {
		return err
	}
	newWM := now
	for _, m := range meetings {
		// Offenes Segment nie anfassen (idx_one_open, laufendes Tracking):
		// Termin vertagen, Watermark davor deckeln — nächster Sync nach Stop
		// holt ihn nach. Bereits verarbeitete spätere Termine werden dann
		// re-carved: löscht den eigenen Import und legt ihn identisch neu an.
		if open != nil && m.End.After(open.Start) {
			if capped := m.End.Add(-time.Second); capped.Before(newWM) {
				newWM = capped
			}
			continue
		}
		if err := s.carveMeeting(m, now); err != nil {
			return err
		}
	}
	if err := s.repo.SetConfig(outlookSyncKey, strconv.FormatInt(newWM.Unix(), 10)); err != nil {
		return err
	}
	return s.repo.SetConfig(outlookErrKey, "")
}

// carveMeeting schneidet den Termin aus bestehenden geschlossenen Segmenten
// heraus (auch Pausen inkl. auto:screenlock — gesperrter Schirm im Meeting ist
// Meeting-Zeit) und füllt das Loch mit einem work-Eintrag, Titel als Notiz.
// checkOverlap entfällt bewusst (wie reopenAs im Lock-Sync): es wird nur
// belegte Zeit zerschnitten und exakt das gerissene Loch gefüllt.
// ponytail: jeder Repo-Call eigene Tx — Crash mid-carve heilt sich beim
// Re-Sync selbst (Watermark wurde nicht fortgeschrieben, Re-Carve räumt auf).
func (s *Service) carveMeeting(m ports.Meeting, now time.Time) error {
	segs, err := s.repo.EntriesBetween(m.Start, m.End, now)
	if err != nil {
		return err
	}
	for _, e := range segs {
		if e.Open {
			continue // defensiv; per Vertagung in syncOutlook ausgeschlossen
		}
		left := e.Start.Before(m.Start)  // strikt → Reste immer >0 Länge (CHECK end>start)
		right := e.End.After(m.End)
		switch {
		case left && right: // Termin mitten im Segment → splitten
			rest := e
			rest.ID = 0
			rest.Start = m.End
			e.End = m.Start
			if err := s.repo.UpdateEntry(e); err != nil {
				return err
			}
			if _, err := s.repo.CreateEntry(rest); err != nil {
				return err
			}
		case left: // nur das Ende ragt in den Termin → kürzen
			e.End = m.Start
			if err := s.repo.UpdateEntry(e); err != nil {
				return err
			}
		case right: // nur der Anfang ragt in den Termin → verschieben
			e.Start = m.End
			if err := s.repo.UpdateEntry(e); err != nil {
				return err
			}
		default: // komplett überdeckt → weg
			if err := s.repo.DeleteEntry(e.ID); err != nil {
				return err
			}
		}
	}
	pids, err := s.resolveProjects(s.matchRule(m.Subject))
	if err != nil {
		return err
	}
	_, err = s.repo.CreateEntry(domain.Segment{
		Kind: domain.KindWork, ProjectIDs: pids, Start: m.Start, End: m.End, Note: m.Subject,
	})
	return err
}

// matchRule liefert das Projekt der ersten passenden Regel, sonst "".
func (s *Service) matchRule(subject string) string {
	lower := strings.ToLower(subject)
	for _, r := range s.outlookRules() {
		if r.Contains != "" && strings.Contains(lower, strings.ToLower(r.Contains)) {
			return r.Project
		}
	}
	return ""
}

func (s *Service) outlookRules() []OutlookRule {
	v, err := s.repo.GetConfig(outlookRulesKey)
	if err != nil || v == "" {
		return nil
	}
	var rules []OutlookRule
	if json.Unmarshal([]byte(v), &rules) != nil {
		return nil
	}
	return rules
}

// outlookWatermark: leere Watermark = Tagesbeginn heute — beim Einrichten
// nicht die gesamte Kalender-Historie importieren.
func (s *Service) outlookWatermark(now time.Time) time.Time {
	v, err := s.repo.GetConfig(outlookSyncKey)
	if err == nil && v != "" {
		if sec, err := strconv.ParseInt(v, 10, 64); err == nil {
			return time.Unix(sec, 0)
		}
	}
	return domain.DateOf(now, s.loc).Time(s.loc)
}
