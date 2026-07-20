package domain

import (
	"slices"
	"time"
)

// EasterSunday berechnet den Ostersonntag nach der Gauß-Formel
// (anonymer gregorianischer Algorithmus).
func EasterSunday(year int) Date {
	a := year % 19
	b := year / 100
	c := year % 100
	d := b / 4
	e := b % 4
	f := (b + 8) / 25
	g := (b - f + 1) / 3
	h := (19*a + b - d - g + 15) % 30
	i := c / 4
	k := c % 4
	l := (32 + 2*e + 2*i - h - k) % 7
	m := (a + 11*h + 22*l) / 451
	month := (h + l - 7*m + 114) / 31
	day := (h+l-7*m+114)%31 + 1
	return Date{year, time.Month(month), day}
}

func in(land Bundesland, lands ...Bundesland) bool {
	return slices.Contains(lands, land)
}

// Holidays liefert alle gesetzlichen Feiertage eines Jahres für ein Bundesland
// als Map Datum → Name. augsburg: Augsburger Friedensfest (nur Stadt Augsburg);
// katholisch: Mariä Himmelfahrt in BY (überwiegend katholische Gemeinden).
func Holidays(year int, land Bundesland, augsburg, katholisch bool) map[Date]string {
	easter := EasterSunday(year)
	h := map[Date]string{}
	add := func(d Date, name string) { h[d] = name }
	fixed := func(m time.Month, day int, name string) { add(Date{year, m, day}, name) }

	// bundesweit
	fixed(time.January, 1, "Neujahr")
	add(easter.AddDays(-2), "Karfreitag")
	add(easter.AddDays(1), "Ostermontag")
	fixed(time.May, 1, "Tag der Arbeit")
	add(easter.AddDays(39), "Christi Himmelfahrt")
	add(easter.AddDays(50), "Pfingstmontag")
	fixed(time.October, 3, "Tag der Deutschen Einheit")
	fixed(time.December, 25, "1. Weihnachtstag")
	fixed(time.December, 26, "2. Weihnachtstag")

	if in(land, BW, BY, ST) {
		fixed(time.January, 6, "Heilige Drei Könige")
	}
	if in(land, BE, MV) {
		fixed(time.March, 8, "Internationaler Frauentag")
	}
	if land == BB {
		add(easter, "Ostersonntag")
		add(easter.AddDays(49), "Pfingstsonntag")
	}
	if in(land, BW, BY, HE, NW, RP, SL) {
		add(easter.AddDays(60), "Fronleichnam")
	}
	if land == BY && augsburg {
		fixed(time.August, 8, "Augsburger Friedensfest")
	}
	if land == SL || (land == BY && katholisch) {
		fixed(time.August, 15, "Mariä Himmelfahrt")
	}
	if land == TH {
		fixed(time.September, 20, "Weltkindertag")
	}
	if in(land, BB, HB, HH, MV, NI, SN, ST, SH, TH) {
		fixed(time.October, 31, "Reformationstag")
	}
	if in(land, BW, BY, NW, RP, SL) {
		fixed(time.November, 1, "Allerheiligen")
	}
	if land == SN {
		// Mittwoch vor dem 23. November
		d := Date{year, time.November, 22}
		for d.Weekday() != time.Wednesday {
			d = d.AddDays(-1)
		}
		add(d, "Buß- und Bettag")
	}
	return h
}
