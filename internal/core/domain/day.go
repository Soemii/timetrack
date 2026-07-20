package domain

import (
	"fmt"
	"time"
)

// MinLegalBreak: Pausen unter 15 Minuten gelten nach ArbZG §4 nicht als
// Ruhepause und werden der Arbeitszeit zugerechnet.
const MinLegalBreak = 15 * time.Minute

// Effective wendet die §4-Reklassifizierung an: geschlossene Pausen-Segmente
// unter 15 Minuten werden zu Arbeitszeit (Projekt bleibt erhalten).
// Muss VOR SplitAtMidnights laufen, damit eine über Mitternacht laufende
// Pause an ihrer Gesamtdauer gemessen wird.
func Effective(segs []Segment) []Segment {
	out := make([]Segment, len(segs))
	for i, s := range segs {
		if s.Kind == KindBreak && !s.Open && s.Duration() < MinLegalBreak {
			s.Kind = KindWork
		}
		out[i] = s
	}
	return out
}

// SplitAtMidnights schneidet Segmente an lokalen Tagesgrenzen und gruppiert
// sie pro Kalendertag. time.Date normalisiert DST-Wechsel korrekt.
func SplitAtMidnights(segs []Segment, loc *time.Location) map[Date][]Segment {
	byDay := map[Date][]Segment{}
	for _, s := range segs {
		cur := s.Start
		for cur.Before(s.End) {
			d := DateOf(cur, loc)
			dayEnd := d.AddDays(1).Time(loc)
			end := s.End
			if dayEnd.Before(end) {
				end = dayEnd
			}
			part := s
			part.Start = cur
			part.End = end
			byDay[d] = append(byDay[d], part)
			cur = end
		}
	}
	return byDay
}

// DayWork ist die aggregierte Ist-Zeit eines Tages.
type DayWork struct {
	Worked     time.Duration
	Break      time.Duration
	PerProject map[int64]time.Duration // key 0 = ohne Projekt
}

func Aggregate(segs []Segment) DayWork {
	w := DayWork{PerProject: map[int64]time.Duration{}}
	for _, s := range segs {
		switch s.Kind {
		case KindWork:
			w.Worked += s.Duration()
			var pid int64
			if s.ProjectID != nil {
				pid = *s.ProjectID
			}
			w.PerProject[pid] += s.Duration()
		case KindBreak:
			w.Break += s.Duration()
		}
	}
	return w
}

// RequiredBreak liefert die Pflichtpause nach ArbZG §4:
// ab 6 Stunden Arbeit 30 Minuten, über 9 Stunden 45 Minuten.
func RequiredBreak(worked time.Duration) time.Duration {
	switch {
	case worked > 9*time.Hour:
		return 45 * time.Minute
	case worked >= 6*time.Hour:
		return 30 * time.Minute
	default:
		return 0
	}
}

const MaxDailyWork = 10 * time.Hour // ArbZG §3

// DayWarnings prüft ArbZG-Grenzen. Nur Hinweise, blockiert nichts.
func DayWarnings(w DayWork) []string {
	var warns []string
	if req := RequiredBreak(w.Worked); w.Break < req {
		warns = append(warns, fmt.Sprintf(
			"Gesetzliche Pause nicht eingehalten: %d Min. Pause bei %s Arbeit (Pflicht: %d Min.)",
			int(w.Break.Minutes()), FormatHM(w.Worked), int(req.Minutes())))
	}
	if w.Worked > MaxDailyWork {
		warns = append(warns, fmt.Sprintf(
			"Mehr als 10 Stunden gearbeitet (%s) — ArbZG §3 verletzt", FormatHM(w.Worked)))
	}
	return warns
}

// FormatHM formatiert eine Dauer als "H:MM Std.".
func FormatHM(d time.Duration) string {
	neg := ""
	if d < 0 {
		neg = "-"
		d = -d
	}
	d = d.Round(time.Minute)
	return fmt.Sprintf("%s%d:%02d Std.", neg, int(d.Hours()), int(d.Minutes())%60)
}
