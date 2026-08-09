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

	if err := a.promptOutlook(r); err != nil {
		return err
	}
	if err := a.promptJira(r); err != nil {
		return err
	}

	fmt.Fprintln(a.Stdout, "")
	fmt.Fprintln(a.Stdout, "Eingerichtet. Tagessoll:")
	a.printWeekdayHours(wh)
	fmt.Fprintln(a.Stdout, "Los geht's mit 'timetrack start [projekt]'.")
	return nil
}

// promptOutlook fragt ICS-URL und Projekt-Regeln ab. Bestehende Werte sind
// die Defaults (Re-Init behält die Konfiguration); "-" löscht die URL.
func (a *App) promptOutlook(r *bufio.Reader) error {
	set, err := a.Svc.OutlookSettings()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "")
	fmt.Fprintln(a.Stdout, "Outlook-Import (optional): veröffentlichte Kalender-URL (.ics), \"-\" entfernt sie")
	url := a.prompt(r, "ICS-Abo-URL", set.URL)
	if url == "-" {
		url = ""
	}
	rules := set.Rules
	if url != "" {
		var parts []string
		for _, ru := range set.Rules {
			parts = append(parts, ru.Contains+"="+ru.Project)
		}
		rulesStr := a.prompt(r, "Projekt-Regeln (z.B. sprint=Scrum,review=QA)", strings.Join(parts, ","))
		rules = nil
		if rulesStr != "" && rulesStr != "-" {
			for _, part := range strings.Split(rulesStr, ",") {
				kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
				if len(kv) != 2 {
					return fmt.Errorf("ungültige Regel %q (erwartet z.B. sprint=Scrum)", part)
				}
				rules = append(rules, service.OutlookRule{
					Contains: strings.TrimSpace(kv[0]), Project: strings.TrimSpace(kv[1]),
				})
			}
		}
	}
	return a.Svc.SaveOutlookSettings(url, rules)
}

// promptJira fragt JIRA-URL und Personal Access Token ab (Server/DC).
// Bestehende URL ist der Default; "-" löscht sie. Leerer Token bei gesetztem
// Token behält den alten. ponytail: Echo beim Token-Prompt; x/term erst bei Bedarf.
func (a *App) promptJira(r *bufio.Reader) error {
	set, err := a.Svc.JiraSettings()
	if err != nil {
		return err
	}
	fmt.Fprintln(a.Stdout, "")
	fmt.Fprintln(a.Stdout, "JIRA-Import (optional, Server/DC): dir zugewiesene Issues werden Aufgaben, \"-\" entfernt die URL")
	url := a.prompt(r, "JIRA-Basis-URL", set.BaseURL)
	if url == "-" {
		return a.Svc.SaveJiraSettings("", "")
	}
	token := ""
	if url != "" {
		def := ""
		if set.TokenSet {
			def = "(gesetzt)"
		}
		token = a.prompt(r, "Personal Access Token", def)
		if token == "(gesetzt)" {
			token = "" // Default übernommen = alten Token behalten
		}
		fmt.Fprintln(a.Stdout, "Zuordnung: im Web-UI (Projekte-Tab) pro Projekt den JIRA-Projekt-Key hinterlegen.")
	}
	return a.Svc.SaveJiraSettings(url, token)
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
	ol, err := a.Svc.OutlookSettings()
	if err != nil {
		return err
	}
	if ol.URL == "" {
		fmt.Fprintln(a.Stdout, "Outlook-Import: aus")
	} else {
		fmt.Fprintf(a.Stdout, "Outlook-ICS-URL: %s\n", ol.URL)
		for _, ru := range ol.Rules {
			fmt.Fprintf(a.Stdout, "  Regel: Betreff enthält %q → Projekt %s\n", ru.Contains, ru.Project)
		}
		if ol.LastErr != "" {
			fmt.Fprintf(a.Stdout, "Letzter Outlook-Abruf fehlgeschlagen: %s\n", ol.LastErr)
		}
	}
	ji, err := a.Svc.JiraSettings()
	if err != nil {
		return err
	}
	if ji.BaseURL == "" {
		fmt.Fprintln(a.Stdout, "JIRA-Import: aus")
		return nil
	}
	fmt.Fprintf(a.Stdout, "JIRA-URL: %s (Token: %s)\n", ji.BaseURL, map[bool]string{true: "gesetzt", false: "fehlt"}[ji.TokenSet])
	if ji.LastErr != "" {
		fmt.Fprintf(a.Stdout, "Letzter JIRA-Abruf fehlgeschlagen: %s\n", ji.LastErr)
	}
	return nil
}
