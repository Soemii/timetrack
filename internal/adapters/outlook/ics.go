// Package outlook liest akzeptierte Termine aus einer Outlook-ICS-Abo-URL.
package outlook

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"timetrack/internal/core/ports"
)

// vevent ist ein roh geparster VEVENT-Block.
type vevent struct {
	uid         string
	start, end  time.Time
	hasEnd      bool
	allDay      bool
	summary     string
	cancelled   bool
	transparent bool
	busy        string // X-MICROSOFT-CDO-BUSYSTATUS: FREE/TENTATIVE/BUSY/OOF
	rrule       string
	exdates     map[int64]bool // Unix-Sekunden der ausgenommenen Occurrence-Starts
	recurID     time.Time      // gesetzt = Override einer Serien-Occurrence
	hasRecurID  bool
}

// accepted meldet, ob der Termin als "angenommen" ins Log gehört.
// Veröffentlichtes ICS hat kein PARTSTAT — Busy-Status ist der Proxy.
func (e vevent) accepted() bool {
	if e.allDay || e.cancelled || e.transparent || !e.hasEnd {
		return false
	}
	if e.busy == "FREE" || e.busy == "TENTATIVE" {
		return false
	}
	return e.end.After(e.start)
}

// parseICS liefert Termine mit Ende in (from, to], nach Start sortiert.
func parseICS(data string, from, to time.Time) []ports.Meeting {
	events := parseEvents(unfold(data))

	// Overrides (RECURRENCE-ID) verdrängen die Basis-Occurrence der Serie;
	// der Override selbst läuft als eigenständiges Event durch den Normalpfad.
	overridden := map[string]map[int64]bool{}
	for _, e := range events {
		if e.hasRecurID && e.uid != "" {
			if overridden[e.uid] == nil {
				overridden[e.uid] = map[int64]bool{}
			}
			overridden[e.uid][e.recurID.Unix()] = true
		}
	}

	var out []ports.Meeting
	emit := func(start, end time.Time, summary string) {
		if end.After(from) && !end.After(to) {
			out = append(out, ports.Meeting{Start: start, End: end, Subject: summary})
		}
	}
	for _, e := range events {
		if !e.accepted() {
			continue
		}
		if e.rrule == "" {
			emit(e.start, e.end, e.summary)
			continue
		}
		for _, st := range expandRRule(e.rrule, e.start, to) {
			if e.exdates[st.Unix()] || overridden[e.uid][st.Unix()] {
				continue
			}
			emit(st, st.Add(e.end.Sub(e.start)), e.summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out
}

// unfold hebt RFC-5545-Zeilenfaltung auf (Folgezeile beginnt mit Space/Tab).
func unfold(data string) []string {
	raw := strings.Split(strings.ReplaceAll(data, "\r\n", "\n"), "\n")
	var lines []string
	for _, l := range raw {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(lines) > 0 {
			lines[len(lines)-1] += l[1:]
			continue
		}
		lines = append(lines, l)
	}
	return lines
}

func parseEvents(lines []string) []vevent {
	var events []vevent
	var cur *vevent
	for _, l := range lines {
		switch {
		case l == "BEGIN:VEVENT":
			cur = &vevent{exdates: map[int64]bool{}}
		case l == "END:VEVENT":
			if cur != nil {
				events = append(events, *cur)
				cur = nil
			}
		case cur != nil:
			name, params, value := splitProp(l)
			switch name {
			case "UID":
				cur.uid = value
			case "SUMMARY":
				cur.summary = unescape(value)
			case "STATUS":
				cur.cancelled = value == "CANCELLED"
			case "TRANSP":
				cur.transparent = value == "TRANSPARENT"
			case "X-MICROSOFT-CDO-BUSYSTATUS":
				cur.busy = value
			case "RRULE":
				cur.rrule = value
			case "DTSTART":
				if params["VALUE"] == "DATE" {
					cur.allDay = true
					continue
				}
				if t, ok := parseICSTime(value, params["TZID"]); ok {
					cur.start = t
				}
			case "DTEND":
				if t, ok := parseICSTime(value, params["TZID"]); ok {
					cur.end = t
					cur.hasEnd = true
				}
			case "RECURRENCE-ID":
				if t, ok := parseICSTime(value, params["TZID"]); ok {
					cur.recurID = t
					cur.hasRecurID = true
				}
			case "EXDATE":
				for _, v := range strings.Split(value, ",") {
					if t, ok := parseICSTime(v, params["TZID"]); ok {
						cur.exdates[t.Unix()] = true
					}
				}
			}
		}
	}
	return events
}

// splitProp zerlegt "NAME;P1=V1;P2=V2:wert" in Name, Params, Wert.
// ponytail: Split am ersten ':' — quoted Params mit Doppelpunkt gibt Outlook nicht aus.
func splitProp(line string) (string, map[string]string, string) {
	head, value, ok := strings.Cut(line, ":")
	if !ok {
		return "", nil, ""
	}
	parts := strings.Split(head, ";")
	params := map[string]string{}
	for _, p := range parts[1:] {
		if k, v, ok := strings.Cut(p, "="); ok {
			params[strings.ToUpper(k)] = v
		}
	}
	return strings.ToUpper(parts[0]), params, value
}

func unescape(s string) string {
	r := strings.NewReplacer(`\\`, `\`, `\;`, ";", `\,`, ",", `\n`, "\n", `\N`, "\n")
	return r.Replace(s)
}

// parseICSTime parst 20060102T150405(Z). Ohne Z gilt die TZID; unbekannte
// (Windows-)TZIDs fallen auf die lokale Zone zurück.
// ponytail: kein VTIMEZONE-Parsing — erst wenn der Fallback jemanden beißt.
func parseICSTime(value, tzid string) (time.Time, bool) {
	if t, err := time.Parse("20060102T150405Z", value); err == nil {
		return t, true
	}
	loc := time.Local
	if tzid != "" {
		if l, err := time.LoadLocation(tzid); err == nil {
			loc = l
		}
	}
	if t, err := time.ParseInLocation("20060102T150405", value, loc); err == nil {
		return t, true
	}
	return time.Time{}, false
}

// expandRRule liefert Occurrence-Starts einer Serie bis to (inklusive).
// Nur FREQ=DAILY|WEEKLY mit INTERVAL, BYDAY (ohne Ordinale), UNTIL, COUNT.
// ponytail: MONTHLY/YEARLY/ordinale BYDAY werden verworfen — nachrüsten, wenn
// jemand solche Serien wirklich loggt.
func expandRRule(rule string, dtstart, to time.Time) []time.Time {
	freq, interval, count := "", 1, -1
	until := time.Time{}
	var byday map[time.Weekday]bool
	for _, part := range strings.Split(rule, ";") {
		k, v, _ := strings.Cut(part, "=")
		switch strings.ToUpper(k) {
		case "FREQ":
			freq = strings.ToUpper(v)
		case "INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				interval = n
			}
		case "COUNT":
			if n, err := strconv.Atoi(v); err == nil {
				count = n
			}
		case "UNTIL":
			if t, ok := parseICSTime(v, ""); ok {
				until = t
			} else if t, err := time.Parse("20060102", v); err == nil {
				until = t.Add(24*time.Hour - time.Second)
			}
		case "BYDAY":
			byday = map[time.Weekday]bool{}
			days := map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday,
				"WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}
			for _, d := range strings.Split(v, ",") {
				wd, ok := days[strings.ToUpper(d)]
				if !ok {
					return nil // Ordinal wie "2MO" → Serie nicht unterstützt
				}
				byday[wd] = true
			}
		}
	}
	if freq != "DAILY" && freq != "WEEKLY" {
		return nil
	}

	var out []time.Time
	add := func(t time.Time) bool { // false = Limits erreicht, aufhören
		if !until.IsZero() && t.After(until) {
			return false
		}
		if t.After(to) {
			return false
		}
		out = append(out, t)
		return count < 0 || len(out) < count
	}

	if freq == "DAILY" {
		for t := dtstart; add(t); t = t.AddDate(0, 0, interval) {
		}
		return out
	}
	if len(byday) == 0 {
		for t := dtstart; add(t); t = t.AddDate(0, 0, 7*interval) {
		}
		return out
	}
	// WEEKLY mit BYDAY: Tage der Woche, Wochen im INTERVAL-Raster ab der
	// Startwoche (WKST=MO, Outlook-Default).
	weekStart := dtstart.AddDate(0, 0, -mondayOffset(dtstart.Weekday()))
	for t := dtstart; ; t = t.AddDate(0, 0, 1) {
		if !until.IsZero() && t.After(until) || t.After(to) {
			return out
		}
		if !byday[t.Weekday()] {
			continue
		}
		if (calendarDays(weekStart, t)/7)%interval != 0 {
			continue
		}
		if !add(t) {
			return out
		}
	}
}

func mondayOffset(d time.Weekday) int { return (int(d) + 6) % 7 }

// calendarDays zählt Kalendertage zwischen a und b — DST-fest, weil über
// UTC-Mitternachten gerechnet wird statt über echte Stunden.
func calendarDays(a, b time.Time) int {
	ua := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	ub := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(ub.Sub(ua).Hours() / 24)
}
