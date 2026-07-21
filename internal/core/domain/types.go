package domain

import (
	"fmt"
	"time"
)

type Kind string

const (
	KindWork  Kind = "work"
	KindBreak Kind = "break"
)

// Segment ist ein zusammenhängender Zeitblock (Arbeit oder Pause).
// Bei einem offenen Segment injiziert der Aufrufer End = now und setzt Open.
// ProjectIDs: leer = ohne Projekt; mehrere = Zeit wird gleichmäßig aufgeteilt.
type Segment struct {
	ID         int64
	Kind       Kind
	ProjectIDs []int64
	Start      time.Time
	End        time.Time
	Open       bool
	Note       string
}

func (s Segment) Duration() time.Duration { return s.End.Sub(s.Start) }

// Date ist ein Kalendertag ohne Zeitzone.
type Date struct {
	Year  int
	Month time.Month
	Day   int
}

func NewDate(y int, m time.Month, d int) Date { return Date{y, m, d} }

func DateOf(t time.Time, loc *time.Location) Date {
	y, m, d := t.In(loc).Date()
	return Date{y, m, d}
}

func ParseDate(s string) (Date, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return Date{}, fmt.Errorf("ungültiges Datum %q (erwartet JJJJ-MM-TT)", s)
	}
	return Date{t.Year(), t.Month(), t.Day()}, nil
}

func (d Date) String() string { return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day) }

// Time liefert Mitternacht des Tages in loc.
func (d Date) Time(loc *time.Location) time.Time {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, loc)
}

func (d Date) Weekday() time.Weekday {
	return time.Date(d.Year, d.Month, d.Day, 0, 0, 0, 0, time.UTC).Weekday()
}

func (d Date) AddDays(n int) Date {
	t := time.Date(d.Year, d.Month, d.Day, 12, 0, 0, 0, time.UTC).AddDate(0, 0, n)
	return Date{t.Year(), t.Month(), t.Day()}
}

func (d Date) Before(o Date) bool {
	if d.Year != o.Year {
		return d.Year < o.Year
	}
	if d.Month != o.Month {
		return d.Month < o.Month
	}
	return d.Day < o.Day
}

func (d Date) After(o Date) bool { return o.Before(d) }

type Project struct {
	ID       int64
	Name     string
	Archived bool
}

type AbsenceType string

const (
	AbsenceUrlaub   AbsenceType = "urlaub"
	AbsenceKrank    AbsenceType = "krank"
	AbsenceFeiertag AbsenceType = "feiertag"
)

type Absence struct {
	ID       int64
	Date     Date
	Type     AbsenceType
	Fraction float64 // 0.5 oder 1.0
	Note     string
}

type Bundesland string

const (
	BW Bundesland = "BW"
	BY Bundesland = "BY"
	BE Bundesland = "BE"
	BB Bundesland = "BB"
	HB Bundesland = "HB"
	HH Bundesland = "HH"
	HE Bundesland = "HE"
	MV Bundesland = "MV"
	NI Bundesland = "NI"
	NW Bundesland = "NW"
	RP Bundesland = "RP"
	SL Bundesland = "SL"
	SN Bundesland = "SN"
	ST Bundesland = "ST"
	SH Bundesland = "SH"
	TH Bundesland = "TH"
)

var BundeslandNames = map[Bundesland]string{
	BW: "Baden-Württemberg", BY: "Bayern", BE: "Berlin", BB: "Brandenburg",
	HB: "Bremen", HH: "Hamburg", HE: "Hessen", MV: "Mecklenburg-Vorpommern",
	NI: "Niedersachsen", NW: "Nordrhein-Westfalen", RP: "Rheinland-Pfalz",
	SL: "Saarland", SN: "Sachsen", ST: "Sachsen-Anhalt",
	SH: "Schleswig-Holstein", TH: "Thüringen",
}

func ParseBundesland(s string) (Bundesland, error) {
	b := Bundesland(s)
	if _, ok := BundeslandNames[b]; !ok {
		return "", fmt.Errorf("unbekanntes Bundesland %q (Codes: BW BY BE BB HB HH HE MV NI NW RP SL SN ST SH TH)", s)
	}
	return b, nil
}
