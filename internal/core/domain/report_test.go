package domain

import (
	"math"
	"testing"
	"time"
)

func TestBuildDayAndSaldo(t *testing.T) {
	wh := SpreadWeekly(40*time.Hour, moFr, nil)
	mon := Date{2026, time.July, 20}

	// 09:00–12:00 Projekt 1, 12:00–12:10 Pause (reklassifiziert), 12:10–17:00 Projekt 2
	segs := []Segment{
		{Kind: KindWork, ProjectIDs: []int64{1}, Start: ts("2026-07-20 09:00"), End: ts("2026-07-20 12:00")},
		{Kind: KindBreak, ProjectIDs: []int64{1}, Start: ts("2026-07-20 12:00"), End: ts("2026-07-20 12:10")},
		{Kind: KindWork, ProjectIDs: []int64{2}, Start: ts("2026-07-20 12:10"), End: ts("2026-07-20 17:00")},
	}
	byDay := SplitAtMidnights(Effective(segs), berlin)
	day := BuildDay(mon, byDay[mon], nil, "", wh)

	if day.Worked != 8*time.Hour {
		t.Errorf("Worked = %v, want 8h (inkl. reklassifizierter Kurzpause)", day.Worked)
	}
	if day.Diff != 0 {
		t.Errorf("Diff = %v, want 0", day.Diff)
	}
	if day.PerProject[1] != 3*time.Hour+10*time.Minute {
		t.Errorf("Projekt 1 = %v, want 3h10m (Kurzpause dem Projekt zugerechnet)", day.PerProject[1])
	}
	// Pausenwarnung: 8h Arbeit, 0 Min legale Pause
	if len(day.Warnings) != 1 {
		t.Errorf("erwartet Pausenwarnung, got %v", day.Warnings)
	}

	// Urlaubstag: kein Ist, Credit = Soll, Diff 0
	tue := Date{2026, time.July, 21}
	vac := BuildDay(tue, nil, &Absence{Type: AbsenceUrlaub, Fraction: 1}, "", wh)
	if vac.Diff != 0 || vac.Credit != 8*time.Hour {
		t.Errorf("Urlaubstag: Credit %v Diff %v, want 8h/0", vac.Credit, vac.Diff)
	}

	// Leerer Arbeitstag zählt negativ
	wed := Date{2026, time.July, 22}
	empty := BuildDay(wed, nil, nil, "", wh)
	if empty.Diff != -8*time.Hour {
		t.Errorf("leerer Tag: Diff = %v, want -8h", empty.Diff)
	}

	if got := Saldo([]DaySummary{day, vac, empty}); got != -8*time.Hour {
		t.Errorf("Saldo = %v, want -8h", got)
	}
}

func TestProjectBreakdown(t *testing.T) {
	days := []DaySummary{
		{PerProject: map[int64]time.Duration{1: 6 * time.Hour, 2: 2 * time.Hour}},
		{PerProject: map[int64]time.Duration{1: 2 * time.Hour}},
	}
	totals, pct := ProjectBreakdown(days)
	if totals[1] != 8*time.Hour {
		t.Errorf("Projekt 1 total = %v", totals[1])
	}
	if math.Abs(pct[1]-80) > 0.01 || math.Abs(pct[2]-20) > 0.01 {
		t.Errorf("Prozente = %v, want 80/20", pct)
	}
}
