package http

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	api "timetrack/internal/adapters/http/gen"
	"timetrack/internal/core/domain"
	"timetrack/internal/core/service"
)

//go:generate oapi-codegen --config ../../../api/oapi-config.yaml ../../../api/openapi.yaml

// Handler implementiert das generierte api.ServerInterface gegen den Service.
type Handler struct {
	Svc *service.Service
	Loc *time.Location
}

var _ api.ServerInterface = (*Handler)(nil)

// --- Helfer ---

func mins(d time.Duration) int { return int(d.Round(time.Minute).Minutes()) }

func oaDate(d domain.Date) openapi_types.Date {
	return openapi_types.Date{Time: d.Time(time.UTC)}
}

func toDate(od openapi_types.Date) domain.Date {
	y, m, d := od.Time.Date()
	return domain.Date{Year: y, Month: m, Day: d}
}

func optStr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusInternalServerError
	switch {
	case errors.Is(err, service.ErrConflict):
		code = http.StatusConflict
	case errors.Is(err, service.ErrNotInitialized):
		code = http.StatusConflict
	case strings.Contains(err.Error(), "nicht gefunden"):
		code = http.StatusNotFound
	}
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "konflikt: ")
	writeJSON(w, code, map[string]string{"message": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": "ungültiger Request-Body: " + err.Error()})
		return false
	}
	return true
}

func (h *Handler) daySummary(d domain.DaySummary) api.DaySummary {
	out := api.DaySummary{
		Date:          oaDate(d.Date),
		WorkedMinutes: mins(d.Worked),
		BreakMinutes:  mins(d.Break),
		TargetMinutes: mins(d.Target),
		CreditMinutes: mins(d.Credit),
		DiffMinutes:   mins(d.Diff),
		HolidayName:   optStr(d.HolidayName),
		Warnings:      d.Warnings,
	}
	if d.Warnings == nil {
		out.Warnings = []string{}
	}
	if d.AbsenceType != "" {
		s := string(d.AbsenceType)
		f := float32(d.Fraction)
		out.AbsenceType = &s
		out.Fraction = &f
	}
	return out
}

func (h *Handler) projectNames() (map[int64]string, error) {
	projects, err := h.Svc.Projects(true)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, p := range projects {
		names[p.ID] = p.Name
	}
	return names, nil
}

// --- Tracking ---

func (h *Handler) GetStatus(w http.ResponseWriter, r *http.Request) {
	st, err := h.Svc.Status()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := api.Status{
		State:        api.StatusState(st.State),
		TodaySummary: h.daySummary(st.Today),
		SaldoMinutes: mins(st.Saldo),
	}
	if st.State != service.StateIdle {
		out.Project = optStr(st.Project)
		since := st.Since
		om := mins(st.OpenDuration)
		ls := st.LongSession
		out.Since = &since
		out.OpenMinutes = &om
		out.LongSession = &ls
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) TrackingStart(w http.ResponseWriter, r *http.Request) {
	var body api.TrackingStartJSONRequestBody
	if r.ContentLength > 0 && !decode(w, r, &body) {
		return
	}
	if err := h.Svc.Start(deref(body.Project)); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TrackingStop(w http.ResponseWriter, r *http.Request) {
	warnings, err := h.Svc.Stop()
	if err != nil {
		writeErr(w, err)
		return
	}
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, http.StatusOK, map[string][]string{"warnings": warnings})
}

func (h *Handler) TrackingPause(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Pause(); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TrackingResume(w http.ResponseWriter, r *http.Request) {
	if err := h.Svc.Resume(); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) TrackingSwitch(w http.ResponseWriter, r *http.Request) {
	var body api.TrackingSwitchJSONRequestBody
	if !decode(w, r, &body) {
		return
	}
	if err := h.Svc.Switch(body.Project); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Entries ---

func (h *Handler) ListEntries(w http.ResponseWriter, r *http.Request, params api.ListEntriesParams) {
	entries, err := h.Svc.ListEntries(toDate(params.From), toDate(params.To))
	if err != nil {
		writeErr(w, err)
		return
	}
	names, err := h.projectNames()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]api.Entry, len(entries))
	for i, e := range entries {
		out[i] = api.Entry{
			Id:    e.ID,
			Kind:  api.EntryKind(e.Kind),
			Start: e.Start.In(h.Loc),
			Open:  e.Open,
			Note:  optStr(e.Note),
		}
		if !e.Open {
			end := e.End.In(h.Loc)
			out[i].End = &end
		}
		if e.ProjectID != nil {
			out[i].Project = optStr(names[*e.ProjectID])
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateEntry(w http.ResponseWriter, r *http.Request) {
	var body api.CreateEntryJSONRequestBody
	if !decode(w, r, &body) {
		return
	}
	id, err := h.Svc.AddEntry(domain.Kind(body.Kind), deref(body.Project), body.Start, body.End, deref(body.Note))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]int64{"id": id})
}

func (h *Handler) UpdateEntry(w http.ResponseWriter, r *http.Request, id int64) {
	var body api.UpdateEntryJSONRequestBody
	if !decode(w, r, &body) {
		return
	}
	patch := service.EntryPatch{
		Project: body.Project,
		Start:   body.Start,
		End:     body.End,
		Note:    body.Note,
	}
	if body.Kind != nil {
		k := domain.Kind(*body.Kind)
		patch.Kind = &k
	}
	if err := h.Svc.UpdateEntry(id, patch); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) DeleteEntry(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.Svc.DeleteEntry(id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Projects ---

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request, params api.ListProjectsParams) {
	include := params.IncludeArchived != nil && *params.IncludeArchived
	projects, err := h.Svc.Projects(include)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]api.Project, len(projects))
	for i, p := range projects {
		out[i] = api.Project{Id: p.ID, Name: p.Name, Archived: p.Archived}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request, id int64) {
	var body api.UpdateProjectJSONRequestBody
	if !decode(w, r, &body) {
		return
	}
	if body.Name != nil {
		if err := h.Svc.RenameProject(id, *body.Name); err != nil {
			writeErr(w, err)
			return
		}
	}
	if body.Archived != nil && *body.Archived {
		if err := h.Svc.ArchiveProject(id); err != nil {
			writeErr(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Absences ---

func (h *Handler) ListAbsences(w http.ResponseWriter, r *http.Request, params api.ListAbsencesParams) {
	absences, err := h.Svc.Absences(params.Year)
	if err != nil {
		writeErr(w, err)
		return
	}
	out := make([]api.Absence, len(absences))
	for i, a := range absences {
		out[i] = api.Absence{
			Id:       a.ID,
			Date:     oaDate(a.Date),
			Type:     api.AbsenceType(a.Type),
			Fraction: float32(a.Fraction),
			Note:     optStr(a.Note),
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) CreateAbsence(w http.ResponseWriter, r *http.Request) {
	var body api.CreateAbsenceJSONRequestBody
	if !decode(w, r, &body) {
		return
	}
	from := toDate(body.From)
	to := from
	if body.To != nil {
		to = toDate(*body.To)
	}
	fraction := 1.0
	if body.Half != nil && *body.Half {
		fraction = 0.5
	}
	added, err := h.Svc.AddAbsence(domain.AbsenceType(body.Type), from, to, fraction, deref(body.Note))
	if err != nil {
		writeErr(w, err)
		return
	}
	dates := make([]string, len(added))
	for i, d := range added {
		dates[i] = d.String()
	}
	writeJSON(w, http.StatusCreated, map[string][]string{"added": dates})
}

func (h *Handler) DeleteAbsence(w http.ResponseWriter, r *http.Request, id int64) {
	if err := h.Svc.DeleteAbsence(id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// --- Holidays / Report / Config ---

func (h *Handler) GetHolidays(w http.ResponseWriter, r *http.Request, params api.GetHolidaysParams) {
	set, err := h.Svc.Settings()
	if err != nil {
		writeErr(w, err)
		return
	}
	out := map[string]string{}
	for d, name := range domain.Holidays(params.Year, set.Land, set.Augsburg, set.Katholisch) {
		out[d.String()] = name
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) GetReport(w http.ResponseWriter, r *http.Request, params api.GetReportParams) {
	rep, err := h.Svc.Report(toDate(params.From), toDate(params.To))
	if err != nil {
		writeErr(w, err)
		return
	}
	out := api.Report{
		From:               oaDate(rep.From),
		To:                 oaDate(rep.To),
		TotalWorkedMinutes: mins(rep.TotalWorked),
		TotalTargetMinutes: mins(rep.TotalTarget),
		TotalCreditMinutes: mins(rep.TotalCredit),
		SaldoMinutes:       mins(rep.Saldo),
		Days:               make([]api.DaySummary, len(rep.Days)),
		Projects:           []api.ProjectShare{},
	}
	for i, d := range rep.Days {
		out.Days[i] = h.daySummary(d)
	}
	for pid, total := range rep.ProjectTotals {
		out.Projects = append(out.Projects, api.ProjectShare{
			Id:      pid,
			Name:    rep.ProjectNames[pid],
			Minutes: mins(total),
			Percent: float32(rep.ProjectPercent[pid]),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

var weekdayKeys = [7]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func (h *Handler) GetConfig(w http.ResponseWriter, r *http.Request) {
	set, err := h.Svc.Settings()
	if err != nil {
		writeErr(w, err)
		return
	}
	hours := map[string]float32{}
	for i, k := range weekdayKeys {
		hours[k] = float32(set.WeekdayHours[i].Hours())
	}
	aug, kath := set.Augsburg, set.Katholisch
	writeJSON(w, http.StatusOK, api.Config{
		WeeklyHours:  float32(set.WeeklyHours),
		WeekdayHours: hours,
		Bundesland:   string(set.Land),
		Augsburg:     &aug,
		Katholisch:   &kath,
		StartDate:    oaDate(set.StartDate),
	})
}

func (h *Handler) PutConfig(w http.ResponseWriter, r *http.Request) {
	var body api.PutConfigJSONRequestBody
	if !decode(w, r, &body) {
		return
	}
	land, err := domain.ParseBundesland(body.Bundesland)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"message": err.Error()})
		return
	}
	set := service.Settings{
		WeeklyHours: float64(body.WeeklyHours),
		Land:        land,
		Augsburg:    body.Augsburg != nil && *body.Augsburg,
		Katholisch:  body.Katholisch != nil && *body.Katholisch,
		StartDate:   toDate(body.StartDate),
	}
	for i, k := range weekdayKeys {
		set.WeekdayHours[i] = time.Duration(float64(body.WeekdayHours[k]) * float64(time.Hour))
	}
	if err := h.Svc.SaveSettings(set); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
