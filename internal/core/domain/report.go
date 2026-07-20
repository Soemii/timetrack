package domain

import "time"

// DaySummary ist die vollständige Bewertung eines Kalendertages.
type DaySummary struct {
	Date        Date
	Worked      time.Duration
	Break       time.Duration
	Target      time.Duration
	Credit      time.Duration
	Diff        time.Duration // Worked + Credit − Target
	AbsenceType AbsenceType   // "" wenn keine
	Fraction    float64
	HolidayName string
	PerProject  map[int64]time.Duration
	Warnings    []string
}

// BuildDay bewertet einen Tag. segs müssen bereits durch Effective und
// SplitAtMidnights gelaufen sein (nur Segmente dieses Tages).
func BuildDay(date Date, segs []Segment, absence *Absence, holidayName string, wh WeekdayHours) DaySummary {
	w := Aggregate(segs)
	target := DayTarget(date, wh)
	credit := DayCredit(target, absence, holidayName != "")
	s := DaySummary{
		Date:        date,
		Worked:      w.Worked,
		Break:       w.Break,
		Target:      target,
		Credit:      credit,
		Diff:        w.Worked + credit - target,
		HolidayName: holidayName,
		PerProject:  w.PerProject,
		Warnings:    DayWarnings(w),
	}
	if absence != nil {
		s.AbsenceType = absence.Type
		s.Fraction = absence.Fraction
	}
	return s
}

func Saldo(days []DaySummary) time.Duration {
	var sum time.Duration
	for _, d := range days {
		sum += d.Diff
	}
	return sum
}

// ProjectBreakdown liefert Anteil je Projekt in Prozent der Gesamtarbeitszeit.
func ProjectBreakdown(days []DaySummary) (totals map[int64]time.Duration, percent map[int64]float64) {
	totals = map[int64]time.Duration{}
	var all time.Duration
	for _, d := range days {
		for pid, dur := range d.PerProject {
			totals[pid] += dur
			all += dur
		}
	}
	percent = map[int64]float64{}
	if all > 0 {
		for pid, dur := range totals {
			percent[pid] = float64(dur) / float64(all) * 100
		}
	}
	return totals, percent
}
