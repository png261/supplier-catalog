package storage_test

import (
	"context"
	"testing"

	"coderpush/pkg/storage"
)

func TestStorage_Migrations(t *testing.T) {
	ctx := context.Background()

	store, err := storage.OpenInMemory()
	if err != nil {
		t.Fatalf("failed to open in-memory storage: %v", err)
	}
	defer store.Close()

	// Initial OpenInMemory should have applied all migrations up to version 2
	version, dirty, err := store.MigrationVersion(ctx)
	if err != nil {
		t.Fatalf("failed to get migration version: %v", err)
	}
	if dirty {
		t.Fatalf("expected clean schema, got dirty")
	}
	if version != 2 {
		t.Fatalf("expected schema version 2, got %d", version)
	}

	// Verify table existence by checking tables can be queried
	var count int
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM products;").Scan(&count); err != nil {
		t.Fatalf("products table not found or unqueryable: %v", err)
	}
	if err := store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM catalog_versions;").Scan(&count); err != nil {
		t.Fatalf("catalog_versions table not found or unqueryable: %v", err)
	}

	// Test MigrateToVersion down to 1
	if err := store.MigrateToVersion(ctx, 1); err != nil {
		t.Fatalf("failed migrating to version 1: %v", err)
	}
	version, dirty, err = store.MigrationVersion(ctx)
	if err != nil {
		t.Fatalf("failed checking version: %v", err)
	}
	if dirty || version != 1 {
		t.Fatalf("expected version 1 (clean), got version %d (dirty=%v)", version, dirty)
	}

	// catalog_versions table should no longer exist
	var versionTableExists int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='catalog_versions';").Scan(&versionTableExists)
	if err != nil {
		t.Fatalf("failed checking sqlite_master: %v", err)
	}
	if versionTableExists != 0 {
		t.Fatalf("expected catalog_versions to be dropped at version 1")
	}

	// products table should still exist at version 1
	var productsTableExists int
	err = store.DB().QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='products';").Scan(&productsTableExists)
	if err != nil {
		t.Fatalf("failed checking sqlite_master: %v", err)
	}
	if productsTableExists != 1 {
		t.Fatalf("expected products table to still exist at version 1")
	}

	// Test MigrateUp back to latest (version 2)
	if err := store.MigrateUp(ctx); err != nil {
		t.Fatalf("failed migrating up to latest: %v", err)
	}
	version, dirty, err = store.MigrationVersion(ctx)
	if err != nil {
		t.Fatalf("failed checking version: %v", err)
	}
	if dirty || version != 2 {
		t.Fatalf("expected version 2 after MigrateUp, got %d", version)
	}

	// Test MigrateDown (all migrations)
	if err := store.MigrateDown(ctx); err != nil {
		t.Fatalf("failed migrating down all: %v", err)
	}
	version, _, err = store.MigrationVersion(ctx)
	if err != nil {
		t.Fatalf("failed getting version after MigrateDown: %v", err)
	}
	if version != 0 {
		t.Fatalf("expected version 0 after MigrateDown, got %d", version)
	}

	// Re-apply up to verify clean recovery from version 0
	if err := store.MigrateUp(ctx); err != nil {
		t.Fatalf("failed migrating up after total down: %v", err)
	}
	version, _, err = store.MigrationVersion(ctx)
	if err != nil || version != 2 {
		t.Fatalf("expected version 2, got %d (err: %v)", version, err)
	}
}
