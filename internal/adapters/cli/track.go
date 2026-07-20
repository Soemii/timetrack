package cli

import (
	"fmt"
	"time"

	"timetrack/internal/core/service"
)

func (a *App) cmdStart(args []string) error {
	project := ""
	if len(args) > 0 {
		project = args[0]
	}
	if err := a.Svc.Start(project); err != nil {
		return err
	}
	now := a.Svc.Now().In(a.Loc)
	if project != "" {
		fmt.Fprintf(a.Stdout, "Gestartet um %s (Projekt %q).\n", now.Format("15:04"), project)
	} else {
		fmt.Fprintf(a.Stdout, "Gestartet um %s.\n", now.Format("15:04"))
	}
	return nil
}

func (a *App) cmdStop() error {
	warnings, err := a.Svc.Stop()
	if err != nil {
		return err
	}
	st, err := a.Svc.Status()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Gestoppt. Heute: %s Arbeit, %s Pause (Soll %s, %s).\n",
		hm(st.Today.Worked), hm(st.Today.Break), hm(st.Today.Target), saldo(st.Today.Diff))
	for _, w := range warnings {
		fmt.Fprintf(a.Stdout, "⚠ %s\n", w)
	}
	return nil
}

func (a *App) cmdPause() error {
	if err := a.Svc.Pause(); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Pause seit %s. Hinweis: zählt erst ab 15 Min. als Pause.\n",
		a.Svc.Now().In(a.Loc).Format("15:04"))
	return nil
}

func (a *App) cmdResume() error {
	if err := a.Svc.Resume(); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Weiter geht's (%s).\n", a.Svc.Now().In(a.Loc).Format("15:04"))
	return nil
}

func (a *App) cmdSwitch(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("Projektname fehlt: timetrack switch <projekt>")
	}
	if err := a.Svc.Switch(args[0]); err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Gewechselt zu %q um %s.\n", args[0], a.Svc.Now().In(a.Loc).Format("15:04"))
	return nil
}

func (a *App) cmdStatus() error {
	st, err := a.Svc.Status()
	if err != nil {
		return err
	}
	switch st.State {
	case service.StateWorking:
		p := st.Project
		if p == "" {
			p = "(ohne Projekt)"
		}
		fmt.Fprintf(a.Stdout, "▶ Am Arbeiten: %s seit %s (%s)\n", p, st.Since.In(a.Loc).Format("15:04"), hm(st.OpenDuration))
	case service.StatePaused:
		fmt.Fprintf(a.Stdout, "⏸ Pause seit %s (%s)", st.Since.In(a.Loc).Format("15:04"), hm(st.OpenDuration))
		if st.OpenDuration < 15*time.Minute {
			fmt.Fprint(a.Stdout, " — zählt erst ab 15 Min. als Pause")
		}
		fmt.Fprintln(a.Stdout)
	default:
		fmt.Fprintln(a.Stdout, "■ Nichts läuft.")
	}
	if st.LongSession {
		fmt.Fprintln(a.Stdout, "⚠ Achtung: Sitzung läuft seit über 12 Stunden — vergessen zu stoppen?")
	}
	t := st.Today
	fmt.Fprintf(a.Stdout, "Heute: %s Arbeit, %s Pause — Soll %s, %s\n",
		hm(t.Worked), hm(t.Break), hm(t.Target), saldo(t.Diff))
	if t.HolidayName != "" {
		fmt.Fprintf(a.Stdout, "Feiertag: %s\n", t.HolidayName)
	}
	if t.AbsenceType != "" {
		fmt.Fprintf(a.Stdout, "Abwesenheit: %s\n", absenceLabel(t.AbsenceType, t.Fraction))
	}
	if t.Diff < 0 && st.State != service.StateIdle {
		fmt.Fprintf(a.Stdout, "Heute noch: %s\n", hm(-t.Diff))
	}
	for _, w := range t.Warnings {
		fmt.Fprintf(a.Stdout, "⚠ %s\n", w)
	}
	fmt.Fprintf(a.Stdout, "Überstundensaldo gesamt: %s\n", saldo(st.Saldo))
	return nil
}
