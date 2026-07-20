package ports

import (
	"time"

	"timetrack/internal/core/domain"
)

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

	// Absences
	CreateAbsence(a domain.Absence) (int64, error)
	DeleteAbsence(id int64) error
	AbsencesBetween(from, to domain.Date) ([]domain.Absence, error)

	// Config
	GetConfig(key string) (string, error) // "" wenn nicht gesetzt
	SetConfig(key, value string) error
	AllConfig() (map[string]string, error)
}
