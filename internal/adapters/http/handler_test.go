package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"timetrack/internal/adapters/sqlite"
	"timetrack/internal/core/domain"
	"timetrack/internal/core/service"
)

var berlin, _ = time.LoadLocation("Europe/Berlin")

func testServer(t *testing.T) (*httptest.Server, *time.Time) {
	t.Helper()
	sqlDB, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	svc := service.New(sqlite.NewRepo(sqlDB), berlin)
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, berlin)
	svc.Now = func() time.Time { return now }
	moFr := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	if err := svc.SaveSettings(service.Settings{
		WeeklyHours:  40,
		WeekdayHours: domain.SpreadWeekly(40*time.Hour, moFr, nil),
		Land:         domain.NW,
		StartDate:    domain.Date{Year: 2026, Month: 7, Day: 20},
	}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(NewServer(svc, berlin))
	t.Cleanup(srv.Close)
	return srv, &now
}

func call(t *testing.T, srv *httptest.Server, method, path string, body any) (*http.Response, []byte) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req, err := http.NewRequest(method, srv.URL+path, &buf)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out bytes.Buffer
	_, _ = out.ReadFrom(resp.Body)
	return resp, out.Bytes()
}

func TestTrackingFlow(t *testing.T) {
	srv, now := testServer(t)

	resp, body := call(t, srv, "POST", "/api/tracking/start", map[string][]string{"projects": {"acme", "intern"}})
	if resp.StatusCode != 204 {
		t.Fatalf("start: %d %s", resp.StatusCode, body)
	}
	// Doppelstart → 409
	resp, _ = call(t, srv, "POST", "/api/tracking/start", nil)
	if resp.StatusCode != 409 {
		t.Errorf("Doppelstart: %d, want 409", resp.StatusCode)
	}

	*now = now.Add(2 * time.Hour)
	resp, _ = call(t, srv, "POST", "/api/tracking/pause", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("pause: %d", resp.StatusCode)
	}
	*now = now.Add(30 * time.Minute)
	resp, _ = call(t, srv, "POST", "/api/tracking/resume", nil)
	if resp.StatusCode != 204 {
		t.Fatalf("resume: %d", resp.StatusCode)
	}
	*now = now.Add(time.Hour)
	resp, body = call(t, srv, "GET", "/api/status", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status: %d", resp.StatusCode)
	}
	var st struct {
		State        string `json:"state"`
		Project      string `json:"project"`
		TodaySummary struct {
			WorkedMinutes int `json:"workedMinutes"`
			BreakMinutes  int `json:"breakMinutes"`
		} `json:"todaySummary"`
	}
	if err := json.Unmarshal(body, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != "working" || st.Project != "acme+intern" {
		t.Errorf("status: %+v", st)
	}
	if st.TodaySummary.WorkedMinutes != 180 || st.TodaySummary.BreakMinutes != 30 {
		t.Errorf("today: %+v", st.TodaySummary)
	}

	resp, body = call(t, srv, "POST", "/api/tracking/stop", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("stop: %d %s", resp.StatusCode, body)
	}
}

func TestEntriesCRUDAndReport(t *testing.T) {
	srv, _ := testServer(t)
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, berlin)

	mk := func(from, to int, project string) int64 {
		resp, body := call(t, srv, "POST", "/api/entries", map[string]any{
			"kind": "work", "projects": []string{project},
			"start": day.Add(time.Duration(from) * time.Hour).Format(time.RFC3339),
			"end":   day.Add(time.Duration(to) * time.Hour).Format(time.RFC3339),
		})
		if resp.StatusCode != 201 {
			t.Fatalf("createEntry: %d %s", resp.StatusCode, body)
		}
		var out struct {
			Id int64 `json:"id"`
		}
		_ = json.Unmarshal(body, &out)
		return out.Id
	}
	id := mk(9, 15, "acme")
	mk(15, 17, "intern")

	// Überlappung → 409
	resp, _ := call(t, srv, "POST", "/api/entries", map[string]any{
		"kind":  "work",
		"start": day.Add(14 * time.Hour).Format(time.RFC3339),
		"end":   day.Add(16 * time.Hour).Format(time.RFC3339),
	})
	if resp.StatusCode != 409 {
		t.Errorf("Überlappung: %d, want 409", resp.StatusCode)
	}

	// Edit: Projekt ändern
	resp, body := call(t, srv, "PUT", fmt.Sprintf("/api/entries/%d", id), map[string]any{"projects": []string{"umbau", "acme"}})
	if resp.StatusCode != 204 {
		t.Fatalf("updateEntry: %d %s", resp.StatusCode, body)
	}

	// Nicht existent → 404
	resp, _ = call(t, srv, "PUT", "/api/entries/999", map[string]any{"projects": []string{"x"}})
	if resp.StatusCode != 404 {
		t.Errorf("update 999: %d, want 404", resp.StatusCode)
	}

	resp, body = call(t, srv, "GET", "/api/entries?from=2026-07-20&to=2026-07-20", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("listEntries: %d", resp.StatusCode)
	}
	var entries []map[string]any
	_ = json.Unmarshal(body, &entries)
	if len(entries) != 2 {
		t.Errorf("erwartet 2 Einträge, got %d", len(entries))
	}

	resp, body = call(t, srv, "GET", "/api/report?from=2026-07-20&to=2026-07-26", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("report: %d", resp.StatusCode)
	}
	var rep struct {
		TotalWorkedMinutes int `json:"totalWorkedMinutes"`
		Projects           []struct {
			Name    string  `json:"name"`
			Percent float32 `json:"percent"`
		} `json:"projects"`
	}
	_ = json.Unmarshal(body, &rep)
	if rep.TotalWorkedMinutes != 8*60 {
		t.Errorf("TotalWorked = %d, want 480", rep.TotalWorkedMinutes)
	}
	// Eintrag 1 (6h) auf umbau+acme gesplittet, Eintrag 2 (2h) intern
	pct := map[string]float32{}
	for _, p := range rep.Projects {
		pct[p.Name] = p.Percent
	}
	if len(rep.Projects) != 3 || pct["umbau"] != 37.5 || pct["acme"] != 37.5 || pct["intern"] != 25 {
		t.Errorf("Projekte: %+v", rep.Projects)
	}
}

func TestAbsencesHolidaysConfig(t *testing.T) {
	srv, _ := testServer(t)

	resp, body := call(t, srv, "POST", "/api/absences", map[string]any{
		"type": "urlaub", "from": "2026-08-03", "to": "2026-08-09",
	})
	if resp.StatusCode != 201 {
		t.Fatalf("createAbsence: %d %s", resp.StatusCode, body)
	}
	var added struct {
		Added []string `json:"added"`
	}
	_ = json.Unmarshal(body, &added)
	if len(added.Added) != 5 {
		t.Errorf("erwartet 5 Urlaubstage (Wochenende übersprungen), got %d", len(added.Added))
	}

	resp, body = call(t, srv, "GET", "/api/holidays?year=2026", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("holidays: %d", resp.StatusCode)
	}
	var hols map[string]string
	_ = json.Unmarshal(body, &hols)
	if hols["2026-06-04"] != "Fronleichnam" { // NW
		t.Errorf("Fronleichnam 2026 fehlt: %v", hols["2026-06-04"])
	}

	resp, body = call(t, srv, "GET", "/api/config", nil)
	if resp.StatusCode != 200 {
		t.Fatalf("getConfig: %d", resp.StatusCode)
	}
	var cfg map[string]any
	_ = json.Unmarshal(body, &cfg)
	if cfg["bundesland"] != "NW" {
		t.Errorf("config: %v", cfg)
	}

	// Config ändern
	resp, body = call(t, srv, "PUT", "/api/config", map[string]any{
		"weeklyHours": 38,
		"weekdayHours": map[string]float64{
			"mon": 8, "tue": 8, "wed": 8, "thu": 8, "fri": 6, "sat": 0, "sun": 0,
		},
		"bundesland": "BY",
		"startDate":  "2026-07-01",
	})
	if resp.StatusCode != 204 {
		t.Fatalf("putConfig: %d %s", resp.StatusCode, body)
	}
	resp, body = call(t, srv, "GET", "/api/config", nil)
	_ = json.Unmarshal(body, &cfg)
	if cfg["bundesland"] != "BY" {
		t.Errorf("config nach PUT: %v", cfg)
	}
}

func TestProjectsCRUD(t *testing.T) {
	srv, _ := testServer(t)

	resp, body := call(t, srv, "POST", "/api/projects", map[string]string{"name": "acme", "color": "#c2183c", "note": "Kundenprojekt"})
	if resp.StatusCode != 201 {
		t.Fatalf("createProject: %d %s", resp.StatusCode, body)
	}
	var created map[string]any
	_ = json.Unmarshal(body, &created)
	id := int64(created["id"].(float64))

	// Duplikat (case-insensitiv) → Konflikt
	resp, body = call(t, srv, "POST", "/api/projects", map[string]string{"name": "ACME"})
	if resp.StatusCode != 409 {
		t.Fatalf("createProject Duplikat: %d %s", resp.StatusCode, body)
	}

	// Meta patchen
	resp, body = call(t, srv, "PUT", fmt.Sprintf("/api/projects/%d", id), map[string]string{"color": "#2e7d4f"})
	if resp.StatusCode != 204 {
		t.Fatalf("updateProject: %d %s", resp.StatusCode, body)
	}

	resp, body = call(t, srv, "GET", "/api/projects", nil)
	var list []map[string]any
	_ = json.Unmarshal(body, &list)
	if len(list) != 1 || list[0]["color"] != "#2e7d4f" || list[0]["note"] != "Kundenprojekt" {
		t.Errorf("listProjects: %d %s", resp.StatusCode, body)
	}
}
