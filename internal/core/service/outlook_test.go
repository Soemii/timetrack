package service

import (
	"errors"
	"slices"
	"testing"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/ports"
)

// outlookService: testService + injizierte ICS-Quelle (Slice per Pointer
// austauschbar, Counter zählt Fetches), URL gesetzt.
func outlookService(t *testing.T, meetings *[]ports.Meeting, calls *int) (*Service, *time.Time) {
	t.Helper()
	svc, now := testService(t)
	svc.Meetings = func(url string, from, to time.Time) ([]ports.Meeting, error) {
		*calls++
		return slices.Clone(*meetings), nil
	}
	if err := svc.SaveOutlookSettings("https://example.com/cal.ics", nil); err != nil {
		t.Fatal(err)
	}
	return svc, now
}

func meeting(start time.Time, d time.Duration, subject string) ports.Meeting {
	return ports.Meeting{Start: start, End: start.Add(d), Subject: subject}
}

func TestOutlookImportIntoFreeTime(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	meetings = []ports.Meeting{meeting(at(start, -2*time.Hour), time.Hour, "Sprint Review")}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 1 {
		t.Fatalf("erwartet 1 Segment, got %d: %+v", len(segs), segs)
	}
	m := segs[0]
	if m.Kind != domain.KindWork || m.Open || m.Note != "Sprint Review" ||
		!m.Start.Equal(at(start, -2*time.Hour)) || !m.End.Equal(at(start, -time.Hour)) {
		t.Errorf("Import: %+v", m)
	}
}

func TestOutlookCarveMiddle(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	if _, err := svc.AddEntry(domain.KindWork, "acme", at(start, -4*time.Hour), at(start, -time.Hour), "Tag"); err != nil {
		t.Fatal(err)
	}
	meetings = []ports.Meeting{meeting(at(start, -3*time.Hour), time.Hour, "Standup")}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 3 {
		t.Fatalf("erwartet 3 Segmente, got %d: %+v", len(segs), segs)
	}
	l, m, r := segs[0], segs[1], segs[2]
	if !l.End.Equal(at(start, -3*time.Hour)) || l.Note != "Tag" {
		t.Errorf("linker Rest: %+v", l)
	}
	if m.Note != "Standup" || !m.Start.Equal(at(start, -3*time.Hour)) || !m.End.Equal(at(start, -2*time.Hour)) {
		t.Errorf("Meeting: %+v", m)
	}
	if !r.Start.Equal(at(start, -2*time.Hour)) || !r.End.Equal(at(start, -time.Hour)) || r.Note != "Tag" {
		t.Errorf("rechter Rest: %+v", r)
	}
	// Projektzuordnung überlebt den Split auf beiden Resten.
	if len(l.ProjectIDs) != 1 || len(r.ProjectIDs) != 1 || l.ProjectIDs[0] != r.ProjectIDs[0] {
		t.Errorf("Projekte auf Resten: %v / %v", l.ProjectIDs, r.ProjectIDs)
	}
}

func TestOutlookCarveEdgesAndFullCover(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	// Drei Segmente: 08-09 (Anfang ragt), 09:00-09:30 (komplett überdeckt), 09:30-10:30 (Ende ragt)
	base := at(start, -8*time.Hour) // 01:00 — egal, Hauptsache heute
	for _, span := range [][2]time.Duration{{0, time.Hour}, {time.Hour, 90 * time.Minute}, {90 * time.Minute, 150 * time.Minute}} {
		if _, err := svc.AddEntry(domain.KindWork, "", base.Add(span[0]), base.Add(span[1]), "w"); err != nil {
			t.Fatal(err)
		}
	}
	// Meeting 0:30–2:00 relativ zu base: schneidet in alle drei
	meetings = []ports.Meeting{meeting(base.Add(30*time.Minute), 90*time.Minute, "Workshop")}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 3 {
		t.Fatalf("erwartet 3 Segmente, got %d: %+v", len(segs), segs)
	}
	if !segs[0].End.Equal(base.Add(30*time.Minute)) || segs[0].Note != "w" {
		t.Errorf("gekürzter Anfang: %+v", segs[0])
	}
	if segs[1].Note != "Workshop" {
		t.Errorf("Meeting: %+v", segs[1])
	}
	if !segs[2].Start.Equal(base.Add(2*time.Hour)) || segs[2].Note != "w" {
		t.Errorf("verschobenes Ende: %+v", segs[2])
	}
}

func TestOutlookCarvesBreaks(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	if _, err := svc.AddEntry(domain.KindBreak, "", at(start, -3*time.Hour), at(start, -2*time.Hour), autoBreakNote); err != nil {
		t.Fatal(err)
	}
	meetings = []ports.Meeting{meeting(at(start, -3*time.Hour), time.Hour, "Meeting im Raum")}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 1 || segs[0].Kind != domain.KindWork || segs[0].Note != "Meeting im Raum" {
		t.Fatalf("Break muss dem Meeting weichen: %+v", segs)
	}
}

func TestOutlookRules(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	rules := []OutlookRule{
		{Contains: "sprint", Project: "Scrum"},
		{Contains: "review", Project: "QA"}, // zweite Regel — erste gewinnt
	}
	if err := svc.SaveOutlookSettings("https://example.com/cal.ics", rules); err != nil {
		t.Fatal(err)
	}
	start := *now
	meetings = []ports.Meeting{
		meeting(at(start, -4*time.Hour), time.Hour, "Sprint Review"), // matcht beide → Scrum
		meeting(at(start, -2*time.Hour), time.Hour, "1:1 mit Chef"),  // kein Treffer → ohne Projekt
	}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 2 {
		t.Fatalf("erwartet 2 Segmente: %+v", segs)
	}
	if len(segs[0].ProjectIDs) != 1 {
		t.Fatalf("Regel-Projekt fehlt: %+v", segs[0])
	}
	p, err := svc.repo.GetProject(segs[0].ProjectIDs[0])
	if err != nil || p.Name != "Scrum" {
		t.Errorf("erste Regel muss gewinnen (case-insensitiv, auto-create): %+v", p)
	}
	if len(segs[1].ProjectIDs) != 0 {
		t.Errorf("ohne Treffer kein Projekt: %+v", segs[1])
	}
}

func TestOutlookDefersMeetingOverlappingOpenSegment(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	meetings = []ports.Meeting{meeting(at(start, time.Hour), time.Hour, "Planung")}
	*now = at(start, 3*time.Hour)
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	if segs := segments(t, svc); len(segs) != 1 {
		t.Fatalf("Termin im offenen Segment muss vertagt werden: %+v", segs)
	}
	// Nach Stop holt der nächste Sync ihn nach — carved aus dem geschlossenen Segment.
	*now = at(start, 4*time.Hour)
	if _, err := svc.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 3 || segs[1].Note != "Planung" {
		t.Fatalf("vertagter Termin nicht nachgeholt: %+v", segs)
	}
}

func TestOutlookIdempotent(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	meetings = []ports.Meeting{meeting(at(start, -2*time.Hour), time.Hour, "Daily")}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 1 {
		t.Fatalf("Re-Sync darf nicht duplizieren: %+v", segs)
	}
	// Nutzer löscht den Import → bleibt gelöscht.
	if err := svc.DeleteEntry(segs[0].ID); err != nil {
		t.Fatal(err)
	}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	if segs := segments(t, svc); len(segs) != 0 {
		t.Errorf("gelöschter Import wiederbelebt: %+v", segs)
	}
}

func TestOutlookSkipsFutureAndOldMeetings(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	meetings = []ports.Meeting{
		meeting(at(start, time.Hour), time.Hour, "Zukunft"),           // Ende > now
		meeting(at(start, -24*time.Hour), time.Hour, "Gestern"),       // vor Tagesbeginn (leere Watermark)
		meeting(at(start, -3*time.Hour), time.Hour, "Heute passiert"), // einziger Import
	}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	segs := segments(t, svc)
	if len(segs) != 1 || segs[0].Note != "Heute passiert" {
		t.Fatalf("erwartet nur 'Heute passiert': %+v", segs)
	}
}

func TestOutlookThrottle(t *testing.T) {
	var meetings []ports.Meeting
	var calls int
	svc, now := outlookService(t, &meetings, &calls)
	start := *now
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	*now = at(start, time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("zweiter Status binnen 5min muss gedrosselt sein, calls=%d", calls)
	}
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("SyncOutlookNow muss Drossel umgehen, calls=%d", calls)
	}
	*now = at(start, 6*time.Minute)
	if _, err := svc.Status(); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Errorf("nach 5min muss wieder gefetcht werden, calls=%d", calls)
	}
}

func TestOutlookFetchErrorBestEffort(t *testing.T) {
	svc, _ := testService(t)
	fail := true
	svc.Meetings = func(url string, from, to time.Time) ([]ports.Meeting, error) {
		if fail {
			return nil, errors.New("kaputt")
		}
		return nil, nil
	}
	if err := svc.SaveOutlookSettings("https://example.com/cal.ics", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Status(); err != nil {
		t.Fatal(err) // Fetch-Fehler darf Status nie brechen
	}
	set, err := svc.OutlookSettings()
	if err != nil || set.LastErr != "kaputt" {
		t.Errorf("LastErr = %q, err = %v", set.LastErr, err)
	}
	fail = false
	if err := svc.syncOutlook(true); err != nil {
		t.Fatal(err)
	}
	if set, _ := svc.OutlookSettings(); set.LastErr != "" {
		t.Errorf("Fehler muss bei Erfolg geleert werden: %q", set.LastErr)
	}
}

func TestOutlookSettingsValidation(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.SaveOutlookSettings("ftp://x", nil); !errors.Is(err, ErrConflict) {
		t.Errorf("ungültige URL muss ErrConflict liefern, got %v", err)
	}
	if err := svc.SaveOutlookSettings("", []OutlookRule{{Contains: " ", Project: "x"}}); !errors.Is(err, ErrConflict) {
		t.Errorf("leeres Schlüsselwort muss ErrConflict liefern, got %v", err)
	}
	if err := svc.SaveOutlookSettings("", nil); err != nil {
		t.Errorf("leere URL (deaktiviert) muss ok sein: %v", err)
	}
}
