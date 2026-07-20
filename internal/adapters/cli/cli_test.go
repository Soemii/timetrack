package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"timetrack/internal/adapters/sqlite"
	"timetrack/internal/core/service"
)

func TestSmoke(t *testing.T) {
	sqlDB, err := sqlite.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	berlin, _ := time.LoadLocation("Europe/Berlin")
	svc := service.New(sqlite.NewRepo(sqlDB), berlin)
	now := time.Date(2026, 7, 20, 9, 0, 0, 0, berlin) // Montag
	svc.Now = func() time.Time { return now }

	var out bytes.Buffer
	app := &App{Svc: svc, Loc: berlin, Stdout: &out}

	// Ohne Init: Hinweis + Exit 1
	if code := app.Run([]string{"start"}); code != 1 {
		t.Fatalf("start ohne init: Exit %d, want 1", code)
	}
	if !strings.Contains(out.String(), "timetrack init") {
		t.Errorf("fehlender Init-Hinweis: %s", out.String())
	}

	// Init über gepipte Antworten: 40h, Mo-Fr, Fr=6, BY, kein Augsburg/kath., Standard-Startdatum
	out.Reset()
	app.Stdin = strings.NewReader("40\n\nFr=6\nBY\nn\nn\n\n")
	if code := app.Run([]string{"init"}); code != 0 {
		t.Fatalf("init: Exit %d: %s", code, out.String())
	}

	run := func(want int, args ...string) string {
		out.Reset()
		if code := app.Run(args); code != want {
			t.Fatalf("%v: Exit %d, want %d: %s", args, code, want, out.String())
		}
		return out.String()
	}

	run(0, "start", "acme")
	now = now.Add(6*time.Hour + 30*time.Minute)
	got := run(0, "stop")
	if !strings.Contains(got, "6:30 Arbeit") {
		t.Errorf("stop: %s", got)
	}
	if !strings.Contains(got, "Gesetzliche Pause") {
		t.Errorf("Pausenwarnung fehlt: %s", got)
	}
	got = run(0, "report", "--week")
	if !strings.Contains(got, "Soll 40:00") || !strings.Contains(got, "acme") {
		t.Errorf("report: %s", got)
	}
	// Doppelstop → Fehler
	run(1, "stop")
}
