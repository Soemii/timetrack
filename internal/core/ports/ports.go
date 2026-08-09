package ports

import (
	"time"

	"timetrack/internal/core/domain"
)

// LockEvent: Bildschirm gesperrt/entsperrt (Quelle: OS-Log oder Watcher).
type LockEvent struct {
	Time   time.Time
	Locked bool // true = gesperrt
}

// Meeting: akzeptierter Kalendertermin aus der Outlook-ICS-Quelle.
type Meeting struct {
	Start, End time.Time
	Subject    string
}

// Issue: dem Nutzer zugewiesenes, ungelöstes JIRA-Ticket.
type Issue struct {
	Key        string // z.B. "ABC-123"
	ProjectKey string // z.B. "ABC"
	Summary    string
}

// Repository ist der einzige Persistenz-Port.
// ponytail: ein Interface statt eines pro Aggregat — aufteilen falls es wächst.
type Repository interface {
	// Entries
	OpenEntry() (*domain.Segment, error) // nil = keins offen
	CreateEntry(e domain.Segment) (int64, error)
	CloseEntry(id int64, end time.Time) error
	GetEntry(id int64) (domain.Segment, error)
	UpdateEntry(e domain.Segment) error
	DeleteEntry(id int64) error
	// Alle Segmente, die das Fenster [from, to) berühren; offene zählen bis now.
	EntriesBetween(from, to, now time.Time) ([]domain.Segment, error)

	// Projects
	ProjectByName(name string) (*domain.Project, error)
	GetProject(id int64) (domain.Project, error)
	CreateProject(name string, createdAt time.Time) (int64, error)
	Projects(includeArchived bool) ([]domain.Project, error)
	RenameProject(id int64, name string) error
	SetProjectArchived(id int64, archived bool) error
	SetProjectMeta(id int64, color, note string) error
	SetProjectCompany(id, companyID int64) error  // companyID 0 = Zuordnung entfernen
	SetProjectJiraKey(id int64, key string) error // "" = Mapping entfernen

	// Tasks
	TasksForProject(projectID int64, includeArchived bool) ([]domain.Task, error)
	Tasks() ([]domain.Task, error)
	GetTask(id int64) (domain.Task, error)
	CreateTask(projectID int64, title string, createdAt time.Time) (int64, error)
	UpsertJiraTask(projectID int64, key, title string, createdAt time.Time) (int64, error)
	RenameTask(id int64, title string) error
	SetTaskArchived(id int64, archived bool) error
	DeleteTask(id int64) error
	CountEntriesForTask(id int64) (int64, error)

	// Companies
	Companies() ([]domain.Company, error)
	CompanyByName(name string) (*domain.Company, error)
	CreateCompany(name string, createdAt time.Time) (int64, error)
	DeleteCompany(id int64) error
	CountProjectsForCompany(id int64) (int64, error)

	// Absences
	CreateAbsence(a domain.Absence) (int64, error)
	DeleteAbsence(id int64) error
	AbsencesBetween(from, to domain.Date) ([]domain.Absence, error)

	// Config
	GetConfig(key string) (string, error) // "" wenn nicht gesetzt
	SetConfig(key, value string) error
	AllConfig() (map[string]string, error)
}
