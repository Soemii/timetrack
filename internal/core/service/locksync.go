package service

import (
	"sort"
	"strconv"
	"time"

	"timetrack/internal/core/domain"
)

// ponytail: bewusst hartkodiert — Config-Knöpfe erst wenn jemand sie braucht.
const (
	autoBreakNote    = "auto:screenlock" // markiert Auto-Pausen, unterscheidet sie von manuellen
	lockMinDuration  = 60 * time.Second  // kürzere Sperren ignorieren
	lockSyncThrottle = 30 * time.Second  // schützt das 2s-Polling der Web-UI
	lockQuerySlack   = 10 * time.Second  // Log-Flush-Verzögerung der Quelle
	lockSyncKey      = "lock_sync_ts"    // Watermark, Unix-Sekunden im Config-Store
)

// SyncLocks erzwingt einen Abgleich — für den In-Prozess-Watcher (Windows serve).
func (s *Service) SyncLocks() { _ = s.syncLockState(true) }

// syncLockState rekonstruiert Bildschirmsperr-Pausen nachträglich aus den
// Lock-Events des OS: gesperrte Intervalle zerschneiden das offene
// work-Segment in work / auto-break / work. Läuft vor jedem Status-Read
// (gedrosselt) und jeder Tracking-Mutation (force). Best effort: Fehler der
// Event-Quelle brechen nie den eigentlichen Aufruf ab.
func (s *Service) syncLockState(force bool) error {
	if s.LockEvents == nil {
		return nil
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return err
	}
	if open == nil {
		return nil // nichts offen → nichts zu rekonstruieren
	}
	now := s.now()
	wm := s.lockWatermark()
	if !force && now.Sub(wm) < lockSyncThrottle {
		return nil
	}
	if open.Kind == domain.KindBreak && open.Note != autoBreakNote {
		return s.setLockWatermark(now) // manuelle Pause: nie anfassen
	}

	since := open.Start
	if wm.After(since) {
		since = wm
	}
	events, err := s.LockEvents(since.Add(-lockQuerySlack))
	if err != nil {
		return nil // best effort; Watermark bleibt, Retry beim nächsten Aufruf
	}
	for i := range events {
		events[i].Time = events[i].Time.Truncate(time.Second)
	}
	sort.Slice(events, func(i, j int) bool { return events[i].Time.Before(events[j].Time) })
	// Idempotenz: open.Start ist selbst High-Water-Mark — alles davor ist
	// bereits in Segmente übersetzt (Slack-Replays sind so harmlos).
	kept := events[:0]
	for _, e := range events {
		if !e.Time.Before(open.Start) {
			kept = append(kept, e)
		}
	}
	events = kept

	newWM := now
	cur := *open
	i := 0
	if cur.Kind == domain.KindBreak { // offener Auto-Break aus früherem Sync
		for i < len(events) && events[i].Locked {
			i++
		}
		if i == len(events) {
			return s.setLockWatermark(now) // weiterhin gesperrt
		}
		u := events[i].Time
		i++
		next, err := s.reopenAs(cur, domain.KindWork, u, "")
		if err != nil {
			return err
		}
		cur = next
	}

	// cur ist jetzt offene Arbeit; Events paarweise (Lock, Unlock) abarbeiten,
	// wiederholte Zustände überspringen.
	for {
		for i < len(events) && !events[i].Locked {
			i++
		}
		if i == len(events) {
			break
		}
		l := events[i].Time
		for i < len(events) && events[i].Locked {
			i++
		}
		if i < len(events) { // Paar (Lock, Unlock)
			u := events[i].Time
			i++
			if u.Sub(l) < lockMinDuration {
				continue // Mikro-Sperre
			}
			br, err := s.reopenAs(cur, domain.KindBreak, l, autoBreakNote)
			if err != nil {
				return err
			}
			next, err := s.reopenAs(br, domain.KindWork, u, "")
			if err != nil {
				return err
			}
			cur = next
			continue
		}
		// Trailing Lock ohne Unlock — Schirm evtl. noch gesperrt.
		if now.Sub(l) < lockMinDuration {
			newWM = l // vertagen: könnte Mikro-Sperre sein, nächster Sync sieht ihn wieder
			break
		}
		if _, err := s.reopenAs(cur, domain.KindBreak, l, autoBreakNote); err != nil {
			return err
		}
		break
	}
	return s.setLockWatermark(newWM)
}

// reopenAs schließt das offene Segment zum Zeitpunkt t und öffnet ein neues
// der Art kind ab t mit denselben Projekten (wie Pause()/Resume()).
// checkOverlap entfällt bewusst: es wird nur der Zeitraum zerschnitten, den
// das offene Segment ohnehin belegte.
func (s *Service) reopenAs(open domain.Segment, kind domain.Kind, t time.Time, note string) (domain.Segment, error) {
	if err := s.closeOrDrop(&open, t); err != nil {
		return domain.Segment{}, err
	}
	next := domain.Segment{Kind: kind, ProjectIDs: open.ProjectIDs, Start: t, Open: true, Note: note}
	id, err := s.repo.CreateEntry(next)
	if err != nil {
		return domain.Segment{}, err
	}
	next.ID = id
	return next, nil
}

func (s *Service) lockWatermark() time.Time {
	v, err := s.repo.GetConfig(lockSyncKey)
	if err != nil || v == "" {
		return time.Time{}
	}
	sec, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

func (s *Service) setLockWatermark(t time.Time) error {
	return s.repo.SetConfig(lockSyncKey, strconv.FormatInt(t.Unix(), 10))
}
