package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	"timetrack/internal/core/domain"
)

// fakeRepo: In-Memory-Implementierung von ports.Repository.
type fakeRepo struct {
	entries  map[int64]domain.Segment
	projects map[int64]domain.Project
	absences map[int64]domain.Absence
	config   map[string]string
	nextID   int64
}

func newFake() *fakeRepo {
	return &fakeRepo{
		entries:  map[int64]domain.Segment{},
		projects: map[int64]domain.Project{},
		absences: map[int64]domain.Absence{},
		config:   map[string]string{},
	}
}

func (f *fakeRepo) id() int64 { f.nextID++; return f.nextID }

func (f *fakeRepo) OpenEntry() (*domain.Segment, error) {
	for _, e := range f.entries {
		if e.Open {
			cp := e
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) CreateEntry(e domain.Segment) (int64, error) {
	if e.Open {
		if open, _ := f.OpenEntry(); open != nil {
			return 0, errors.New("UNIQUE constraint failed: idx_one_open")
		}
	}
	e.ID = f.id()
	f.entries[e.ID] = e
	return e.ID, nil
}

func (f *fakeRepo) CloseEntry(id int64, end time.Time) error {
	e := f.entries[id]
	e.End, e.Open = end, false
	f.entries[id] = e
	return nil
}

func (f *fakeRepo) GetEntry(id int64) (domain.Segment, error) {
	e, ok := f.entries[id]
	if !ok {
		return domain.Segment{}, errors.New("not found")
	}
	return e, nil
}

func (f *fakeRepo) UpdateEntry(e domain.Segment) error { f.entries[e.ID] = e; return nil }
func (f *fakeRepo) DeleteEntry(id int64) error         { delete(f.entries, id); return nil }

func (f *fakeRepo) EntriesBetween(from, to, now time.Time) ([]domain.Segment, error) {
	var out []domain.Segment
	for _, e := range f.entries {
		end := e.End
		if e.Open {
			end = now
		}
		if e.Start.Before(to) && end.After(from) {
			out = append(out, e)
		}
	}
	return out, nil
}

func (f *fakeRepo) ProjectByName(name string) (*domain.Project, error) {
	for _, p := range f.projects {
		if strings.EqualFold(p.Name, name) {
			cp := p
			return &cp, nil
		}
	}
	return nil, nil
}

func (f *fakeRepo) GetProject(id int64) (domain.Project, error) {
	p, ok := f.projects[id]
	if !ok {
		return domain.Project{}, errors.New("not found")
	}
	return p, nil
}

func (f *fakeRepo) CreateProject(name string, _ time.Time) (int64, error) {
	id := f.id()
	f.projects[id] = domain.Project{ID: id, Name: name}
	return id, nil
}

func (f *fakeRepo) Projects(inc bool) ([]domain.Project, error) {
	var out []domain.Project
	for _, p := range f.projects {
		if inc || !p.Archived {
			out = append(out, p)
		}
	}
	return out, nil
}

func (f *fakeRepo) RenameProject(id int64, name string) error {
	p := f.projects[id]
	p.Name = name
	f.projects[id] = p
	return nil
}

func (f *fakeRepo) SetProjectArchived(id int64, a bool) error {
	p := f.projects[id]
	p.Archived = a
	f.projects[id] = p
	return nil
}

func (f *fakeRepo) SetProjectMeta(id int64, color, note string) error {
	p := f.projects[id]
	p.Color = color
	p.Note = note
	f.projects[id] = p
	return nil
}

func (f *fakeRepo) CreateAbsence(a domain.Absence) (int64, error) {
	for _, x := range f.absences {
		if x.Date == a.Date {
			return 0, errors.New("UNIQUE constraint failed: absences.date")
		}
	}
	a.ID = f.id()
	f.absences[a.ID] = a
	return a.ID, nil
}

func (f *fakeRepo) DeleteAbsence(id int64) error { delete(f.absences, id); return nil }

func (f *fakeRepo) AbsencesBetween(from, to domain.Date) ([]domain.Absence, error) {
	var out []domain.Absence
	for _, a := range f.absences {
		if !a.Date.Before(from) && !to.Before(a.Date) {
			out = append(out, a)
		}
	}
	return out, nil
}

func (f *fakeRepo) GetConfig(key string) (string, error) { return f.config[key], nil }
func (f *fakeRepo) SetConfig(key, value string) error    { f.config[key] = value; return nil }
func (f *fakeRepo) AllConfig() (map[string]string, error) {
	return f.config, nil
}

// --- Tests ---

var berlin, _ = time.LoadLocation("Europe/Berlin")

func testService(t *testing.T) (*Service, *time.Time) {
	t.Helper()
	svc := New(newFake(), berlin)
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, berlin) // Montag
	svc.Now = func() time.Time { return now }
	moFr := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday}
	err := svc.SaveSettings(Settings{
		WeeklyHours:  40,
		WeekdayHours: domain.SpreadWeekly(40*time.Hour, moFr, map[time.Weekday]time.Duration{time.Friday: 6 * time.Hour}),
		Land:         domain.BY,
		StartDate:    domain.Date{Year: 2026, Month: 7, Day: 20},
	})
	if err != nil {
		t.Fatal(err)
	}
	return svc, &now
}

func TestSettingsRoundtrip(t *testing.T) {
	svc, _ := testService(t)
	set, err := svc.Settings()
	if err != nil {
		t.Fatal(err)
	}
	if set.WeekdayHours[time.Friday] != 6*time.Hour || set.WeekdayHours[time.Monday] != 8*time.Hour+30*time.Minute {
		t.Errorf("WeekdayHours-Roundtrip: %v", set.WeekdayHours)
	}
	if set.Land != domain.BY || set.WeeklyHours != 40 {
		t.Errorf("Settings-Roundtrip: %+v", set)
	}
}

func TestStateMachine(t *testing.T) {
	svc, now := testService(t)

	if err := svc.Pause(); !errors.Is(err, ErrConflict) {
		t.Errorf("Pause ohne Start muss ErrConflict sein, got %v", err)
	}
	if _, err := svc.Stop(); !errors.Is(err, ErrConflict) {
		t.Errorf("Stop ohne Start muss ErrConflict sein, got %v", err)
	}
	if err := svc.Start("acme"); err != nil {
		t.Fatal(err)
	}
	if err := svc.Start("acme"); !errors.Is(err, ErrConflict) {
		t.Errorf("Doppelstart muss ErrConflict sein, got %v", err)
	}
	*now = now.Add(2 * time.Hour)
	if err := svc.Pause(); err != nil {
		t.Fatal(err)
	}
	if err := svc.Pause(); !errors.Is(err, ErrConflict) {
		t.Errorf("Doppelpause muss ErrConflict sein")
	}
	if err := svc.Switch("intern"); !errors.Is(err, ErrConflict) {
		t.Errorf("Switch während Pause muss ErrConflict sein")
	}
	st, err := svc.Status()
	if err != nil {
		t.Fatal(err)
	}
	if st.State != StatePaused || st.Project != "acme" {
		t.Errorf("Status: %+v", st)
	}
	*now = now.Add(30 * time.Minute)
	if err := svc.Resume(); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	if err := svc.Switch("intern"); err != nil {
		t.Fatal(err)
	}
	*now = now.Add(time.Hour)
	if _, err := svc.Stop(); err != nil {
		t.Fatal(err)
	}
	st, _ = svc.Status()
	if st.State != StateIdle {
		t.Errorf("nach Stop nicht idle")
	}
	// 2h acme + 1h acme + 1h intern = 4h Arbeit, 30 Min Pause
	if st.Today.Worked != 4*time.Hour {
		t.Errorf("Worked = %v, want 4h", st.Today.Worked)
	}
	if st.Today.Break != 30*time.Minute {
		t.Errorf("Break = %v, want 30m", st.Today.Break)
	}
	// Saldo: 4h Ist − 8,5h Soll
	if st.Saldo != -(4*time.Hour + 30*time.Minute) {
		t.Errorf("Saldo = %v, want -4h30m", st.Saldo)
	}
}

func TestAutoProjectCreation(t *testing.T) {
	svc, _ := testService(t)
	if err := svc.Start("Neues Projekt"); err != nil {
		t.Fatal(err)
	}
	p, _ := svc.repo.ProjectByName("neues projekt")
	if p == nil {
		t.Errorf("Projekt nicht automatisch angelegt (case-insensitive)")
	}
}

func TestOverlapRejection(t *testing.T) {
	svc, now := testService(t)
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, berlin)
	add := func(from, to int) (int64, error) {
		return svc.AddEntry(domain.KindWork, "", day.Add(time.Duration(from)*time.Hour), day.Add(time.Duration(to)*time.Hour), "")
	}
	id, err := add(9, 12)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := add(11, 13); !errors.Is(err, ErrConflict) {
		t.Errorf("Überlappung nicht abgelehnt: %v", err)
	}
	if _, err := add(12, 13); err != nil {
		t.Errorf("berührender Eintrag fälschlich abgelehnt: %v", err)
	}
	// Edit auf sich selbst erlaubt, Overlap mit anderem nicht
	start2 := day.Add(9*time.Hour + 30*time.Minute)
	if err := svc.UpdateEntry(id, EntryPatch{Start: &start2}); err != nil {
		t.Errorf("Edit am eigenen Eintrag abgelehnt: %v", err)
	}
	bad := day.Add(12*time.Hour + 30*time.Minute)
	if err := svc.UpdateEntry(id, EntryPatch{End: &bad}); !errors.Is(err, ErrConflict) {
		t.Errorf("Edit-Überlappung nicht abgelehnt: %v", err)
	}
	// Überlappung mit offenem Segment
	if err := svc.Start(""); err != nil {
		t.Fatal(err)
	}
	if _, err := add(8, 10); !errors.Is(err, ErrConflict) {
		t.Errorf("Überlappung mit offenem Segment nicht erkannt: %v", err)
	}
	_ = now
}

func TestAbsencesAndReport(t *testing.T) {
	svc, _ := testService(t)
	mon := domain.Date{Year: 2026, Month: 7, Day: 20}
	fri := domain.Date{Year: 2026, Month: 7, Day: 24}
	sun := domain.Date{Year: 2026, Month: 7, Day: 26}

	// Urlaub Mo–So: Wochenende übersprungen → 5 Tage
	added, err := svc.AddAbsence(domain.AbsenceUrlaub, mon, sun, 1.0, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 5 {
		t.Errorf("erwartet 5 Urlaubstage, got %d (%v)", len(added), added)
	}
	// Doppelbelegung
	if _, err := svc.AddAbsence(domain.AbsenceKrank, mon, mon, 1.0, ""); !errors.Is(err, ErrConflict) {
		t.Errorf("Doppelbelegung nicht abgelehnt")
	}

	rep, err := svc.Report(mon, sun)
	if err != nil {
		t.Fatal(err)
	}
	// Volle Urlaubswoche: Credit = Soll = 40h, Saldo 0
	if rep.TotalTarget != 40*time.Hour || rep.TotalCredit != 40*time.Hour || rep.Saldo != 0 {
		t.Errorf("Urlaubswoche: Target %v Credit %v Saldo %v", rep.TotalTarget, rep.TotalCredit, rep.Saldo)
	}
	_ = fri
}

func TestReportProjects(t *testing.T) {
	svc, _ := testService(t)
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, berlin)
	if _, err := svc.AddEntry(domain.KindWork, "acme", day.Add(9*time.Hour), day.Add(15*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddEntry(domain.KindWork, "intern", day.Add(15*time.Hour), day.Add(17*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	d := domain.Date{Year: 2026, Month: 7, Day: 20}
	rep, err := svc.Report(d, d)
	if err != nil {
		t.Fatal(err)
	}
	var acmeID int64
	for id, name := range rep.ProjectNames {
		if name == "acme" {
			acmeID = id
		}
	}
	if pct := rep.ProjectPercent[acmeID]; pct < 74.9 || pct > 75.1 {
		t.Errorf("acme = %.1f%%, want 75%%", pct)
	}
}

func TestMultiProjectSplit(t *testing.T) {
	svc, _ := testService(t)
	day := time.Date(2026, 7, 20, 0, 0, 0, 0, berlin)
	// 6h auf acme+intern (dedupe: "Acme" doppelt), 2h nur acme
	if _, err := svc.AddEntry(domain.KindWork, "acme+intern+Acme", day.Add(9*time.Hour), day.Add(15*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AddEntry(domain.KindWork, "acme", day.Add(15*time.Hour), day.Add(17*time.Hour), ""); err != nil {
		t.Fatal(err)
	}
	d := domain.Date{Year: 2026, Month: 7, Day: 20}
	rep, err := svc.Report(d, d)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]time.Duration{}
	for pid, dur := range rep.ProjectTotals {
		byName[rep.ProjectNames[pid]] = dur
	}
	// acme: 3h + 2h = 5h, intern: 3h
	if byName["acme"] != 5*time.Hour || byName["intern"] != 3*time.Hour {
		t.Errorf("Split: %v", byName)
	}
	if len(rep.ProjectTotals) != 2 {
		t.Errorf("Dedupe fehlgeschlagen: %v", rep.ProjectNames)
	}
}

func TestHalfVacation(t *testing.T) {
	svc, _ := testService(t)
	mon := domain.Date{Year: 2026, Month: 7, Day: 20}
	if _, err := svc.AddAbsence(domain.AbsenceUrlaub, mon, mon, 0.5, ""); err != nil {
		t.Fatal(err)
	}
	rep, _ := svc.Report(mon, mon)
	// Soll Mo 8,5h, halber Urlaub → Credit 4h15m, Diff = -4h15m
	want := -(4*time.Hour + 15*time.Minute)
	if rep.Saldo != want {
		t.Errorf("halber Urlaub: Saldo %v, want %v", rep.Saldo, want)
	}
}
