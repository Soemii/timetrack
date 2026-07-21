package sqlite

import (
	"database/sql"
	"embed"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

//go:generate sqlc generate

// Open öffnet die Datenbank und bringt das Schema auf den neuesten Stand.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := runMigrations(sqlDB); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("migration fehlgeschlagen: %w", err)
	}
	return sqlDB, nil
}

func runMigrations(sqlDB *sql.DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return err
	}
	// Eigener Tabellenname: die DB-Datei kann ein fremdes schema_migrations
	// enthalten (z.B. von einem früheren Tool im selben Verzeichnis) — das
	// würde unsere Migration als "schon angewendet" erscheinen lassen.
	drv, err := migratesqlite.WithInstance(sqlDB, &migratesqlite.Config{MigrationsTable: "timetrack_migrations"})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", src, "sqlite", drv)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	// Sanity-Check: Kerntabelle muss existieren, sonst ist die Datei defekt.
	var n int
	if err := sqlDB.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='config'`).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("Datenbank enthält die timetrack-Tabellen nicht — Datei prüfen oder mit TIMETRACK_DB einen anderen Pfad wählen")
	}
	return nil
}
