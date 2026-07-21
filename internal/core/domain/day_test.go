package domain

import (
	"testing"
	"time"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func ts(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04", s, berlin)
	if err != nil {
		panic(err)
	}
	return t
}

func seg(kind Kind, start, end string) Segment {
	return Segment{Kind: kind, Start: ts(start), End: ts(end)}
}

func TestEffectiveReclassifiesShortBreaks(t *testing.T) {
	cases := []struct {
		name string
		in   Segment
		want Kind
	}{
		{"14min Pause wird Arbeit", seg(KindBreak, "2026-07-20 12:00", "2026-07-20 12:14"), KindWork},
		{"15min Pause bleibt Pause", seg(KindBreak, "2026-07-20 12:00", "2026-07-20 12:15"), KindBreak},
		{"Arbeit bleibt Arbeit", seg(KindWork, "2026-07-20 12:00", "2026-07-20 12:10"), KindWork},
	}
	for _, c := range cases {
		got := Effective([]Segment{c.in})[0].Kind
		if got != c.want {
			t.Errorf("%s: Kind = %s, want %s", c.name, got, c.want)
		}
	}
	// laufende Kurzpause wird NICHT reklassifiziert
	open := seg(KindBreak, "2026-07-20 12:00", "2026-07-20 12:05")
	open.Open = true
	if got := Effective([]Segment{open})[0].Kind; got != KindBreak {
		t.Errorf("offene Kurzpause reklassifiziert, soll Pause bleiben")
	}
}

func TestEffectiveKeepsProject(t *testing.T) {
	s := seg(KindBreak, "2026-07-20 12:00", "2026-07-20 12:10")
	s.ProjectIDs = []int64{7, 9}
	got := Effective([]Segment{s})[0]
	if got.Kind != KindWork || len(got.ProjectIDs) != 2 || got.ProjectIDs[0] != 7 {
		t.Errorf("Projekte bei Reklassifizierung verloren: %+v", got)
	}
}

func TestAggregateSplitsAcrossProjects(t *testing.T) {
	// 6h auf zwei Projekte → je 3h; 3h auf drei Projekte → je 1h.
	two := seg(KindWork, "2026-07-20 09:00", "2026-07-20 15:00")
	two.ProjectIDs = []int64{1, 2}
	three := seg(KindWork, "2026-07-20 15:00", "2026-07-20 18:00")
	three.ProjectIDs = []int64{1, 2, 3}
	w := Aggregate([]Segment{two, three})
	if w.Worked != 9*time.Hour {
		t.Errorf("Worked = %v, want 9h", w.Worked)
	}
	if w.PerProject[1] != 4*time.Hour || w.PerProject[2] != 4*time.Hour || w.PerProject[3] != 1*time.Hour {
		t.Errorf("Split falsch: %v", w.PerProject)
	}
	var sum time.Duration
	for _, d := range w.PerProject {
		sum += d
	}
	if sum != w.Worked {
		t.Errorf("Anteile (%v) ergeben nicht die Gesamtzeit (%v)", sum, w.Worked)
	}
}

func TestSplitAtMidnights(t *testing.T) {
	s := seg(KindWork, "2026-07-20 22:00", "2026-07-21 02:00")
	byDay := SplitAtMidnights([]Segment{s}, berlin)
	d1 := Date{2026, time.July, 20}
	d2 := Date{2026, time.July, 21}
	if len(byDay) != 2 {
		t.Fatalf("erwartet 2 Tage, got %d", len(byDay))
	}
	if got := byDay[d1][0].Duration(); got != 2*time.Hour {
		t.Errorf("Tag 1: %v, want 2h", got)
	}
	if got := byDay[d2][0].Duration(); got != 2*time.Hour {
		t.Errorf("Tag 2: %v, want 2h", got)
	}
	if byDay[d2][0].Start != ts("2026-07-21 00:00") {
		t.Errorf("Tag 2 startet nicht um Mitternacht: %v", byDay[d2][0].Start)
	}
}

func TestSplitDST(t *testing.T) {
	// Sommerzeitbeginn 29.03.2026: 02:00→03:00, Tag hat 23h.
	spring := seg(KindWork, "2026-03-29 01:00", "2026-03-29 05:00")
	byDay := SplitAtMidnights([]Segment{spring}, berlin)
	if got := byDay[Date{2026, time.March, 29}][0].Duration(); got != 3*time.Hour {
		t.Errorf("DST Frühjahr: %v, want 3h (Uhr springt 02→03)", got)
	}
	// Winterzeitbeginn 25.10.2026: 03:00→02:00, Tag hat 25h.
	fall := seg(KindWork, "2026-10-25 01:00", "2026-10-25 05:00")
	byDay = SplitAtMidnights([]Segment{fall}, berlin)
	if got := byDay[Date{2026, time.October, 25}][0].Duration(); got != 5*time.Hour {
		t.Errorf("DST Herbst: %v, want 5h (Stunde doppelt)", got)
	}
	// Split an Mitternacht des 25h-Tages
	over := seg(KindWork, "2026-10-24 23:00", "2026-10-25 01:00")
	byDay = SplitAtMidnights([]Segment{over}, berlin)
	if got := byDay[Date{2026, time.October, 25}][0].Duration(); got != 1*time.Hour {
		t.Errorf("Mitternacht am DST-Tag: %v, want 1h", got)
	}
}

func TestRequiredBreak(t *testing.T) {
	cases := []struct {
		worked time.Duration
		want   time.Duration
	}{
		{5*time.Hour + 59*time.Minute, 0},
		{6 * time.Hour, 30 * time.Minute},
		{9 * time.Hour, 30 * time.Minute},
		{9*time.Hour + 1*time.Minute, 45 * time.Minute},
	}
	for _, c := range cases {
		if got := RequiredBreak(c.worked); got != c.want {
			t.Errorf("RequiredBreak(%v) = %v, want %v", c.worked, got, c.want)
		}
	}
}

func TestDayWarnings(t *testing.T) {
	// 7h Arbeit, 20 Min Pause → Pausenwarnung
	w := DayWork{Worked: 7 * time.Hour, Break: 20 * time.Minute}
	if warns := DayWarnings(w); len(warns) != 1 {
		t.Errorf("erwartet 1 Warnung, got %v", warns)
	}
	// 10:30h Arbeit, 45 Min Pause → nur §3-Warnung
	w = DayWork{Worked: 10*time.Hour + 30*time.Minute, Break: 45 * time.Minute}
	if warns := DayWarnings(w); len(warns) != 1 {
		t.Errorf("erwartet 1 Warnung (§3), got %v", warns)
	}
	// 5h, keine Pause → keine Warnung
	w = DayWork{Worked: 5 * time.Hour}
	if warns := DayWarnings(w); len(warns) != 0 {
		t.Errorf("erwartet keine Warnung, got %v", warns)
	}
}

func TestFormatHM(t *testing.T) {
	if got := FormatHM(8*time.Hour + 30*time.Minute); got != "8:30 Std." {
		t.Errorf("got %q", got)
	}
	if got := FormatHM(-90 * time.Minute); got != "-1:30 Std." {
		t.Errorf("got %q", got)
	}
}
