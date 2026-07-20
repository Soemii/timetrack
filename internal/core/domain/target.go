package domain

import "time"

// WeekdayHours ist das Tagessoll pro Wochentag, Index = time.Weekday (0 = Sonntag).
type WeekdayHours [7]time.Duration

func (wh WeekdayHours) WeeklyTotal() time.Duration {
	var sum time.Duration
	for _, d := range wh {
		sum += d
	}
	return sum
}

// SpreadWeekly verteilt Wochenstunden gleichmäßig auf die Arbeitstage und
// wendet Abweichungen an. overrides: Wochentag → Stunden (ersetzt den Anteil).
// Die Restsumme (weekly − Summe der Overrides) wird gleichmäßig auf die
// übrigen Arbeitstage verteilt.
func SpreadWeekly(weekly time.Duration, workdays []time.Weekday, overrides map[time.Weekday]time.Duration) WeekdayHours {
	var wh WeekdayHours
	rest := weekly
	var plain []time.Weekday
	for _, wd := range workdays {
		if d, ok := overrides[wd]; ok {
			wh[wd] = d
			rest -= d
		} else {
			plain = append(plain, wd)
		}
	}
	if len(plain) > 0 {
		per := rest / time.Duration(len(plain))
		for _, wd := range plain {
			wh[wd] = per
		}
	}
	return wh
}

func DayTarget(d Date, wh WeekdayHours) time.Duration {
	return wh[d.Weekday()]
}

// DayCredit ist die gutgeschriebene Zeit für Abwesenheit/Feiertag:
// Feiertag oder volle Abwesenheit → volles Tagessoll, halbe → die Hälfte.
func DayCredit(target time.Duration, absence *Absence, isHoliday bool) time.Duration {
	if isHoliday {
		return target
	}
	if absence == nil {
		return 0
	}
	if absence.Fraction == 0.5 {
		return target / 2
	}
	return target
}
