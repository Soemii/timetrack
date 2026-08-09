package service

import (
	"errors"
	"slices"
	"testing"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/ports"
)

// jiraService: testService + injizierte JIRA-Quelle (Slice/Fehler per Pointer
// austauschbar, Counter zählt Fetches), URL + Token gesetzt.
func jiraService(t *testing.T, issues *[]ports.Issue, fetchErr *error, calls *int) (*Service, *time.Time) {
	t.Helper()
	svc, now := testService(t)
	svc.Issues = func(baseURL, token string) ([]ports.Issue, error) {
		*calls++
		if *fetchErr != nil {
			return nil, *fetchErr
		}
		return slices.Clone(*issues), nil
	}
	if err := svc.SaveJiraSettings("https://jira.example.com", "tok"); err != nil {
		t.Fatal(err)
	}
	return svc, now
}

// jiraProject legt ein Projekt mit JIRA-Key an.
func jiraProject(t *testing.T, svc *Service, name, key string) domain.Project {
	t.Helper()
	p, err := svc.CreateProjectExplicit(name, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		if err := svc.SetProjectJiraKey(p.ID, key); err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func taskByKey(t *testing.T, svc *Service, pid int64, key string) *domain.Task {
	t.Helper()
	tasks, err := svc.Tasks(pid, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tk := range tasks {
		if tk.JiraKey == key {
			return &tk
		}
	}
	return nil
}

func TestJiraSyncBucketsByProjectKey(t *testing.T) {
	issues := []ports.Issue{
		{Key: "ABC-1", ProjectKey: "ABC", Summary: "Login bauen"},
		{Key: "ABC-2", ProjectKey: "ABC", Summary: "Bug fixen"},
		{Key: "XYZ-9", ProjectKey: "XYZ", Summary: "Ohne Mapping"},
	}
	var fetchErr error
	var calls int
	svc, _ := jiraService(t, &issues, &fetchErr, &calls)
	acme := jiraProject(t, svc, "acme", "abc") // Key wird uppercased gespeichert
	other := jiraProject(t, svc, "other", "")

	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	acmeTasks, _ := svc.Tasks(acme.ID, false)
	if len(acmeTasks) != 2 {
		t.Fatalf("erwartet 2 Aufgaben in acme, got %+v", acmeTasks)
	}
	if tk := taskByKey(t, svc, acme.ID, "ABC-1"); tk == nil || tk.Title != "Login bauen" {
		t.Errorf("ABC-1 fehlt oder falsch: %+v", tk)
	}
	if otherTasks, _ := svc.Tasks(other.ID, true); len(otherTasks) != 0 {
		t.Errorf("ungemapptes XYZ-Issue darf nirgends landen: %+v", otherTasks)
	}
}

func TestJiraSyncUpsertNoDupes(t *testing.T) {
	issues := []ports.Issue{{Key: "ABC-1", ProjectKey: "ABC", Summary: "alt"}}
	var fetchErr error
	var calls int
	svc, _ := jiraService(t, &issues, &fetchErr, &calls)
	p := jiraProject(t, svc, "acme", "ABC")

	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	issues = []ports.Issue{{Key: "ABC-1", ProjectKey: "ABC", Summary: "neu"}}
	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	tasks, _ := svc.Tasks(p.ID, true)
	if len(tasks) != 1 || tasks[0].Title != "neu" {
		t.Fatalf("Upsert: erwartet 1 Aufgabe mit Titel 'neu', got %+v", tasks)
	}
}

func TestJiraSyncArchivesGoneAndRevives(t *testing.T) {
	issues := []ports.Issue{
		{Key: "ABC-1", ProjectKey: "ABC", Summary: "eins"},
		{Key: "ABC-2", ProjectKey: "ABC", Summary: "zwei"},
	}
	var fetchErr error
	var calls int
	svc, _ := jiraService(t, &issues, &fetchErr, &calls)
	p := jiraProject(t, svc, "acme", "ABC")

	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	issues = issues[:1] // ABC-2 erledigt/abgegeben
	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	if tk := taskByKey(t, svc, p.ID, "ABC-2"); tk == nil || !tk.Archived {
		t.Fatalf("verschwundenes Issue muss archiviert sein: %+v", tk)
	}
	if tk := taskByKey(t, svc, p.ID, "ABC-1"); tk == nil || tk.Archived {
		t.Fatalf("ABC-1 darf nicht archiviert sein: %+v", tk)
	}
	issues = []ports.Issue{
		{Key: "ABC-1", ProjectKey: "ABC", Summary: "eins"},
		{Key: "ABC-2", ProjectKey: "ABC", Summary: "zwei"},
	}
	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	if tk := taskByKey(t, svc, p.ID, "ABC-2"); tk == nil || tk.Archived {
		t.Fatalf("wieder aufgetauchtes Issue muss reaktiviert sein: %+v", tk)
	}
}

func TestJiraSyncErrorRoundtrip(t *testing.T) {
	var issues []ports.Issue
	var fetchErr error
	var calls int
	svc, _ := jiraService(t, &issues, &fetchErr, &calls)

	fetchErr = errors.New("401 Unauthorized")
	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	set, _ := svc.JiraSettings()
	if set.LastErr != "401 Unauthorized" {
		t.Fatalf("LastErr: %q", set.LastErr)
	}
	fetchErr = nil
	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	set, _ = svc.JiraSettings()
	if set.LastErr != "" {
		t.Fatalf("LastErr muss nach Erfolg leer sein: %q", set.LastErr)
	}
}

func TestJiraSyncThrottle(t *testing.T) {
	var issues []ports.Issue
	var fetchErr error
	var calls int
	svc, now := jiraService(t, &issues, &fetchErr, &calls)

	_ = svc.syncJira(false)
	_ = svc.syncJira(false)
	if calls != 1 {
		t.Fatalf("Throttle: erwartet 1 Fetch, got %d", calls)
	}
	*now = now.Add(6 * time.Minute)
	_ = svc.syncJira(false)
	if calls != 2 {
		t.Fatalf("nach Throttle-Fenster: erwartet 2 Fetches, got %d", calls)
	}
	_ = svc.syncJira(true)
	if calls != 3 {
		t.Fatalf("force muss Throttle umgehen: got %d", calls)
	}
}

func TestJiraSyncOffWithoutConfig(t *testing.T) {
	var calls int
	svc, _ := testService(t)
	svc.Issues = func(baseURL, token string) ([]ports.Issue, error) {
		calls++
		return nil, nil
	}
	if err := svc.syncJira(true); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatalf("ohne URL/Token darf nicht gefetcht werden: %d", calls)
	}
}

func TestJiraSettingsValidation(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.SaveJiraSettings("ftp://kaputt", "x"); !errors.Is(err, ErrConflict) {
		t.Fatalf("ungültiges Schema muss ErrConflict geben: %v", err)
	}
	if err := svc.SaveJiraSettings("https://jira.example.com/", "tok"); err != nil {
		t.Fatal(err)
	}
	set, _ := svc.JiraSettings()
	if set.BaseURL != "https://jira.example.com" || !set.TokenSet {
		t.Fatalf("Settings: %+v", set)
	}
	// Leerer Token bei gesetztem Token = behalten
	if err := svc.SaveJiraSettings("https://jira.example.com", ""); err != nil {
		t.Fatal(err)
	}
	if set, _ = svc.JiraSettings(); !set.TokenSet {
		t.Fatal("leerer Token muss den alten behalten")
	}
	// Leere URL schaltet ab und löscht den Token
	if err := svc.SaveJiraSettings("", ""); err != nil {
		t.Fatal(err)
	}
	if set, _ = svc.JiraSettings(); set.BaseURL != "" || set.TokenSet {
		t.Fatalf("Abschalten muss URL+Token löschen: %+v", set)
	}
}
