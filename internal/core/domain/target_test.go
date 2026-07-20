package domain

import (
	"testing"
	"time"
)

var moFr = []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}

func TestSpreadWeekly(t *testing.T) {
	// 40h, Fr nur 6h → Mo–Do je 8,5h
	wh := SpreadWeekly(40*time.Hour, moFr, map[time.Weekday]time.Duration{time.Friday: 6 * time.Hour})
	if wh[time.Monday] != 8*time.Hour+30*time.Minute {
		t.Errorf("Mo = %v, want 8h30m", wh[time.Monday])
	}
	if wh[time.Friday] != 6*time.Hour {
		t.Errorf("Fr = %v, want 6h", wh[time.Friday])
	}
	if wh[time.Saturday] != 0 || wh[time.Sunday] != 0 {
		t.Errorf("Wochenende muss 0 sein")
	}
	if wh.WeeklyTotal() != 40*time.Hour {
		t.Errorf("Summe = %v, want 40h", wh.WeeklyTotal())
	}
	// ohne Overrides: 40h / 5 = 8h
	wh = SpreadWeekly(40*time.Hour, moFr, nil)
	if wh[time.Wednesday] != 8*time.Hour {
		t.Errorf("Mi = %v, want 8h", wh[time.Wednesday])
	}
}

func TestDayTargetAndCredit(t *testing.T) {
	wh := SpreadWeekly(40*time.Hour, moFr, nil)
	mon := Date{2026, time.July, 20} // Montag
	sat := Date{2026, time.July, 25} // Samstag
	if DayTarget(mon, wh) != 8*time.Hour {
		t.Errorf("Montag-Soll falsch")
	}
	if DayTarget(sat, wh) != 0 {
		t.Errorf("Samstag-Soll muss 0 sein")
	}
	target := 8 * time.Hour
	full := &Absence{Type: AbsenceUrlaub, Fraction: 1.0}
	half := &Absence{Type: AbsenceUrlaub, Fraction: 0.5}
	if DayCredit(target, full, false) != 8*time.Hour {
		t.Errorf("voller Urlaub: Credit != Soll")
	}
	if DayCredit(target, half, false) != 4*time.Hour {
		t.Errorf("halber Urlaub: Credit != Soll/2")
	}
	if DayCredit(target, nil, true) != 8*time.Hour {
		t.Errorf("Feiertag: Credit != Soll")
	}
	// Feiertag am Samstag: Soll 0 → Credit 0
	if DayCredit(0, nil, true) != 0 {
		t.Errorf("Feiertag am Samstag darf nichts gutschreiben")
	}
	if DayCredit(target, nil, false) != 0 {
		t.Errorf("normaler Tag: Credit muss 0 sein")
	}
}
