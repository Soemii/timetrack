package cli

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"
	"time"

	"timetrack/internal/core/domain"
	"timetrack/internal/core/service"
)

var weekdayByName = map[string]time.Weekday{
	"mo": time.Monday, "di": time.Tuesday, "mi": time.Wednesday,
	"do": time.Thursday, "fr": time.Friday, "sa": time.Saturday, "so": time.Sunday,
}

func (a *App) prompt(r *bufio.Reader, question, def string) string {
	if def != "" {
		fmt.Fprintf(a.Stdout, "%s [%s]: ", question, def)
	} else {
		fmt.Fprintf(a.Stdout, "%s: ", question)
	}
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return def
	}
	return line
}

func (a *App) promptYesNo(r *bufio.Reader, question string) bool {
	ans := a.prompt(r, question+" (j/n)", "n")
	return strings.HasPrefix(strings.ToLower(ans), "j")
}

func (a *App) cmdInit() error {
	r := bufio.NewReader(a.Stdin)
	fmt.Fprintln(a.Stdout, "Einrichtung von timetrack")
	fmt.Fprintln(a.Stdout, "")

	weekly, err := strconv.ParseFloat(strings.ReplaceAll(a.prompt(r, "Wochenstunden", "40"), ",", "."), 64)
	if err != nil || weekly <= 0 || weekly > 80 {
		return fmt.Errorf("ungültige Wochenstunden")
	}

	daysStr := a.prompt(r, "Arbeitstage (z.B. Mo,Di,Mi,Do,Fr)", "Mo,Di,Mi,Do,Fr")
	var workdays []time.Weekday
	for _, part := range strings.Split(daysStr, ",") {
		wd, ok := weekdayByName[strings.ToLower(strings.TrimSpace(part))]
		if !ok {
			return fmt.Errorf("unbekannter Wochentag %q", part)
		}
		workdays = append(workdays, wd)
	}

	overrides := map[time.Weekday]time.Duration{}
	ovStr := a.prompt(r, "Abweichende Tage (z.B. Fr=6, leer = keine)", "")
	if ovStr != "" {
		for _, part := range strings.Split(ovStr, ",") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) != 2 {
				return fmt.Errorf("ungültige Abweichung %q (erwartet z.B. Fr=6)", part)
			}
			wd, ok := weekdayByName[strings.ToLower(strings.TrimSpace(kv[0]))]
			if !ok {
				return fmt.Errorf("unbekannter Wochentag %q", kv[0])
			}
			h, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(kv[1]), ",", "."), 64)
			if err != nil || h < 0 {
				return fmt.Errorf("ungültige Stundenzahl %q", kv[1])
			}
			overrides[wd] = time.Duration(h * float64(time.Hour))
		}
	}

	fmt.Fprintln(a.Stdout, "Bundesland (für Feiertage): BW BY BE BB HB HH HE MV NI NW RP SL SN ST SH TH")
	land, err := domain.ParseBundesland(strings.ToUpper(a.prompt(r, "Bundesland", "NW")))
	if err != nil {
		return err
	}
	augsburg, katholisch := false, false
	if land == domain.BY {
		augsburg = a.promptYesNo(r, "Stadt Augsburg (Friedensfest 8.8.)?")
		katholisch = a.promptYesNo(r, "Überwiegend katholische Gemeinde (Mariä Himmelfahrt 15.8.)?")
	}

	today := domain.DateOf(a.Svc.Now(), a.Loc)
	startStr := a.prompt(r, "Startdatum für den Überstundensaldo (JJJJ-MM-TT)", today.String())
	startDate, err := domain.ParseDate(startStr)
	if err != nil {
		return err
	}

	wh := domain.SpreadWeekly(time.Duration(weekly*float64(time.Hour)), workdays, overrides)
	if err := a.Svc.SaveSettings(service.Settings{
		WeeklyHours:  weekly,
		WeekdayHours: wh,
		Land:         land,
		Augsburg:     augsburg,
		Katholisch:   katholisch,
		StartDate:    startDate,
	}); err != nil {
		return err
	}

	fmt.Fprintln(a.Stdout, "")
	fmt.Fprintln(a.Stdout, "Eingerichtet. Tagessoll:")
	a.printWeekdayHours(wh)
	fmt.Fprintln(a.Stdout, "Los geht's mit 'timetrack start [projekt]'.")
	return nil
}

func (a *App) printWeekdayHours(wh domain.WeekdayHours) {
	order := []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
	for _, wd := range order {
		if wh[wd] > 0 {
			fmt.Fprintf(a.Stdout, "  %s: %s\n", weekdayShort[wd], hm(wh[wd]))
		}
	}
}

func (a *App) cmdConfig(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("Einstellungen ändern: einfach 'timetrack init' erneut ausführen (Daten bleiben erhalten)")
	}
	set, err := a.Svc.Settings()
	if err != nil {
		return err
	}
	fmt.Fprintf(a.Stdout, "Wochenstunden: %g\n", set.WeeklyHours)
	fmt.Fprintln(a.Stdout, "Tagessoll:")
	a.printWeekdayHours(set.WeekdayHours)
	fmt.Fprintf(a.Stdout, "Bundesland: %s (%s)\n", set.Land, domain.BundeslandNames[set.Land])
	if set.Land == domain.BY {
		fmt.Fprintf(a.Stdout, "Augsburg: %t, katholisch: %t\n", set.Augsburg, set.Katholisch)
	}
	fmt.Fprintf(a.Stdout, "Saldo-Startdatum: %s\n", set.StartDate)
	return nil
}
