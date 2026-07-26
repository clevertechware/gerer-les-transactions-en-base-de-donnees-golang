// Package migrate applies the SQL migrations. It is separate from the postgres
// package so the test helpers can use it without importing the code they test.
package migrate

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"

	"github.com/clevertechware/gerer-les-transactions-en-base-de-donnees/internal/config"
)

// Up applies every pending migration found in migrationsPath.
// golang-migrate wraps each migration file in its own transaction.
func Up(cfg config.Postgres, migrationsPath string) error {
	m, err := open(cfg, migrationsPath)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	if err = m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("running migrations: %w", err)
	}

	return checkVersion(m)
}

// Down reverts every applied migration. Used to prove the down files actually
// undo what the up files did, which nothing else exercises.
func Down(cfg config.Postgres, migrationsPath string) error {
	m, err := open(cfg, migrationsPath)
	if err != nil {
		return err
	}
	defer closeMigrate(m)

	if err = m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("reverting migrations: %w", err)
	}

	return checkVersion(m)
}

func open(cfg config.Postgres, migrationsPath string) (*migrate.Migrate, error) {
	m, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), cfg.MigrateURL())
	if err != nil {
		return nil, fmt.Errorf("creating migrate instance: %w", err)
	}
	return m, nil
}

func closeMigrate(m *migrate.Migrate) {
	_, _ = m.Close()
}

// checkVersion refuses to report success while the database is half-migrated.
func checkVersion(m *migrate.Migrate) error {
	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("reading migration version: %w", err)
	}
	if dirty {
		return fmt.Errorf("database is in a dirty state at version %d", version)
	}
	return nil
}
