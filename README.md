# timetrack

Persönlicher Arbeitszeit-Tracker als ein Go-Binary: CLI + lokale Weboberfläche, SQLite-Speicher, deutsches Arbeitszeitrecht (ArbZG) eingebaut.

## Features

- **Tracking:** `start` / `stop` / `pause` / `resume` / `switch <projekt>` — Projekt-Zuordnung beim Start, Wechsel im Lauf, alles nachträglich änderbar.
- **Sollzeit:** Wochenstunden + abweichende Tage (z.B. Freitag kürzer), eingerichtet per Wizard.
- **ArbZG §4:** Pausen unter 15 Min. zählen automatisch als Arbeitszeit. Warnungen bei fehlender Pflichtpause (30 Min. ab 6h, 45 Min. über 9h) und über 10h/Tag (§3) — nur Hinweise, nichts wird blockiert.
- **Abwesenheiten:** Urlaub (auch halbe Tage), Krankheit, manuelle Feiertage — zählen als Soll erfüllt. Gesetzliche Feiertage werden automatisch pro Bundesland berechnet (alle 16, inkl. Augsburger Friedensfest und Mariä Himmelfahrt in BY).
- **Überstundensaldo:** fortlaufend ab konfigurierbarem Startdatum.
- **Reports:** Woche/Monat/Jahr/frei — Ist vs. Soll, Saldo, Projekt-Prozente.
- **Weboberfläche:** `timetrack serve` → http://localhost:8090 — voller Funktionsumfang, nur localhost, kein Login.

## Installation

```sh
go build -o timetrack ./cmd/timetrack
./timetrack init      # Wizard: Wochenstunden, Arbeitstage, Bundesland, ...
```

Daten liegen in `~/.timetrack/timetrack.db` (überschreibbar mit `TIMETRACK_DB=/pfad/zur.db`).

## Benutzung

```sh
timetrack start acme          # Arbeit starten (Projekt optional, wird automatisch angelegt)
timetrack pause               # Pause
timetrack resume              # weiter
timetrack switch intern       # Projekt wechseln
timetrack stop                # Feierabend — zeigt Tagesbilanz + ArbZG-Warnungen
timetrack status              # Zustand, Tagesübersicht, Saldo

timetrack add --date 2026-07-20 --from 09:00 --to 12:30 --project acme
timetrack edit 17 --to 13:00
timetrack delete 17
timetrack list --week         # zeigt IDs für edit/delete

timetrack absence add urlaub 2026-08-03 --to 2026-08-14   # Wochenenden werden übersprungen
timetrack absence add urlaub 2026-08-17 --half
timetrack absence add krank 2026-09-02
timetrack absence list        # inkl. Urlaubstage-Zähler

timetrack report --week       # auch --month, --year, --from/--to
timetrack config              # Einstellungen anzeigen (ändern: init erneut ausführen)
timetrack serve --port 8090   # Weboberfläche
```

## Architektur

Hexagonal, API-First:

- `api/openapi.yaml` — Quelle der Wahrheit für die HTTP-API, Server-Code generiert mit oapi-codegen (`internal/adapters/http/gen`).
- `internal/core/domain` — pure Businesslogik: Feiertage (Gauß-Formel), Pausenregeln, Tagesbewertung, Soll/Saldo. Keine I/O, am stärksten getestet.
- `internal/core/ports` — Repository-Interface.
- `internal/core/service` — Orchestrierung (Zustandsmaschine, Überlappungsprüfung).
- `internal/adapters/sqlite` — Migrationen (golang-migrate, embedded), Queries via sqlc (`gen/`), Repo.
- `internal/adapters/http` — generierter Server + Handler + eingebettete Web-UI (Vanilla JS).
- `internal/adapters/cli` — deutsche Kommandozeile; ruft den Service direkt, nicht über HTTP.

Codegen nach Änderungen an Spec/Queries: `go generate ./...` (braucht `sqlc` und `oapi-codegen` im PATH).

## Tests

```sh
go test ./...
```

Domain-Logik (Feiertage, §4-Reklassifizierung, DST, Mitternachts-Split, Soll/Saldo) table-driven; SQLite-Repo integrativ gegen echte Temp-DB; HTTP über httptest; CLI als Smoke-Test.
