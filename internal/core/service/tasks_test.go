package service

import (
	"errors"
	"testing"
	"time"

	"timetrack/internal/core/domain"
)

func TestTaskCRUD(t *testing.T) {
	svc, now := testService(t)
	p := jiraProject(t, svc, "acme", "")

	if _, err := svc.CreateTask(p.ID, "  "); !errors.Is(err, ErrConflict) {
		t.Fatalf("leerer Titel muss ErrConflict geben: %v", err)
	}
	tk, err := svc.CreateTask(p.ID, " Doku schreiben ")
	if err != nil || tk.Title != "Doku schreiben" {
		t.Fatalf("CreateTask: %+v, %v", tk, err)
	}

	title := "Doku überarbeiten"
	if err := svc.UpdateTask(tk.ID, &title, nil); err != nil {
		t.Fatal(err)
	}
	arch := true
	if err := svc.UpdateTask(tk.ID, nil, &arch); err != nil {
		t.Fatal(err)
	}
	tasks, _ := svc.Tasks(p.ID, false)
	if len(tasks) != 0 {
		t.Fatalf("archivierte Aufgabe darf nicht gelistet werden: %+v", tasks)
	}
	tasks, _ = svc.Tasks(p.ID, true)
	if len(tasks) != 1 || tasks[0].Title != "Doku überarbeiten" || !tasks[0].Archived {
		t.Fatalf("Tasks(includeArchived): %+v", tasks)
	}

	if err := svc.DeleteTask(tk.ID); err != nil {
		t.Fatal(err)
	}
	_ = now
}

func TestJiraTaskImmutable(t *testing.T) {
	svc, _ := testService(t)
	p := jiraProject(t, svc, "acme", "ABC")
	id, err := svc.repo.UpsertJiraTask(p.ID, "ABC-1", "Import", svc.now())
	if err != nil {
		t.Fatal(err)
	}
	title := "umbenannt"
	if err := svc.UpdateTask(id, &title, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("JIRA-Aufgabe umbenennen muss ErrConflict geben: %v", err)
	}
	if err := svc.DeleteTask(id); !errors.Is(err, ErrConflict) {
		t.Fatalf("JIRA-Aufgabe löschen muss ErrConflict geben: %v", err)
	}
}

func TestEntryTaskValidation(t *testing.T) {
	svc, now := testService(t)
	acme := jiraProject(t, svc, "acme", "")
	other := jiraProject(t, svc, "other", "")
	tk, err := svc.CreateTask(acme.ID, "Doku")
	if err != nil {
		t.Fatal(err)
	}
	day := now.Truncate(24 * time.Hour)

	// Aufgabe aus fremdem Projekt → abgelehnt
	if _, err := svc.AddEntry(domain.KindWork, "other", day.Add(9*time.Hour), day.Add(10*time.Hour), "", tk.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("fremde Aufgabe muss ErrConflict geben: %v", err)
	}
	_ = other

	id, err := svc.AddEntry(domain.KindWork, "acme", day.Add(9*time.Hour), day.Add(10*time.Hour), "", tk.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Referenzierte Aufgabe nicht löschbar
	if err := svc.DeleteTask(tk.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("verwendete Aufgabe löschen muss ErrConflict geben: %v", err)
	}

	// Projekt wechseln bei gesetzter Aufgabe → abgelehnt
	np := "other"
	if err := svc.UpdateEntry(id, EntryPatch{Project: &np}); !errors.Is(err, ErrConflict) {
		t.Fatalf("Projektwechsel mit fremder Aufgabe muss ErrConflict geben: %v", err)
	}

	// Aufgabe entfernen (0), dann ist auch der Projektwechsel ok
	zero := int64(0)
	if err := svc.UpdateEntry(id, EntryPatch{Task: &zero}); err != nil {
		t.Fatal(err)
	}
	if err := svc.UpdateEntry(id, EntryPatch{Project: &np}); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteTask(tk.ID); err != nil {
		t.Fatalf("nach Entfernen der Referenz muss Löschen gehen: %v", err)
	}
}
