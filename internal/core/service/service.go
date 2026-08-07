package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/ports"
)

// ErrConflict markiert Zustands-/Validierungsfehler (HTTP 409, CLI-Exit 1).
var ErrConflict = errors.New("konflikt")

// ErrNotInitialized: Setup fehlt.
var ErrNotInitialized = errors.New("noch nicht eingerichtet — bitte zuerst 'timetrack init' ausführen")

type Service struct {
	repo ports.Repository
	loc  *time.Location
	Now  func() time.Time // injizierbar für Tests
	// LockEvents liefert Sperr-/Entsperr-Events seit einem Zeitpunkt.
	// nil = kein Lock-Tracking (nicht unterstütztes OS, Tests).
	LockEvents func(since time.Time) ([]ports.LockEvent, error)
}

func New(repo ports.Repository, loc *time.Location) *Service {
	return &Service{repo: repo, loc: loc, Now: time.Now}
}

// now liefert die aktuelle Zeit sekundengenau — die DB speichert
// Unix-Sekunden, feinere Auflösung führt zu Vergleichsfehlern.
func (s *Service) now() time.Time { return s.Now().Truncate(time.Second) }

// --- Settings ---

type Settings struct {
	WeeklyHours  float64
	WeekdayHours domain.WeekdayHours
	Land         domain.Bundesland
	Augsburg     bool
	Katholisch   bool
	StartDate    domain.Date
}

var weekdayKeys = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func (s *Service) Initialized() bool {
	v, err := s.repo.GetConfig("weekday_hours")
	return err == nil && v != ""
}

func (s *Service) Settings() (Settings, error) {
	cfg, err := s.repo.AllConfig()
	if err != nil {
		return Settings{}, err
	}
	if cfg["weekday_hours"] == "" {
		return Settings{}, ErrNotInitialized
	}
	var hours map[string]float64
	if err := json.Unmarshal([]byte(cfg["weekday_hours"]), &hours); err != nil {
		return Settings{}, fmt.Errorf("weekday_hours defekt: %w", err)
	}
	set := Settings{Augsburg: cfg["by_augsburg"] == "true", Katholisch: cfg["by_katholisch"] == "true"}
	for i, k := range weekdayKeys {
		set.WeekdayHours[i] = time.Duration(hours[k] * float64(time.Hour))
	}
	fmt.Sscanf(cfg["weekly_hours"], "%g", &set.WeeklyHours)
	set.Land = domain.Bundesland(cfg["bundesland"])
	if set.StartDate, err = domain.ParseDate(cfg["start_date"]); err != nil {
		return Settings{}, fmt.Errorf("start_date defekt: %w", err)
	}
	return set, nil
}

func (s *Service) SaveSettings(set Settings) error {
	hours := map[string]float64{}
	for i, k := range weekdayKeys {
		hours[k] = set.WeekdayHours[i].Hours()
	}
	js, _ := json.Marshal(hours)
	pairs := map[string]string{
		"weekly_hours":  fmt.Sprintf("%g", set.WeeklyHours),
		"weekday_hours": string(js),
		"bundesland":    string(set.Land),
		"by_augsburg":   fmt.Sprintf("%t", set.Augsburg),
		"by_katholisch": fmt.Sprintf("%t", set.Katholisch),
		"start_date":    set.StartDate.String(),
	}
	for k, v := range pairs {
		if err := s.repo.SetConfig(k, v); err != nil {
			return err
		}
	}
	return nil
}

// --- Tracking ---

type TrackState string

const (
	StateIdle    TrackState = "idle"
	StateWorking TrackState = "working"
	StatePaused  TrackState = "paused"
)

// resolveProjects parst eine Projektangabe wie "acme" oder "acme+intern"
// (Zeit wird gleichmäßig aufgeteilt), legt unbekannte Projekte an und
// dedupliziert case-insensitiv. Leer → nil (ohne Projekt).
func (s *Service) resolveProjects(spec string) ([]int64, error) {
	var ids []int64
	seen := map[string]bool{}
	for part := range strings.SplitSeq(spec, "+") {
		name := strings.TrimSpace(part)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		p, err := s.repo.ProjectByName(name)
		if err != nil {
			return nil, err
		}
		if p != nil {
			ids = append(ids, p.ID)
			continue
		}
		id, err := s.repo.CreateProject(name, s.now())
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// projectLabel liefert die Projektnamen eines Segments, mit "+" verbunden.
func (s *Service) projectLabel(ids []int64) string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		p, err := s.repo.GetProject(id)
		if err != nil {
			names = append(names, "?")
			continue
		}
		names = append(names, p.Name)
	}
	return strings.Join(names, "+")
}

// closeOrDrop schließt ein offenes Segment; Segmente ohne Dauer (Start ==
// Ende, z.B. pause+resume in derselben Sekunde) werden gelöscht, da das
// DB-CHECK end_ts > start_ts verlangt und leere Segmente nichts aussagen.
func (s *Service) closeOrDrop(open *domain.Segment, now time.Time) error {
	if !now.After(open.Start) {
		return s.repo.DeleteEntry(open.ID)
	}
	return s.repo.CloseEntry(open.ID, now)
}

func (s *Service) Start(project string) error {
	if err := s.syncLockState(true); err != nil {
		return err
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return err
	}
	if open != nil {
		if open.Kind == domain.KindWork {
			return fmt.Errorf("%w: bereits gestartet (Projekt %q seit %s). Projektwechsel mit 'timetrack switch'",
				ErrConflict, s.projectLabel(open.ProjectIDs), open.Start.In(s.loc).Format("15:04"))
		}
		return fmt.Errorf("%w: Pause läuft — fortsetzen mit 'timetrack resume'", ErrConflict)
	}
	pids, err := s.resolveProjects(project)
	if err != nil {
		return err
	}
	_, err = s.repo.CreateEntry(domain.Segment{Kind: domain.KindWork, ProjectIDs: pids, Start: s.now(), Open: true})
	return err
}

func (s *Service) Pause() error {
	if err := s.syncLockState(true); err != nil {
		return err
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return err
	}
	if open == nil {
		return fmt.Errorf("%w: nichts gestartet — zuerst 'timetrack start'", ErrConflict)
	}
	if open.Kind == domain.KindBreak {
		return fmt.Errorf("%w: Pause läuft bereits seit %s", ErrConflict, open.Start.In(s.loc).Format("15:04"))
	}
	now := s.now()
	if err := s.closeOrDrop(open, now); err != nil {
		return err
	}
	_, err = s.repo.CreateEntry(domain.Segment{Kind: domain.KindBreak, ProjectIDs: open.ProjectIDs, Start: now, Open: true})
	return err
}

func (s *Service) Resume() error {
	if err := s.syncLockState(true); err != nil {
		return err
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return err
	}
	if open == nil || open.Kind != domain.KindBreak {
		return fmt.Errorf("%w: keine Pause aktiv", ErrConflict)
	}
	now := s.now()
	if err := s.closeOrDrop(open, now); err != nil {
		return err
	}
	_, err = s.repo.CreateEntry(domain.Segment{Kind: domain.KindWork, ProjectIDs: open.ProjectIDs, Start: now, Open: true})
	return err
}

func (s *Service) Switch(project string) error {
	if strings.TrimSpace(project) == "" {
		return fmt.Errorf("%w: Projektname fehlt", ErrConflict)
	}
	if err := s.syncLockState(true); err != nil {
		return err
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return err
	}
	if open == nil || open.Kind != domain.KindWork {
		return fmt.Errorf("%w: nicht am Arbeiten — zuerst 'timetrack start' bzw. 'timetrack resume'", ErrConflict)
	}
	pids, err := s.resolveProjects(project)
	if err != nil {
		return err
	}
	now := s.now()
	if err := s.closeOrDrop(open, now); err != nil {
		return err
	}
	_, err = s.repo.CreateEntry(domain.Segment{Kind: domain.KindWork, ProjectIDs: pids, Start: now, Open: true})
	return err
}

// Stop schließt das offene Segment und liefert die Warnungen des Tages.
func (s *Service) Stop() ([]string, error) {
	if err := s.syncLockState(true); err != nil {
		return nil, err
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return nil, err
	}
	if open == nil {
		return nil, fmt.Errorf("%w: nichts gestartet", ErrConflict)
	}
	if err := s.closeOrDrop(open, s.now()); err != nil {
		return nil, err
	}
	st, err := s.Status()
	if err != nil {
		return nil, err
	}
	return st.Today.Warnings, nil
}

type Status struct {
	State        TrackState
	Project      string
	Since        time.Time
	OpenDuration time.Duration
	Today        domain.DaySummary
	Saldo        time.Duration // inkl. heute, ab start_date
	LongSession  bool          // offenes Segment > 12h
}

func (s *Service) Status() (Status, error) {
	st := Status{State: StateIdle}
	if err := s.syncLockState(false); err != nil {
		return st, err
	}
	open, err := s.repo.OpenEntry()
	if err != nil {
		return st, err
	}
	now := s.now()
	if open != nil {
		if open.Kind == domain.KindWork {
			st.State = StateWorking
		} else {
			st.State = StatePaused
		}
		st.Project = s.projectLabel(open.ProjectIDs)
		st.Since = open.Start
		st.OpenDuration = now.Sub(open.Start)
		st.LongSession = st.OpenDuration > 12*time.Hour
	}
	set, err := s.Settings()
	if err != nil {
		return st, err
	}
	today := domain.DateOf(now, s.loc)
	rep, err := s.Report(set.StartDate, today)
	if err != nil {
		return st, err
	}
	st.Saldo = rep.Saldo
	for _, d := range rep.Days {
		if d.Date == today {
			st.Today = d
		}
	}
	return st, nil
}

// --- Entries ---

func (s *Service) checkOverlap(candidate domain.Segment) error {
	now := s.now()
	existing, err := s.repo.EntriesBetween(candidate.Start, candidate.End, now)
	if err != nil {
		return err
	}
	for i := range existing {
		if existing[i].Open {
			existing[i].End = now
		}
	}
	if hit := domain.FindOverlap(candidate, existing); hit != nil {
		return fmt.Errorf("%w: Überschneidung mit Eintrag #%d (%s–%s)", ErrConflict, hit.ID,
			hit.Start.In(s.loc).Format("02.01. 15:04"), hit.End.In(s.loc).Format("15:04"))
	}
	return nil
}

func (s *Service) validateEntry(e domain.Segment) error {
	if !e.Open && !e.End.After(e.Start) {
		return fmt.Errorf("%w: Ende muss nach dem Start liegen", ErrConflict)
	}
	return s.checkOverlap(e)
}

func (s *Service) AddEntry(kind domain.Kind, project string, start, end time.Time, note string) (int64, error) {
	pids, err := s.resolveProjects(project)
	if err != nil {
		return 0, err
	}
	e := domain.Segment{Kind: kind, ProjectIDs: pids, Start: start, End: end, Note: note}
	if err := s.validateEntry(e); err != nil {
		return 0, err
	}
	return s.repo.CreateEntry(e)
}

// EntryPatch: nil = Feld unverändert.
type EntryPatch struct {
	Kind    *domain.Kind
	Project *string
	Start   *time.Time
	End     *time.Time
	Note    *string
}

func (s *Service) UpdateEntry(id int64, p EntryPatch) error {
	e, err := s.repo.GetEntry(id)
	if err != nil {
		return fmt.Errorf("Eintrag #%d nicht gefunden", id)
	}
	if p.Kind != nil {
		e.Kind = *p.Kind
	}
	if p.Project != nil {
		if e.ProjectIDs, err = s.resolveProjects(*p.Project); err != nil {
			return err
		}
	}
	if p.Start != nil {
		e.Start = *p.Start
	}
	if p.End != nil {
		e.End = *p.End
		e.Open = false
	}
	if p.Note != nil {
		e.Note = *p.Note
	}
	check := e
	if check.Open {
		check.End = s.now()
	}
	if err := s.validateEntry(check); err != nil {
		return err
	}
	return s.repo.UpdateEntry(e)
}

func (s *Service) DeleteEntry(id int64) error {
	if _, err := s.repo.GetEntry(id); err != nil {
		return fmt.Errorf("Eintrag #%d nicht gefunden", id)
	}
	return s.repo.DeleteEntry(id)
}

func (s *Service) ListEntries(from, to domain.Date) ([]domain.Segment, error) {
	return s.repo.EntriesBetween(from.Time(s.loc), to.AddDays(1).Time(s.loc), s.now())
}

// --- Projects ---

func (s *Service) Projects(includeArchived bool) ([]domain.Project, error) {
	return s.repo.Projects(includeArchived)
}

func (s *Service) RenameProject(id int64, name string) error {
	return s.repo.RenameProject(id, name)
}

func (s *Service) ArchiveProject(id int64) error {
	return s.repo.SetProjectArchived(id, true)
}

// UpdateProjectMeta ändert Farbe und/oder Notiz; nil lässt das Feld unverändert.
func (s *Service) UpdateProjectMeta(id int64, color, note *string) error {
	p, err := s.repo.GetProject(id)
	if err != nil {
		return err
	}
	if color != nil {
		p.Color = *color
	}
	if note != nil {
		p.Note = *note
	}
	return s.repo.SetProjectMeta(id, p.Color, p.Note)
}

// CreateProjectExplicit legt ein Projekt gezielt an (Projekte-Tab),
// im Gegensatz zur impliziten Anlage über resolveProjects.
func (s *Service) CreateProjectExplicit(name, color, note string) (domain.Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return domain.Project{}, fmt.Errorf("%w: Projektname fehlt", ErrConflict)
	}
	if existing, err := s.repo.ProjectByName(name); err != nil {
		return domain.Project{}, err
	} else if existing != nil {
		return domain.Project{}, fmt.Errorf("%w: Projekt %q existiert bereits", ErrConflict, existing.Name)
	}
	id, err := s.repo.CreateProject(name, s.now())
	if err != nil {
		return domain.Project{}, err
	}
	if color != "" || note != "" {
		if err := s.repo.SetProjectMeta(id, color, note); err != nil {
			return domain.Project{}, err
		}
	}
	return s.repo.GetProject(id)
}

// --- Absences ---

// AddAbsence trägt eine Abwesenheit für einen Zeitraum ein.
// Tage ohne Soll (Wochenende) werden bei Zeiträumen übersprungen.
func (s *Service) AddAbsence(t domain.AbsenceType, from, to domain.Date, fraction float64, note string) ([]domain.Date, error) {
	if fraction != 0.5 && fraction != 1.0 {
		return nil, fmt.Errorf("%w: Bruchteil muss 0.5 oder 1.0 sein", ErrConflict)
	}
	set, err := s.Settings()
	if err != nil {
		return nil, err
	}
	if to.Before(from) {
		return nil, fmt.Errorf("%w: Enddatum vor Startdatum", ErrConflict)
	}
	var added []domain.Date
	single := from == to
	for d := from; !to.Before(d); d = d.AddDays(1) {
		if !single && domain.DayTarget(d, set.WeekdayHours) == 0 {
			continue // Wochenende im Zeitraum überspringen
		}
		if _, err := s.repo.CreateAbsence(domain.Absence{Date: d, Type: t, Fraction: fraction, Note: note}); err != nil {
			return added, fmt.Errorf("%w: am %s ist bereits eine Abwesenheit eingetragen", ErrConflict, d)
		}
		added = append(added, d)
	}
	return added, nil
}

func (s *Service) DeleteAbsence(id int64) error {
	return s.repo.DeleteAbsence(id)
}

func (s *Service) Absences(year int) ([]domain.Absence, error) {
	return s.repo.AbsencesBetween(domain.Date{Year: year, Month: 1, Day: 1}, domain.Date{Year: year, Month: 12, Day: 31})
}

// --- Report ---

type Report struct {
	From, To       domain.Date
	Days           []domain.DaySummary
	TotalWorked    time.Duration
	TotalTarget    time.Duration
	TotalCredit    time.Duration
	Saldo          time.Duration
	ProjectTotals  map[int64]time.Duration
	ProjectPercent map[int64]float64
	ProjectNames   map[int64]string
}

func (s *Service) Report(from, to domain.Date) (Report, error) {
	set, err := s.Settings()
	if err != nil {
		return Report{}, err
	}
	if to.Before(from) {
		return Report{}, fmt.Errorf("%w: Enddatum vor Startdatum", ErrConflict)
	}
	now := s.now()
	segs, err := s.repo.EntriesBetween(from.Time(s.loc), to.AddDays(1).Time(s.loc), now)
	if err != nil {
		return Report{}, err
	}
	for i := range segs {
		if segs[i].Open {
			segs[i].End = now
		}
	}
	byDay := domain.SplitAtMidnights(domain.Effective(segs), s.loc)

	absences, err := s.repo.AbsencesBetween(from, to)
	if err != nil {
		return Report{}, err
	}
	absByDay := map[domain.Date]*domain.Absence{}
	for i := range absences {
		absByDay[absences[i].Date] = &absences[i]
	}
	holidays := map[int]map[domain.Date]string{}
	for y := from.Year; y <= to.Year; y++ {
		holidays[y] = domain.Holidays(y, set.Land, set.Augsburg, set.Katholisch)
	}

	rep := Report{From: from, To: to}
	for d := from; !to.Before(d); d = d.AddDays(1) {
		day := domain.BuildDay(d, byDay[d], absByDay[d], holidays[d.Year][d], set.WeekdayHours)
		rep.Days = append(rep.Days, day)
		rep.TotalWorked += day.Worked
		rep.TotalTarget += day.Target
		rep.TotalCredit += day.Credit
	}
	rep.Saldo = domain.Saldo(rep.Days)
	rep.ProjectTotals, rep.ProjectPercent = domain.ProjectBreakdown(rep.Days)

	rep.ProjectNames = map[int64]string{0: "(ohne Projekt)"}
	projects, err := s.repo.Projects(true)
	if err != nil {
		return Report{}, err
	}
	for _, p := range projects {
		rep.ProjectNames[p.ID] = p.Name
	}
	return rep, nil
}
