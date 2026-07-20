package domain

import (
	"testing"
	"time"
)

func TestEasterSunday(t *testing.T) {
	cases := map[int]Date{
		2024: {2024, time.March, 31},
		2025: {2025, time.April, 20},
		2026: {2026, time.April, 5},
	}
	for year, want := range cases {
		if got := EasterSunday(year); got != want {
			t.Errorf("EasterSunday(%d) = %s, want %s", year, got, want)
		}
	}
}

func TestHolidays(t *testing.T) {
	type check struct {
		year       int
		land       Bundesland
		augsburg   bool
		katholisch bool
		date       Date
		name       string // "" = darf NICHT enthalten sein
	}
	cases := []check{
		{2024, BY, false, false, Date{2024, time.May, 30}, "Fronleichnam"},
		{2025, NW, false, false, Date{2025, time.June, 19}, "Fronleichnam"},
		{2024, SN, false, false, Date{2024, time.November, 20}, "Buß- und Bettag"},
		{2025, SN, false, false, Date{2025, time.November, 19}, "Buß- und Bettag"},
		{2026, SN, false, false, Date{2026, time.November, 18}, "Buß- und Bettag"},
		{2026, BY, true, false, Date{2026, time.August, 8}, "Augsburger Friedensfest"},
		{2026, BY, false, false, Date{2026, time.August, 8}, ""},
		{2026, BY, false, true, Date{2026, time.August, 15}, "Mariä Himmelfahrt"},
		{2026, BY, false, false, Date{2026, time.August, 15}, ""},
		{2026, SL, false, false, Date{2026, time.August, 15}, "Mariä Himmelfahrt"},
		{2026, SN, false, false, Date{2026, time.October, 31}, "Reformationstag"},
		{2026, BY, false, false, Date{2026, time.October, 31}, ""},
		{2026, BE, false, false, Date{2026, time.March, 8}, "Internationaler Frauentag"},
		{2026, BB, false, false, Date{2026, time.April, 5}, "Ostersonntag"},
		{2026, HE, false, false, Date{2026, time.April, 5}, ""},
		{2026, TH, false, false, Date{2026, time.September, 20}, "Weltkindertag"},
		{2026, NW, false, false, Date{2026, time.November, 1}, "Allerheiligen"},
		{2026, ST, false, false, Date{2026, time.January, 6}, "Heilige Drei Könige"},
		{2026, NW, false, false, Date{2026, time.January, 6}, ""},
	}
	for _, c := range cases {
		h := Holidays(c.year, c.land, c.augsburg, c.katholisch)
		got := h[c.date]
		if got != c.name {
			t.Errorf("Holidays(%d, %s, aug=%v, kath=%v)[%s] = %q, want %q",
				c.year, c.land, c.augsburg, c.katholisch, c.date, got, c.name)
		}
	}
}

func TestHolidaysNationwideCount(t *testing.T) {
	// Jedes Land hat mindestens die 9 bundesweiten Feiertage.
	for land := range BundeslandNames {
		h := Holidays(2026, land, false, false)
		if len(h) < 9 {
			t.Errorf("%s: nur %d Feiertage, erwartet >= 9", land, len(h))
		}
	}
}
