package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/service"
)

func (a *App) parseClock(day domain.Date, clock string) (time.Time, error) {
	t, err := time.Parse("15:04", clock)
	if err != nil {
		return time.Time{}, fmt.Errorf("ungültige Uhrzeit %q (erwartet HH:MM)", clock)
	}
	return time.Date(day.Year, day.Month, day.Day, t.Hour(), t.Minute(), 0, 0, a.Loc), nil
}

func (a *App) cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	fs.SetOutput(a.Stdout)
	date := fs.String("date", "", "Datum (JJJJ-MM-TT, Standard: heute)")
	from := fs.String("from", "", "Beginn (HH:MM)")
	to := fs.String("to", "", "Ende (HH:MM)")
	project := fs.String("project", "", "Projekt")
	typ := fs.String("type", "work", "work oder break")
	note := fs.String("note", "", "Notiz")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *from == "" || *to == "" {
		return fmt.Errorf("--from und --to sind erforderlich")
	}
	day := domain.DateOf(a.Svc.Now(), a.Loc)
	if *date != "" {
		var err error
		if day, err = domain.ParseDate(*date); err != nil {
			return err
		}
	}
	start, err := a.parseClock(day, *from)
	if err != nil {
		return err
	}
	end, err := a.parseClock(day, *to)
	if err != nil {
		return err
	}
	if !end.After(start) {
		end = end.AddDate(0, 0, 1) // über Mitternacht: Ende am Folgetag
	}
	kind := domain.Kind(*typ)
	if kind != domain.KindWork && kind != domain.KindBreak {
		return fmt.Errorf("ungültiger Typ %q (work oder break)", *typ)
	}
	id, err := a.Svc.AddEntry(kind, *project, start, end, *note)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Eintrag #%d angelegt: %s %s–%s.\n", id, dayLabel(day),
		start.In(a.Loc).Format("15:04"), end.In(a.Loc).Format("15:04"))
	return nil
}

func (a *App) cmdEdit(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("ID fehlt: timetrack edit <id> [--flags]")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("ungültige ID %q", args[0])
	}
	fs := flag.NewFlagSet("edit", flag.ContinueOnError)
	fs.SetOutput(a.Stdout)
	date := fs.String("date", "", "Datum (JJJJ-MM-TT)")
	from := fs.String("from", "", "Beginn (HH:MM)")
	to := fs.String("to", "", "Ende (HH:MM)")
	project := fs.String("project", "", "Projekt (leer lassen = unverändert)")
	typ := fs.String("type", "", "work oder break")
	note := fs.String("note", "", "Notiz")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	// Basis: bestehender Eintrag bestimmt den Tag, wenn --date fehlt.
	entries, err := a.Svc.ListEntries(domain.Date{Year: 1970, Month: 1, Day: 1}, domain.DateOf(a.Svc.Now(), a.Loc).AddDays(1))
	if err != nil {
		return err
	}
	var cur *domain.Segment
	for i := range entries {
		if entries[i].ID == id {
			cur = &entries[i]
		}
	}
	if cur == nil {
		return fmt.Errorf("Eintrag #%d nicht gefunden", id)
	}
	day := domain.DateOf(cur.Start, a.Loc)
	if *date != "" {
		if day, err = domain.ParseDate(*date); err != nil {
			return err
		}
	}

	var patch service.EntryPatch
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })

	if set["date"] || set["from"] {
		clock := cur.Start.In(a.Loc).Format("15:04")
		if set["from"] {
			clock = *from
		}
		start, err := a.parseClock(day, clock)
		if err != nil {
			return err
		}
		patch.Start = &start
	}
	if set["date"] || set["to"] {
		if cur.Open && !set["to"] {
			return fmt.Errorf("Eintrag #%d läuft noch — Ende mit --to setzen", id)
		}
		clock := ""
		if set["to"] {
			clock = *to
		} else {
			clock = cur.End.In(a.Loc).Format("15:04")
		}
		end, err := a.parseClock(day, clock)
		if err != nil {
			return err
		}
		if patch.Start != nil && !end.After(*patch.Start) {
			end = end.AddDate(0, 0, 1)
		}
		patch.End = &end
	}
	if set["project"] {
		patch.Project = project
	}
	if set["type"] {
		k := domain.Kind(*typ)
		if k != domain.KindWork && k != domain.KindBreak {
			return fmt.Errorf("ungültiger Typ %q (work oder break)", *typ)
		}
		patch.Kind = &k
	}
	if set["note"] {
		patch.Note = note
	}
	if err := a.Svc.UpdateEntry(id, patch); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Eintrag #%d geändert.\n", id)
	return nil
}

func (a *App) cmdDelete(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("ID fehlt: timetrack delete <id>")
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil {
		return fmt.Errorf("ungültige ID %q", args[0])
	}
	if err := a.Svc.DeleteEntry(id); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Eintrag #%d gelöscht.\n", id)
	return nil
}

// parseRange liest --week/--month/--year/--from/--to und liefert den Zeitraum.
func (a *App) parseRange(fs *flag.FlagSet, args []string, allowYear bool) (domain.Date, domain.Date, error) {
	week := fs.Bool("week", false, "aktuelle Woche")
	month := fs.Bool("month", false, "aktueller Monat")
	var year *bool
	if allowYear {
		year = fs.Bool("year", false, "aktuelles Jahr")
	}
	from := fs.String("from", "", "von (JJJJ-MM-TT)")
	to := fs.String("to", "", "bis (JJJJ-MM-TT)")
	if err := fs.Parse(args); err != nil {
		return domain.Date{}, domain.Date{}, err
	}
	today := domain.DateOf(a.Svc.Now(), a.Loc)
	switch {
	case *from != "" || *to != "":
		if *from == "" || *to == "" {
			return domain.Date{}, domain.Date{}, fmt.Errorf("--from und --to müssen zusammen angegeben werden")
		}
		f, err := domain.ParseDate(*from)
		if err != nil {
			return domain.Date{}, domain.Date{}, err
		}
		t, err := domain.ParseDate(*to)
		if err != nil {
			return domain.Date{}, domain.Date{}, err
		}
		return f, t, nil
	case *month:
		first := domain.Date{Year: today.Year, Month: today.Month, Day: 1}
		last := time.Date(today.Year, today.Month+1, 1, 12, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
		return first, domain.Date{Year: last.Year(), Month: last.Month(), Day: last.Day()}, nil
	case allowYear && *year:
		return domain.Date{Year: today.Year, Month: 1, Day: 1}, domain.Date{Year: today.Year, Month: 12, Day: 31}, nil
	default:
		_ = week // Standard ist die aktuelle Woche
		monday := today
		for monday.Weekday() != time.Monday {
			monday = monday.AddDays(-1)
		}
		return monday, monday.AddDays(6), nil
	}
}

func (a *App) cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	fs.SetOutput(a.Stdout)
	from, to, err := a.parseRange(fs, args, false)
	if err != nil {
		return err
	}
	entries, err := a.Svc.ListEntries(from, to)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintf(a.Stdout, "Keine Einträge von %s bis %s.\n", from, to)
		return nil
	}
	rep, err := a.Svc.Report(from, to)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "%4s  %-10s %-11s %-7s %-6s %-20s %s\n", "ID", "Datum", "Zeit", "Dauer", "Typ", "Projekt", "Notiz")
	for _, e := range entries {
		end := "…läuft"
		dur := a.Svc.Now().Sub(e.Start)
		if !e.Open {
			end = e.End.In(a.Loc).Format("15:04")
			dur = e.Duration()
		}
		project := ""
		if e.ProjectID != nil {
			project = rep.ProjectNames[*e.ProjectID]
		}
		fmt.Fprintf(a.Stdout, "%4d  %-10s %s–%-6s %-7s %-6s %-20s %s\n",
			e.ID, dayLabel(domain.DateOf(e.Start, a.Loc)),
			e.Start.In(a.Loc).Format("15:04"), end, hm(dur), kindLabel(e.Kind), project, e.Note)
	}
	return nil
}

func (a *App) cmdAbsence(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("Unterbefehl fehlt: absence add|list|delete")
	}
	switch args[0] {
	case "add":
		return a.cmdAbsenceAdd(args[1:])
	case "list":
		year := domain.DateOf(a.Svc.Now(), a.Loc).Year
		if len(args) > 1 {
			y, err := strconv.Atoi(args[1])
			if err != nil {
				return fmt.Errorf("ungültiges Jahr %q", args[1])
			}
			year = y
		}
		absences, err := a.Svc.Absences(year)
		if err != nil {
			return err
		}
		if len(absences) == 0 {
			fmt.Fprintf(a.Stdout, "Keine Abwesenheiten in %d.\n", year)
			return nil
		}
		var vacation float64
		for _, ab := range absences {
			fmt.Fprintf(a.Stdout, "%4d  %s  %s  %s\n", ab.ID, ab.Date, absenceLabel(ab.Type, ab.Fraction), ab.Note)
			if ab.Type == domain.AbsenceUrlaub {
				vacation += ab.Fraction
			}
		}
		fmt.Fprintf(a.Stdout, "Urlaubstage %d: %g\n", year, vacation)
		return nil
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("ID fehlt: absence delete <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("ungültige ID %q", args[1])
		}
		if err := a.Svc.DeleteAbsence(id); err != nil {
			return err
		}
		fmt.Fprintln(a.Stdout, "Abwesenheit gelöscht.")
		return nil
	default:
		return fmt.Errorf("unbekannter Unterbefehl %q", args[0])
	}
}

func (a *App) cmdAbsenceAdd(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("Aufruf: absence add <urlaub|krank|feiertag> <datum> [--to <datum>] [--half]")
	}
	typ := domain.AbsenceType(strings.ToLower(args[0]))
	if typ != domain.AbsenceUrlaub && typ != domain.AbsenceKrank && typ != domain.AbsenceFeiertag {
		return fmt.Errorf("ungültiger Typ %q (urlaub, krank oder feiertag)", args[0])
	}
	from, err := domain.ParseDate(args[1])
	if err != nil {
		return err
	}
	fs := flag.NewFlagSet("absence add", flag.ContinueOnError)
	fs.SetOutput(a.Stdout)
	toStr := fs.String("to", "", "bis (JJJJ-MM-TT)")
	half := fs.Bool("half", false, "halber Tag")
	note := fs.String("note", "", "Notiz")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	to := from
	if *toStr != "" {
		if to, err = domain.ParseDate(*toStr); err != nil {
			return err
		}
	}
	fraction := 1.0
	if *half {
		fraction = 0.5
	}
	added, err := a.Svc.AddAbsence(typ, from, to, fraction, *note)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "%d Tag(e) %s eingetragen.\n", len(added), absenceLabel(typ, fraction))
	return nil
}
