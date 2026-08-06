package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"timetrack/internal/adapters/cli"
	web "timetrack/internal/adapters/http"
	"timetrack/internal/adapters/sqlite"
	"timetrack/internal/core/service"
	"timetrack/internal/update"
)

// version wird von GoReleaser via -X main.version gesetzt.
var version = "dev"

func main() {
	update.CleanupOld()

	dbPath := os.Getenv("TIMETRACK_DB")
	if dbPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		dir := filepath.Join(home, ".timetrack")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "Fehler:", err)
			os.Exit(1)
		}
		dbPath = filepath.Join(dir, "timetrack.db")
	}

	sqlDB, err := sqlite.Open(dbPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
	defer sqlDB.Close()

	svc := service.New(sqlite.NewRepo(sqlDB), time.Local)
	app := &cli.App{
		Svc:     svc,
		Loc:     time.Local,
		Stdin:   os.Stdin,
		Stdout:  os.Stdout,
		Serve:   func(port int) error { return web.Serve(svc, time.Local, port) },
		Version: version,
		Update:  func() error { return update.Run(version, os.Stdout) },
	}
	update.MaybeNotify(version, os.Stderr)
	os.Exit(app.Run(os.Args[1:]))
}
