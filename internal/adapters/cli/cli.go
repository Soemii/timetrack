package cli

import (
	"fmt"
	"io"
	"time"

	"timetrack/internal/core/service"
)

// App bündelt Abhängigkeiten der CLI.
type App struct {
	Svc    *service.Service
	Loc    *time.Location
	Stdin  io.Reader
	Stdout io.Writer
	// Serve wird vom main-Paket gesetzt (vermeidet cli→http-Abhängigkeit erst ab M5).
	Serve func(port int) error
}

const usage = `timetrack — Arbeitszeit-Tracker

Tracking:
  start [projekt]        Arbeit starten (Projekt optional)
  stop                   Arbeit beenden
  pause                  Pause beginnen
  resume                 Pause beenden, weiterarbeiten
  switch <projekt>       Projekt wechseln
                         Mehrere Projekte mit '+': start acme+intern
                         (Zeit wird gleichmäßig aufgeteilt)
  status                 Aktueller Zustand, Tagesübersicht, Saldo

Einträge:
  add --date D --from HH:MM --to HH:MM [--project P] [--type work|break] [--note N]
  edit <id> [--date D] [--from HH:MM] [--to HH:MM] [--project P] [--type T] [--note N]
  delete <id>
  list [--week|--month|--from D --to D]

Abwesenheiten:
  absence add <urlaub|krank|feiertag> <datum> [--to <datum>] [--half]
  absence list [jahr]
  absence delete <id>

Auswertung:
  report [--week|--month|--year|--from D --to D]

Projekte:
  project list | rename <id> <name> | archive <id>

Sonstiges:
  init                   Einrichtung (Wochenstunden, Bundesland, ...)
  config                 Einstellungen anzeigen
  serve [--port 8090]    Weboberfläche starten
`

// Run führt einen CLI-Aufruf aus und liefert den Exit-Code.
func (a *App) Run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.Stdout, usage)
		return 0
	}
	cmd, rest := args[0], args[1:]

	if cmd != "init" && cmd != "help" && cmd != "--help" && cmd != "-h" && !a.Svc.Initialized() {
		fmt.Fprintln(a.Stdout, "Noch nicht eingerichtet — bitte zuerst 'timetrack init' ausführen.")
		return 1
	}

	var err error
	switch cmd {
	case "help", "--help", "-h":
		fmt.Fprint(a.Stdout, usage)
	case "init":
		err = a.cmdInit()
	case "config":
		err = a.cmdConfig(rest)
	case "start":
		err = a.cmdStart(rest)
	case "stop":
		err = a.cmdStop()
	case "pause":
		err = a.cmdPause()
	case "resume":
		err = a.cmdResume()
	case "switch":
		err = a.cmdSwitch(rest)
	case "status":
		err = a.cmdStatus()
	case "add":
		err = a.cmdAdd(rest)
	case "edit":
		err = a.cmdEdit(rest)
	case "delete":
		err = a.cmdDelete(rest)
	case "list":
		err = a.cmdList(rest)
	case "absence":
		err = a.cmdAbsence(rest)
	case "report":
		err = a.cmdReport(rest)
	case "project":
		err = a.cmdProject(rest)
	case "serve":
		err = a.cmdServe(rest)
	default:
		fmt.Fprintf(a.Stdout, "Unbekannter Befehl %q — 'timetrack help' zeigt alle Befehle.\n", cmd)
		return 1
	}
	if err != nil {
		fmt.Fprintf(a.Stdout, "Fehler: %s\n", trimConflict(err))
		return 1
	}
	return 0
}

// trimConflict entfernt das technische "konflikt: "-Präfix aus Fehlermeldungen.
func trimConflict(err error) string {
	s := err.Error()
	const p = "konflikt: "
	if len(s) > len(p) && s[:len(p)] == p {
		return s[len(p):]
	}
	return s
}
