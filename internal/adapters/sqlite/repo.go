package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"timetrack/internal/adapters/sqlite/gen"
	"timetrack/internal/core/domain"
	"timetrack/internal/core/ports"
)

type Repo struct {
	db *sql.DB
	q  *db.Queries
}

var _ ports.Repository = (*Repo)(nil)

func NewRepo(sqlDB *sql.DB) *Repo {
	return &Repo{db: sqlDB, q: db.New(sqlDB)}
}

// inTx führt fn in einer Transaktion mit tx-gebundenen Queries aus.
func (r *Repo) inTx(fn func(q *db.Queries) error) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(r.q.WithTx(tx)); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

var ctx = context.Background() // ponytail: lokales Tool, keine Request-Kontexte

func toSegment(e db.Entry) domain.Segment {
	s := domain.Segment{
		ID:    e.ID,
		Kind:  domain.Kind(e.Kind),
		Start: time.Unix(e.StartTs, 0),
		Note:  e.Note.String,
	}
	if e.EndTs.Valid {
		s.End = time.Unix(e.EndTs.Int64, 0)
	} else {
		s.Open = true
	}
	return s
}

func nullEnd(s domain.Segment) sql.NullInt64 {
	if s.Open {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: s.End.Unix(), Valid: true}
}

func nullStr(s string) sql.NullString {
	return sql.NullString{String: s, Valid: s != ""}
}

// --- Entries ---

// attachProjects lädt die Projektzuordnungen für die gegebenen Segmente.
func (r *Repo) attachProjects(segs []domain.Segment) error {
	if len(segs) == 0 {
		return nil
	}
	ids := make([]int64, len(segs))
	byID := map[int64]*domain.Segment{}
	for i := range segs {
		ids[i] = segs[i].ID
		byID[segs[i].ID] = &segs[i]
	}
	links, err := r.q.ListEntryProjectsFor(ctx, ids)
	if err != nil {
		return err
	}
	for _, l := range links {
		s := byID[l.EntryID]
		s.ProjectIDs = append(s.ProjectIDs, l.ProjectID)
	}
	return nil
}

func (r *Repo) OpenEntry() (*domain.Segment, error) {
	e, err := r.q.GetOpenEntry(ctx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	segs := []domain.Segment{toSegment(e)}
	if err := r.attachProjects(segs); err != nil {
		return nil, err
	}
	return &segs[0], nil
}

func (r *Repo) CreateEntry(s domain.Segment) (int64, error) {
	var id int64
	err := r.inTx(func(q *db.Queries) error {
		var err error
		id, err = q.CreateEntry(ctx, db.CreateEntryParams{
			Kind:    string(s.Kind),
			StartTs: s.Start.Unix(),
			EndTs:   nullEnd(s),
			Note:    nullStr(s.Note),
		})
		if err != nil {
			return err
		}
		for _, pid := range s.ProjectIDs {
			if err := q.AddEntryProject(ctx, db.AddEntryProjectParams{EntryID: id, ProjectID: pid}); err != nil {
				return err
			}
		}
		return nil
	})
	return id, err
}

func (r *Repo) CloseEntry(id int64, end time.Time) error {
	return r.q.CloseEntry(ctx, db.CloseEntryParams{
		EndTs: sql.NullInt64{Int64: end.Unix(), Valid: true},
		ID:    id,
	})
}

func (r *Repo) GetEntry(id int64) (domain.Segment, error) {
	e, err := r.q.GetEntry(ctx, id)
	if err != nil {
		return domain.Segment{}, err
	}
	segs := []domain.Segment{toSegment(e)}
	if err := r.attachProjects(segs); err != nil {
		return domain.Segment{}, err
	}
	return segs[0], nil
}

func (r *Repo) UpdateEntry(s domain.Segment) error {
	return r.inTx(func(q *db.Queries) error {
		if err := q.UpdateEntry(ctx, db.UpdateEntryParams{
			Kind:    string(s.Kind),
			StartTs: s.Start.Unix(),
			EndTs:   nullEnd(s),
			Note:    nullStr(s.Note),
			ID:      s.ID,
		}); err != nil {
			return err
		}
		if err := q.DeleteEntryProjects(ctx, s.ID); err != nil {
			return err
		}
		for _, pid := range s.ProjectIDs {
			if err := q.AddEntryProject(ctx, db.AddEntryProjectParams{EntryID: s.ID, ProjectID: pid}); err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repo) DeleteEntry(id int64) error {
	return r.q.DeleteEntry(ctx, id)
}

func (r *Repo) EntriesBetween(from, to, now time.Time) ([]domain.Segment, error) {
	rows, err := r.q.ListEntriesTouching(ctx, db.ListEntriesTouchingParams{
		Until:  to.Unix(),
		Now:    sql.NullInt64{Int64: now.Unix(), Valid: true},
		FromTs: sql.NullInt64{Int64: from.Unix(), Valid: true},
	})
	if err != nil {
		return nil, err
	}
	segs := make([]domain.Segment, len(rows))
	for i, e := range rows {
		segs[i] = toSegment(e)
	}
	if err := r.attachProjects(segs); err != nil {
		return nil, err
	}
	return segs, nil
}

// --- Projects ---

func toProject(p db.Project) domain.Project {
	return domain.Project{ID: p.ID, Name: p.Name, Archived: p.Archived != 0, Color: p.Color.String, Note: p.Note.String}
}

func (r *Repo) ProjectByName(name string) (*domain.Project, error) {
	p, err := r.q.GetProjectByName(ctx, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dp := toProject(p)
	return &dp, nil
}

func (r *Repo) GetProject(id int64) (domain.Project, error) {
	p, err := r.q.GetProject(ctx, id)
	if err != nil {
		return domain.Project{}, err
	}
	return toProject(p), nil
}

func (r *Repo) CreateProject(name string, createdAt time.Time) (int64, error) {
	return r.q.CreateProject(ctx, db.CreateProjectParams{Name: name, CreatedAt: createdAt.Unix()})
}

func (r *Repo) Projects(includeArchived bool) ([]domain.Project, error) {
	inc := int64(0)
	if includeArchived {
		inc = 1
	}
	rows, err := r.q.ListProjects(ctx, inc)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Project, len(rows))
	for i, p := range rows {
		out[i] = toProject(p)
	}
	return out, nil
}

func (r *Repo) RenameProject(id int64, name string) error {
	return r.q.RenameProject(ctx, db.RenameProjectParams{Name: name, ID: id})
}

func (r *Repo) SetProjectArchived(id int64, archived bool) error {
	a := int64(0)
	if archived {
		a = 1
	}
	return r.q.SetProjectArchived(ctx, db.SetProjectArchivedParams{Archived: a, ID: id})
}

func (r *Repo) SetProjectMeta(id int64, color, note string) error {
	return r.q.SetProjectMeta(ctx, db.SetProjectMetaParams{Color: nullStr(color), Note: nullStr(note), ID: id})
}

// --- Absences ---

func (r *Repo) CreateAbsence(a domain.Absence) (int64, error) {
	return r.q.CreateAbsence(ctx, db.CreateAbsenceParams{
		Date:     a.Date.String(),
		Type:     string(a.Type),
		Fraction: a.Fraction,
		Note:     nullStr(a.Note),
	})
}

func (r *Repo) DeleteAbsence(id int64) error {
	return r.q.DeleteAbsence(ctx, id)
}

func (r *Repo) AbsencesBetween(from, to domain.Date) ([]domain.Absence, error) {
	rows, err := r.q.ListAbsencesBetween(ctx, db.ListAbsencesBetweenParams{
		FromDate: from.String(),
		ToDate:   to.String(),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Absence, len(rows))
	for i, a := range rows {
		d, err := domain.ParseDate(a.Date)
		if err != nil {
			return nil, err
		}
		out[i] = domain.Absence{ID: a.ID, Date: d, Type: domain.AbsenceType(a.Type), Fraction: a.Fraction, Note: a.Note.String}
	}
	return out, nil
}

// --- Config ---

func (r *Repo) GetConfig(key string) (string, error) {
	v, err := r.q.GetConfig(ctx, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (r *Repo) SetConfig(key, value string) error {
	return r.q.SetConfig(ctx, db.SetConfigParams{Key: key, Value: value})
}

func (r *Repo) AllConfig() (map[string]string, error) {
	rows, err := r.q.AllConfig(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, c := range rows {
		out[c.Key] = c.Value
	}
	return out, nil
}
