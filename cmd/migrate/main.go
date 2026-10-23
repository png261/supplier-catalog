package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"coderpush/pkg/storage"
)

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)

	defaultDB := "catalog.db"
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		defaultDB = envDB
	}

	dbPath := fs.String("db", defaultDB, "Path to SQLite database file")
	action := fs.String("action", "up", "Migration action: up, down, version, goto, create")
	targetVersion := fs.Uint("version", 0, "Target version for 'goto' action")
	name := fs.String("name", "", "Migration name for 'create' action (e.g. add_users)")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *action == "create" {
		if *name == "" {
			return fmt.Errorf("migration name is required for create action (-name <name>)")
		}
		return createMigrationFiles(*name, stdout)
	}

	ctx := context.Background()
	store, err := storage.Open(*dbPath)
	if err != nil {
		return fmt.Errorf("failed to open database %s: %w", *dbPath, err)
	}
	defer store.Close()

	switch *action {
	case "up":
		if err := store.MigrateUp(ctx); err != nil {
			return fmt.Errorf("migrate up failed: %w", err)
		}
		ver, dirty, _ := store.MigrationVersion(ctx)
		fmt.Fprintf(stdout, "✅ Migrations applied successfully. Current version: %d (dirty: %v)\n", ver, dirty)

	case "down":
		if err := store.MigrateDown(ctx); err != nil {
			return fmt.Errorf("migrate down failed: %w", err)
		}
		ver, dirty, _ := store.MigrationVersion(ctx)
		fmt.Fprintf(stdout, "✅ Rollback complete. Current version: %d (dirty: %v)\n", ver, dirty)

	case "goto":
		if err := store.MigrateToVersion(ctx, *targetVersion); err != nil {
			return fmt.Errorf("migrate to version %d failed: %w", *targetVersion, err)
		}
		ver, dirty, _ := store.MigrationVersion(ctx)
		fmt.Fprintf(stdout, "✅ Schema migrated to version: %d (dirty: %v)\n", ver, dirty)

	case "version":
		ver, dirty, err := store.MigrationVersion(ctx)
		if err != nil {
			return fmt.Errorf("failed to inspect version: %w", err)
		}
		fmt.Fprintf(stdout, "Current schema version: %d (dirty: %v)\n", ver, dirty)

	default:
		return fmt.Errorf("unknown action '%s' (valid: up, down, goto, version, create)", *action)
	}

	return nil
}

func createMigrationFiles(name string, stdout io.Writer) error {
	migrationsDir := filepath.Join("pkg", "storage", "migrations")
	if err := os.MkdirAll(migrationsDir, 0755); err != nil {
		return fmt.Errorf("failed to ensure migrations directory: %w", err)
	}

	// Calculate next migration index based on existing files or timestamp
	files, _ := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	nextNum := (len(files) / 2) + 1
	prefix := fmt.Sprintf("%06d_%s", nextNum, name)

	upFile := filepath.Join(migrationsDir, prefix+".up.sql")
	downFile := filepath.Join(migrationsDir, prefix+".down.sql")

	header := fmt.Sprintf("-- Migration: %s\n-- Created: %s\n\n", name, time.Now().UTC().Format(time.RFC3339))
	if err := os.WriteFile(upFile, []byte(header+"-- Write UP SQL statements here\n"), 0644); err != nil {
		return fmt.Errorf("failed writing up migration: %w", err)
	}
	if err := os.WriteFile(downFile, []byte(header+"-- Write DOWN SQL statements here\n"), 0644); err != nil {
		return fmt.Errorf("failed writing down migration: %w", err)
	}

	fmt.Fprintf(stdout, "Created migration files:\n  - %s\n  - %s\n", upFile, downFile)
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
