package sqlite

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"timetrack/internal/core/domain"
)

func testRepo(t *testing.T) *Repo {
	t.Helper()
	sqlDB, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewRepo(sqlDB)
}

// TestOpenForeignDB: eine bestehende Datei mit fremdem schema_migrations
// (z.B. von einem anderen Tool) darf unsere Migration nicht verhindern.
func TestOpenForeignDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	pre, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"CREATE TABLE activity_types (id INTEGER PRIMARY KEY, name TEXT)",
		"CREATE TABLE schema_migrations (version uint64, dirty bool)",
		"INSERT INTO schema_migrations VALUES (1, 0)",
	} {
		if _, err := pre.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	pre.Close()

	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("Open auf Fremddatei: %v", err)
	}
	defer sqlDB.Close()
	r := NewRepo(sqlDB)
	if err := r.SetConfig("test", "ok"); err != nil {
		t.Fatalf("config-Tabelle fehlt nach Migration: %v", err)
	}
	// Fremde Tabelle unangetastet
	var n int
	if err := sqlDB.QueryRow("SELECT count(*) FROM activity_types").Scan(&n); err != nil {
		t.Errorf("fremde Tabelle beschädigt: %v", err)
	}
}

// TestMigration0002MovesProjectIDs: Bestandsdaten mit entries.project_id
// (Schema-Stand 0001) landen nach der Migration in entry_projects.
func TestMigration0002MovesProjectIDs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	pre, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	schema, err := os.ReadFile("migrations/0001_init.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pre.Exec(string(schema)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		"CREATE TABLE timetrack_migrations (version uint64, dirty bool)",
		"INSERT INTO timetrack_migrations VALUES (1, 0)",
		"INSERT INTO projects (id, name, created_at) VALUES (1, 'alt', 0)",
		"INSERT INTO entries (id, kind, project_id, start_ts, end_ts) VALUES (1, 'work', 1, 100, 200)",
	} {
		if _, err := pre.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	pre.Close()

	sqlDB, err := Open(path)
	if err != nil {
		t.Fatalf("Open/Migration: %v", err)
	}
	defer sqlDB.Close()
	got, err := NewRepo(sqlDB).GetEntry(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ProjectIDs) != 1 || got.ProjectIDs[0] != 1 {
		t.Errorf("Bestandsprojekt nicht migriert: %v", got.ProjectIDs)
	}
}

func TestEntryRoundtrip(t *testing.T) {
	r := testRepo(t)
	start := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)

	pid, err := r.CreateProject("acme", start)
	if err != nil {
		t.Fatal(err)
	}
	pid2, err := r.CreateProject("intern", start)
	if err != nil {
		t.Fatal(err)
	}
	id, err := r.CreateEntry(domain.Segment{Kind: domain.KindWork, ProjectIDs: []int64{pid, pid2}, Start: start, Open: true})
	if err != nil {
		t.Fatal(err)
	}

	open, err := r.OpenEntry()
	if err != nil || open == nil || open.ID != id || !open.Open {
		t.Fatalf("OpenEntry: %v %+v", err, open)
	}
	if !open.Start.Equal(start) {
		t.Errorf("Start-Roundtrip: %v != %v", open.Start, start)
	}

	// idx_one_open: zweites offenes Entry muss scheitern
	if _, err := r.CreateEntry(domain.Segment{Kind: domain.KindWork, Start: start.Add(time.Hour), Open: true}); err == nil {
		t.Errorf("zweites offenes Segment wurde nicht abgelehnt")
	}

	if err := r.CloseEntry(id, start.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if open, _ := r.OpenEntry(); open != nil {
		t.Errorf("nach Close darf nichts offen sein")
	}

	segs, err := r.EntriesBetween(start.Add(-time.Hour), start.Add(4*time.Hour), start.Add(4*time.Hour))
	if err != nil || len(segs) != 1 {
		t.Fatalf("EntriesBetween: %v, %d Segmente", err, len(segs))
	}
	if len(segs[0].ProjectIDs) != 2 || segs[0].ProjectIDs[0] != pid || segs[0].ProjectIDs[1] != pid2 {
		t.Errorf("Projekt-IDs verloren: %v", segs[0].ProjectIDs)
	}

	// Update: Projektliste ändern + Notiz
	s := segs[0]
	s.Note = "geändert"
	s.ProjectIDs = []int64{pid2}
	if err := r.UpdateEntry(s); err != nil {
		t.Fatal(err)
	}
	got, _ := r.GetEntry(id)
	if got.Note != "geändert" {
		t.Errorf("Update ging verloren")
	}
	if len(got.ProjectIDs) != 1 || got.ProjectIDs[0] != pid2 {
		t.Errorf("Projektliste nach Update: %v", got.ProjectIDs)
	}
	if err := r.DeleteEntry(id); err != nil {
		t.Fatal(err)
	}
	if segs, _ := r.EntriesBetween(start.Add(-time.Hour), start.Add(4*time.Hour), start); len(segs) != 0 {
		t.Errorf("Delete ging verloren")
	}
}

func TestEntriesBetweenBoundaries(t *testing.T) {
	r := testRepo(t)
	base := time.Date(2026, 7, 20, 9, 0, 0, 0, time.UTC)
	end := base.Add(time.Hour)
	if _, err := r.CreateEntry(domain.Segment{Kind: domain.KindWork, Start: base, End: end}); err != nil {
		t.Fatal(err)
	}
	// Fenster beginnt exakt am Ende → kein Treffer (halboffen)
	if segs, _ := r.EntriesBetween(end, end.Add(time.Hour), end); len(segs) != 0 {
		t.Errorf("berührendes Fenster darf nicht treffen")
	}
	// Fenster endet exakt am Start → kein Treffer
	if segs, _ := r.EntriesBetween(base.Add(-time.Hour), base, base); len(segs) != 0 {
		t.Errorf("berührendes Fenster darf nicht treffen")
	}
	// Überschneidung um 1s → Treffer
	if segs, _ := r.EntriesBetween(base.Add(-time.Hour), base.Add(time.Second), base); len(segs) != 1 {
		t.Errorf("überlappendes Fenster muss treffen")
	}
}

func TestProjectsAbsencesConfig(t *testing.T) {
	r := testRepo(t)
	now := time.Now()

	pid, err := r.CreateProject("Acme", now)
	if err != nil {
		t.Fatal(err)
	}
	// COLLATE NOCASE
	if p, _ := r.ProjectByName("acme"); p == nil || p.ID != pid {
		t.Errorf("case-insensitive Lookup fehlgeschlagen")
	}
	if _, err := r.CreateProject("ACME", now); err == nil {
		t.Errorf("Duplikat (case-insensitive) nicht abgelehnt")
	}
	if err := r.SetProjectArchived(pid, true); err != nil {
		t.Fatal(err)
	}
	if ps, _ := r.Projects(false); len(ps) != 0 {
		t.Errorf("archiviertes Projekt sichtbar")
	}
	if ps, _ := r.Projects(true); len(ps) != 1 {
		t.Errorf("archiviertes Projekt fehlt bei includeArchived")
	}

	d := domain.Date{Year: 2026, Month: time.August, Day: 3}
	aid, err := r.CreateAbsence(domain.Absence{Date: d, Type: domain.AbsenceUrlaub, Fraction: 1})
	if err != nil {
		t.Fatal(err)
	}
	// UNIQUE(date)
	if _, err := r.CreateAbsence(domain.Absence{Date: d, Type: domain.AbsenceKrank, Fraction: 1}); err == nil {
		t.Errorf("zweite Abwesenheit am selben Tag nicht abgelehnt")
	}
	abs, err := r.AbsencesBetween(d, d)
	if err != nil || len(abs) != 1 || abs[0].ID != aid || abs[0].Type != domain.AbsenceUrlaub {
		t.Fatalf("AbsencesBetween: %v %+v", err, abs)
	}
	if err := r.DeleteAbsence(aid); err != nil {
		t.Fatal(err)
	}

	if v, _ := r.GetConfig("fehlt"); v != "" {
		t.Errorf("fehlender Key muss leer sein")
	}
	if err := r.SetConfig("bundesland", "BY"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetConfig("bundesland", "NW"); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.GetConfig("bundesland"); v != "NW" {
		t.Errorf("Upsert fehlgeschlagen: %q", v)
	}
	all, _ := r.AllConfig()
	if all["bundesland"] != "NW" {
		t.Errorf("AllConfig: %v", all)
	}
}

// TestTaskRoundtrip: Task-CRUD, Upsert-Idempotenz (pinnt ON CONFLICT gegen den
// Partial-Index unter modernc/sqlite) und FK-Schutz für referenzierte Aufgaben.
func TestTaskRoundtrip(t *testing.T) {
	r := testRepo(t)
	now := time.Unix(1000, 0)
	pid, err := r.CreateProject("acme", now)
	if err != nil {
		t.Fatal(err)
	}

	// Lokale Aufgabe + JIRA-Aufgabe
	localID, err := r.CreateTask(pid, "Doku", now)
	if err != nil {
		t.Fatal(err)
	}
	jid, err := r.UpsertJiraTask(pid, "ABC-1", "alt", now)
	if err != nil {
		t.Fatal(err)
	}
	// Upsert mit gleichem Key: gleiche Zeile, neuer Titel, Archiv-Reset
	if err := r.SetTaskArchived(jid, true); err != nil {
		t.Fatal(err)
	}
	jid2, err := r.UpsertJiraTask(pid, "ABC-1", "neu", now)
	if err != nil {
		t.Fatal(err)
	}
	if jid2 != jid {
		t.Fatalf("Upsert muss dieselbe Zeile treffen: %d != %d", jid2, jid)
	}
	tk, err := r.GetTask(jid)
	if err != nil || tk.Title != "neu" || tk.Archived || tk.JiraKey != "ABC-1" {
		t.Fatalf("nach Upsert: %+v, %v", tk, err)
	}
	// Zweite lokale Aufgabe: NULL-Keys kollidieren nicht
	if _, err := r.CreateTask(pid, "Zweite", now); err != nil {
		t.Fatalf("zwei lokale Aufgaben müssen möglich sein: %v", err)
	}
	tasks, err := r.TasksForProject(pid, true)
	if err != nil || len(tasks) != 3 {
		t.Fatalf("TasksForProject: %+v, %v", tasks, err)
	}

	// Entry mit TaskID: Roundtrip + FK blockt Löschen
	eid, err := r.CreateEntry(domain.Segment{Kind: domain.KindWork, Start: now, End: now.Add(time.Hour), TaskID: localID, ProjectIDs: []int64{pid}})
	if err != nil {
		t.Fatal(err)
	}
	e, err := r.GetEntry(eid)
	if err != nil || e.TaskID != localID {
		t.Fatalf("Entry-TaskID: %+v, %v", e, err)
	}
	if err := r.DeleteTask(localID); err == nil {
		t.Fatal("FK muss Löschen einer referenzierten Aufgabe blocken")
	}
	e.TaskID = 0
	if err := r.UpdateEntry(e); err != nil {
		t.Fatal(err)
	}
	if e, _ = r.GetEntry(eid); e.TaskID != 0 {
		t.Fatalf("TaskID muss entfernt sein: %+v", e)
	}
	if err := r.DeleteTask(localID); err != nil {
		t.Fatalf("ohne Referenz muss Löschen gehen: %v", err)
	}

	// Projekt-JIRA-Key
	if err := r.SetProjectJiraKey(pid, "ABC"); err != nil {
		t.Fatal(err)
	}
	p, _ := r.GetProject(pid)
	if p.JiraKey != "ABC" {
		t.Fatalf("JiraKey: %+v", p)
	}
	if err := r.SetProjectJiraKey(pid, ""); err != nil {
		t.Fatal(err)
	}
	if p, _ = r.GetProject(pid); p.JiraKey != "" {
		t.Fatalf("JiraKey muss entfernt sein: %+v", p)
	}
}
