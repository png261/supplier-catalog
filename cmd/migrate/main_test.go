package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMigrateCLI(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")

	var stdout, stderr bytes.Buffer

	// 1. Initial migrate up
	err := run([]string{"-db", dbPath, "-action", "up"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run up failed: %v, stderr: %s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Current version: 2") {
		t.Errorf("expected version 2 in stdout, got: %s", stdout.String())
	}

	// 2. Check version
	stdout.Reset()
	stderr.Reset()
	err = run([]string{"-db", dbPath, "-action", "version"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run version failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Current schema version: 2") {
		t.Errorf("expected version 2, got: %s", stdout.String())
	}

	// 3. Migrate goto version 1
	stdout.Reset()
	stderr.Reset()
	err = run([]string{"-db", dbPath, "-action", "goto", "-version", "1"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run goto failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Schema migrated to version: 1") {
		t.Errorf("expected version 1, got: %s", stdout.String())
	}

	// 4. Migrate down
	stdout.Reset()
	stderr.Reset()
	err = run([]string{"-db", dbPath, "-action", "down"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run down failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Rollback complete. Current version: 0") {
		t.Errorf("expected version 0, got: %s", stdout.String())
	}

	// 5. Test create action
	stdout.Reset()
	stderr.Reset()
	err = run([]string{"-action", "create", "-name", "test_migration"}, &stdout, &stderr)
	if err != nil {
		t.Fatalf("run create failed: %v", err)
	}
	if !strings.Contains(stdout.String(), "Created migration files:") {
		t.Errorf("expected created message, got: %s", stdout.String())
	}
	// Clean up created test migration files
	matches, _ := filepath.Glob(filepath.Join("pkg", "storage", "migrations", "*test_migration*"))
	for _, f := range matches {
		_ = os.Remove(f)
	}

	// 6. Test invalid action
	stdout.Reset()
	stderr.Reset()
	err = run([]string{"-db", dbPath, "-action", "nonexistent"}, &stdout, &stderr)
	if err == nil {
		t.Errorf("expected error for invalid action")
	}

	// 7. Test create missing name
	stdout.Reset()
	stderr.Reset()
	err = run([]string{"-action", "create"}, &stdout, &stderr)
	if err == nil {
		t.Errorf("expected error for create with missing name")
	}
}
