package outlook

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

var (
	from = time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	to   = time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)
)

func ics(body string) string {
	return "BEGIN:VCALENDAR\r\n" + body + "END:VCALENDAR\r\n"
}

func event(props string) string {
	return "BEGIN:VEVENT\r\n" + props + "END:VEVENT\r\n"
}

const baseEvent = "UID:e1\r\nDTSTART:20260803T090000Z\r\nDTEND:20260803T100000Z\r\nSUMMARY:Daily Standup\r\n"

func TestParseSimpleEvent(t *testing.T) {
	got := parseICS(ics(event(baseEvent)), from, to)
	if len(got) != 1 {
		t.Fatalf("erwartet 1 Termin, erhalten %d", len(got))
	}
	m := got[0]
	if m.Subject != "Daily Standup" {
		t.Errorf("Subject = %q", m.Subject)
	}
	if !m.Start.Equal(time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)) || !m.End.Equal(time.Date(2026, 8, 3, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("Zeiten falsch: %v–%v", m.Start, m.End)
	}
}

func TestUnfoldingAndUnescaping(t *testing.T) {
	body := "UID:e1\r\nDTSTART:20260803T090000Z\r\nDTEND:20260803T100000Z\r\n" +
		"SUMMARY:Planung\\, Sprint 12\r\n \\; Teil 2\r\n"
	got := parseICS(ics(event(body)), from, to)
	if len(got) != 1 {
		t.Fatalf("erwartet 1 Termin, erhalten %d", len(got))
	}
	if want := "Planung, Sprint 12; Teil 2"; got[0].Subject != want {
		t.Errorf("Subject = %q, erwartet %q", got[0].Subject, want)
	}
}

func TestTZIDParsing(t *testing.T) {
	body := "UID:e1\r\nDTSTART;TZID=Europe/Berlin:20260803T110000\r\nDTEND;TZID=Europe/Berlin:20260803T120000\r\nSUMMARY:x\r\n"
	got := parseICS(ics(event(body)), from, to)
	if len(got) != 1 {
		t.Fatalf("erwartet 1 Termin, erhalten %d", len(got))
	}
	// 11:00 Berlin Sommerzeit = 09:00 UTC
	if !got[0].Start.Equal(time.Date(2026, 8, 3, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("Start = %v", got[0].Start)
	}
}

func TestUnknownTZIDFallsBackToLocal(t *testing.T) {
	body := "UID:e1\r\nDTSTART;TZID=W. Europe Standard Time:20260803T110000\r\nDTEND;TZID=W. Europe Standard Time:20260803T120000\r\nSUMMARY:x\r\n"
	got := parseICS(ics(event(body)), from, to)
	if len(got) != 1 {
		t.Fatalf("erwartet 1 Termin, erhalten %d", len(got))
	}
	want := time.Date(2026, 8, 3, 11, 0, 0, 0, time.Local)
	if !got[0].Start.Equal(want) {
		t.Errorf("Start = %v, erwartet %v (lokal)", got[0].Start, want)
	}
}

func TestFilters(t *testing.T) {
	cases := map[string]string{
		"Ganztag":     "UID:e1\r\nDTSTART;VALUE=DATE:20260803\r\nDTEND;VALUE=DATE:20260804\r\nSUMMARY:x\r\n",
		"Cancelled":   baseEvent + "STATUS:CANCELLED\r\n",
		"Transparent": baseEvent + "TRANSP:TRANSPARENT\r\n",
		"Free":        baseEvent + "X-MICROSOFT-CDO-BUSYSTATUS:FREE\r\n",
		"Tentative":   baseEvent + "X-MICROSOFT-CDO-BUSYSTATUS:TENTATIVE\r\n",
		"OhneEnde":    "UID:e1\r\nDTSTART:20260803T090000Z\r\nSUMMARY:x\r\n",
	}
	for name, body := range cases {
		if got := parseICS(ics(event(body)), from, to); len(got) != 0 {
			t.Errorf("%s: sollte gefiltert werden, erhalten %d Termine", name, len(got))
		}
	}
	// BUSY bleibt drin
	if got := parseICS(ics(event(baseEvent+"X-MICROSOFT-CDO-BUSYSTATUS:BUSY\r\n")), from, to); len(got) != 1 {
		t.Errorf("BUSY: erwartet 1 Termin, erhalten %d", len(got))
	}
}

func TestWindow(t *testing.T) {
	// Ende genau auf from → raus (from exklusiv); Ende genau auf to → drin.
	early := "UID:e1\r\nDTSTART:20260731T230000Z\r\nDTEND:20260801T000000Z\r\nSUMMARY:x\r\n"
	late := "UID:e2\r\nDTSTART:20260830T230000Z\r\nDTEND:20260831T000000Z\r\nSUMMARY:y\r\n"
	after := "UID:e3\r\nDTSTART:20260831T000000Z\r\nDTEND:20260831T010000Z\r\nSUMMARY:z\r\n"
	got := parseICS(ics(event(early)+event(late)+event(after)), from, to)
	if len(got) != 1 || got[0].Subject != "y" {
		t.Fatalf("erwartet nur 'y', erhalten %v", got)
	}
}

func TestRRuleDaily(t *testing.T) {
	body := baseEvent + "RRULE:FREQ=DAILY;COUNT=3\r\n"
	got := parseICS(ics(event(body)), from, to)
	if len(got) != 3 {
		t.Fatalf("erwartet 3 Termine, erhalten %d", len(got))
	}
	if !got[2].Start.Equal(time.Date(2026, 8, 5, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("dritter Start = %v", got[2].Start)
	}
}

func TestRRuleWeeklyByDay(t *testing.T) {
	// Mo 3.8. Start, Mo+Mi, alle 2 Wochen, bis Ende August:
	// Wo 1: 3.8., 5.8.; Wo 3: 17.8., 19.8.; Wo 5: 31.8. (Ende 10:00 > to 31.8. 00:00 → raus)
	body := baseEvent + "RRULE:FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,WE\r\n"
	got := parseICS(ics(event(body)), from, to)
	want := []int{3, 5, 17, 19}
	if len(got) != len(want) {
		t.Fatalf("erwartet %d Termine, erhalten %d: %v", len(want), len(got), got)
	}
	for i, d := range want {
		if got[i].Start.Day() != d {
			t.Errorf("Termin %d: Tag %d, erwartet %d", i, got[i].Start.Day(), d)
		}
	}
}

func TestRRuleUntil(t *testing.T) {
	body := baseEvent + "RRULE:FREQ=DAILY;UNTIL=20260805T090000Z\r\n"
	got := parseICS(ics(event(body)), from, to)
	if len(got) != 3 { // 3.8., 4.8., 5.8.
		t.Fatalf("erwartet 3 Termine, erhalten %d", len(got))
	}
}

func TestRRuleExdate(t *testing.T) {
	body := baseEvent + "RRULE:FREQ=DAILY;COUNT=3\r\nEXDATE:20260804T090000Z\r\n"
	got := parseICS(ics(event(body)), from, to)
	if len(got) != 2 {
		t.Fatalf("erwartet 2 Termine, erhalten %d", len(got))
	}
	for _, m := range got {
		if m.Start.Day() == 4 {
			t.Error("EXDATE-Occurrence nicht entfernt")
		}
	}
}

func TestRecurrenceIDOverride(t *testing.T) {
	series := baseEvent + "RRULE:FREQ=DAILY;COUNT=2\r\n"
	// 4.8. verschoben auf 14:00
	override := "UID:e1\r\nRECURRENCE-ID:20260804T090000Z\r\nDTSTART:20260804T140000Z\r\nDTEND:20260804T150000Z\r\nSUMMARY:Daily Standup (verschoben)\r\n"
	got := parseICS(ics(event(series)+event(override)), from, to)
	if len(got) != 2 {
		t.Fatalf("erwartet 2 Termine, erhalten %d: %v", len(got), got)
	}
	if got[1].Start.Hour() != 14 || got[1].Subject != "Daily Standup (verschoben)" {
		t.Errorf("Override nicht übernommen: %v", got[1])
	}
	// Abgesagte Occurrence: Override mit STATUS:CANCELLED → Occurrence weg, Override gefiltert
	cancelled := "UID:e1\r\nRECURRENCE-ID:20260804T090000Z\r\nDTSTART:20260804T090000Z\r\nDTEND:20260804T100000Z\r\nSUMMARY:x\r\nSTATUS:CANCELLED\r\n"
	got = parseICS(ics(event(series)+event(cancelled)), from, to)
	if len(got) != 1 || got[0].Start.Day() != 3 {
		t.Fatalf("abgesagte Occurrence: erwartet nur 3.8., erhalten %v", got)
	}
}

func TestRRuleMonthlySkipped(t *testing.T) {
	body := baseEvent + "RRULE:FREQ=MONTHLY;BYMONTHDAY=3\r\n"
	if got := parseICS(ics(event(body)), from, to); len(got) != 0 {
		t.Errorf("MONTHLY sollte geskippt werden, erhalten %d", len(got))
	}
	ordinal := baseEvent + "RRULE:FREQ=WEEKLY;BYDAY=2MO\r\n"
	if got := parseICS(ics(event(ordinal)), from, to); len(got) != 0 {
		t.Errorf("ordinales BYDAY sollte geskippt werden, erhalten %d", len(got))
	}
}

func TestFetch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(ics(event(baseEvent))))
	}))
	defer srv.Close()
	got, err := Fetch(srv.URL, from, to)
	if err != nil || len(got) != 1 {
		t.Fatalf("Fetch: %v, %d Termine", err, len(got))
	}

	srv404 := httptest.NewServer(http.NotFoundHandler())
	defer srv404.Close()
	if _, err := Fetch(srv404.URL, from, to); err == nil {
		t.Error("404 sollte Fehler liefern")
	}
}
