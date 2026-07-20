package cli

import (
	"flag"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func (a *App) cmdReport(args []string) error {
	fs := flag.NewFlagSet("report", flag.ContinueOnError)
	fs.SetOutput(a.Stdout)
	from, to, err := a.parseRange(fs, args, true)
	if err != nil {
		return err
	}
	rep, err := a.Svc.Report(from, to)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.Stdout, "Zeitraum %s bis %s\n\n", from, to)
	for _, d := range rep.Days {
		if d.Worked == 0 && d.Target == 0 && d.AbsenceType == "" && d.HolidayName == "" {
			continue // freie Tage ohne Aktivität überspringen
		}
		extra := ""
		if d.HolidayName != "" {
			extra = "  " + d.HolidayName
		}
		if d.AbsenceType != "" {
			extra += "  " + absenceLabel(d.AbsenceType, d.Fraction)
		}
		fmt.Fprintf(a.Stdout, "%s  Ist %5s  Pause %5s  Soll %5s  %6s%s\n",
			dayLabel(d.Date), hm(d.Worked), hm(d.Break), hm(d.Target), saldo(d.Diff), extra)
		for _, w := range d.Warnings {
			fmt.Fprintf(a.Stdout, "              ⚠ %s\n", w)
		}
	}
	fmt.Fprintf(a.Stdout, "\nSumme: Ist %s, Soll %s, Gutschrift %s → Saldo %s\n",
		hm(rep.TotalWorked), hm(rep.TotalTarget), hm(rep.TotalCredit), saldo(rep.Saldo))

	if len(rep.ProjectTotals) > 0 {
		fmt.Fprintln(a.Stdout, "\nProjekte:")
		type row struct {
			name string
			pct  float64
			id   int64
		}
		var rows []row
		for pid := range rep.ProjectTotals {
			rows = append(rows, row{rep.ProjectNames[pid], rep.ProjectPercent[pid], pid})
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].pct > rows[j].pct })
		for _, r := range rows {
			bar := strings.Repeat("█", int(r.pct/5+0.5))
			fmt.Fprintf(a.Stdout, "  %-20s %5.1f%%  %6s  %s\n", r.name, r.pct, hm(rep.ProjectTotals[r.id]), bar)
		}
	}
	return nil
}

func (a *App) cmdProject(args []string) error {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		projects, err := a.Svc.Projects(true)
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Fprintln(a.Stdout, "Keine Projekte. Anlegen mit 'timetrack start <projekt>'.")
			return nil
		}
		for _, p := range projects {
			suffix := ""
			if p.Archived {
				suffix = "  (archiviert)"
			}
			fmt.Fprintf(a.Stdout, "%4d  %s%s\n", p.ID, p.Name, suffix)
		}
		return nil
	case "rename":
		if len(args) < 3 {
			return fmt.Errorf("Aufruf: project rename <id> <name>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("ungültige ID %q", args[1])
		}
		if err := a.Svc.RenameProject(id, args[2]); err != nil {
			return err
		}
		fmt.Fprintln(a.Stdout, "Projekt umbenannt.")
		return nil
	case "archive":
		if len(args) < 2 {
			return fmt.Errorf("Aufruf: project archive <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return fmt.Errorf("ungültige ID %q", args[1])
		}
		if err := a.Svc.ArchiveProject(id); err != nil {
			return err
		}
		fmt.Fprintln(a.Stdout, "Projekt archiviert.")
		return nil
	default:
		return fmt.Errorf("unbekannter Unterbefehl %q (list, rename, archive)", args[0])
	}
}

func (a *App) cmdServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	fs.SetOutput(a.Stdout)
	port := fs.Int("port", 8090, "Port für die Weboberfläche")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if a.Serve == nil {
		return fmt.Errorf("Weboberfläche nicht verfügbar")
	}
	fmt.Fprintf(a.Stdout, "Weboberfläche läuft auf http://localhost:%d (Beenden mit Strg+C)\n", *port)
	return a.Serve(*port)
}
