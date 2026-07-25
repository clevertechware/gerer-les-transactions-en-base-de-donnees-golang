package postgres

import (
	"errors"
	"fmt"

	"github.com/clevertechware/handling-db-transactions-in-golang/internal/config"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// RunMigrations runs database migrations in a single transaction
func RunMigrations(cfg config.Database, migrationsPath string) error {
	// Build DSN for migrating (using pgx5 driver)
	dsn := cfg.DSN()

	// Create migrate instance
	m, err := migrate.New(fmt.Sprintf("file://%s", migrationsPath), dsn)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}
	defer func(m *migrate.Migrate) {
		_, _ = m.Close()
	}(m)

	// Run migrations
	if err = m.Up(); err != nil {
		if errors.Is(err, migrate.ErrNoChange) {
			return nil
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Get current migration version
	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("failed to get migration version: %w", err)
	}

	if dirty {
		return fmt.Errorf("database is in dirty state at version %d", version)
	}
	return nil
}
