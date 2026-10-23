package storage

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

//go:embed migrations/*.sql
var migrationFS embed.FS

// NewMigrator returns a configured golang-migrate instance using embedded migration files
// and the provided SQLite database connection.
// Note: Do NOT call migrator.Close() if the underlying *sql.DB should remain open,
// because golang-migrate's sqlite driver closes the database connection on Close().
func NewMigrator(db *sql.DB) (*migrate.Migrate, error) {
	sourceDriver, err := iofs.New(migrationFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("failed to create migration source driver: %w", err)
	}

	dbDriver, err := sqlite.WithInstance(db, &sqlite.Config{
		MigrationsTable: sqlite.DefaultMigrationsTable,
		NoTxWrap:        false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create migration database driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "sqlite", dbDriver)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize migrator: %w", err)
	}

	return m, nil
}

// RunMigrations applies all pending migrations to the database.
func RunMigrations(db *sql.DB) error {
	m, err := NewMigrator(db)
	if err != nil {
		return err
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to apply migrations: %w", err)
	}
	return nil
}

// MigrateUp applies all available migrations to the database.
func (s *SQLiteStorage) MigrateUp(ctx context.Context) error {
	return RunMigrations(s.db)
}

// MigrateDown rolls back all migrations.
func (s *SQLiteStorage) MigrateDown(ctx context.Context) error {
	m, err := NewMigrator(s.db)
	if err != nil {
		return err
	}

	if err := m.Down(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to rollback migrations: %w", err)
	}
	return nil
}

// MigrateToVersion migrates the database schema to a specified version number.
func (s *SQLiteStorage) MigrateToVersion(ctx context.Context, version uint) error {
	m, err := NewMigrator(s.db)
	if err != nil {
		return err
	}

	if err := m.Migrate(version); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("failed to migrate to version %d: %w", version, err)
	}
	return nil
}

// MigrationVersion returns the current schema version and whether the schema is dirty.
func (s *SQLiteStorage) MigrationVersion(ctx context.Context) (uint, bool, error) {
	m, err := NewMigrator(s.db)
	if err != nil {
		return 0, false, err
	}

	version, dirty, err := m.Version()
	if err != nil {
		if errors.Is(err, migrate.ErrNilVersion) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("failed to get migration version: %w", err)
	}
	return version, dirty, nil
}
