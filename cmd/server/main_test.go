package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/storage"
)

func getFreePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to listen on free port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func TestParseFlags_DefaultsAndCustom(t *testing.T) {
	t.Run("Default flags", func(t *testing.T) {
		var stderr bytes.Buffer
		opts, err := ParseFlags([]string{}, &stderr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.Port != 8080 {
			t.Errorf("expected port 8080, got %d", opts.Port)
		}
		if opts.DBPath != "catalog.db" {
			t.Errorf("expected db catalog.db, got %s", opts.DBPath)
		}
		if opts.StaticDir != "" {
			t.Errorf("expected empty static dir, got %s", opts.StaticDir)
		}
	})

	t.Run("Custom flags", func(t *testing.T) {
		var stderr bytes.Buffer
		args := []string{"--port", "9090", "--db", "/tmp/custom.db", "--static-dir", "/tmp/static"}
		opts, err := ParseFlags(args, &stderr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.Port != 9090 {
			t.Errorf("expected port 9090, got %d", opts.Port)
		}
		if opts.DBPath != "/tmp/custom.db" {
			t.Errorf("expected db /tmp/custom.db, got %s", opts.DBPath)
		}
		if opts.StaticDir != "/tmp/static" {
			t.Errorf("expected static dir /tmp/static, got %s", opts.StaticDir)
		}
	})

	t.Run("Environment PORT override", func(t *testing.T) {
		t.Setenv("PORT", "7777")
		var stderr bytes.Buffer
		opts, err := ParseFlags([]string{}, &stderr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.Port != 7777 {
			t.Errorf("expected port 7777 from env, got %d", opts.Port)
		}
	})

	t.Run("Environment DB_PATH override", func(t *testing.T) {
		t.Setenv("DB_PATH", "/tmp/env_catalog.db")
		var stderr bytes.Buffer
		opts, err := ParseFlags([]string{}, &stderr)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if opts.DBPath != "/tmp/env_catalog.db" {
			t.Errorf("expected db /tmp/env_catalog.db from env, got %s", opts.DBPath)
		}
	})

	t.Run("Invalid ports", func(t *testing.T) {
		invalidPorts := []string{"0", "-1", "70000"}
		for _, p := range invalidPorts {
			var stderr bytes.Buffer
			_, err := ParseFlags([]string{"--port", p}, &stderr)
			if err == nil {
				t.Errorf("expected error for port %s, got nil", p)
			}
		}
	})

	t.Run("Unknown flag", func(t *testing.T) {
		var stderr bytes.Buffer
		_, err := ParseFlags([]string{"--nonexistent"}, &stderr)
		if err == nil {
			t.Error("expected error for unknown flag, got nil")
		}
	})
}

func TestRunMain_ExitCodes(t *testing.T) {
	ctx := context.Background()

	t.Run("Flag parse error returns code 2", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := runMainWithContext(ctx, []string{"--invalid-flag"}, &stdout, &stderr)
		if code != 2 {
			t.Errorf("expected exit code 2, got %d", code)
		}
	})

	t.Run("Storage failure returns code 1", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		code := runMainWithContext(ctx, []string{"--db", "/dev/null/cannot/create/db.sqlite"}, &stdout, &stderr)
		if code != 1 {
			t.Errorf("expected exit code 1, got %d", code)
		}
	})
}

func TestServer_StartupRecoveryAndGracefulShutdown(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_catalog.db")

	// 1. Seed an active RUNNING job in database before server start
	ctx := context.Background()
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	now := time.Now().UTC()
	staleJob := &domain.SyncJob{
		JobID:               "job-crash-recovery-test",
		Status:              domain.JobStatusRunning,
		TotalBatches:        10,
		CommittedBatchIndex: 3,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	if err := store.CreateJob(ctx, staleJob); err != nil {
		t.Fatalf("failed to create stale job: %v", err)
	}
	_ = store.Close()

	// 2. Pick a free port and start server
	port := getFreePort(t)
	serverCtx, serverCancel := context.WithCancel(context.Background())
	defer serverCancel()

	var stdout, stderr bytes.Buffer
	doneCh := make(chan int, 1)

	go func() {
		args := []string{
			"--port", fmt.Sprintf("%d", port),
			"--db", dbPath,
		}
		doneCh <- runMainWithContext(serverCtx, args, &stdout, &stderr)
	}()

	// 3. Poll server until active
	client := &http.Client{Timeout: 1 * time.Second}
	url := fmt.Sprintf("http://127.0.0.1:%d/api/jobs", port)
	ready := false
	for i := 0; i < 30; i++ {
		time.Sleep(100 * time.Millisecond)
		resp, err := client.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				ready = true
				break
			}
		}
	}
	if !ready {
		t.Fatalf("server failed to become ready on port %d: stdout=%s stderr=%s", port, stdout.String(), stderr.String())
	}

	// 4. Verify root dashboard endpoint serves index.html
	rootResp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
	if err != nil {
		t.Fatalf("failed to fetch root dashboard: %v", err)
	}
	_ = rootResp.Body.Close()
	if rootResp.StatusCode != http.StatusOK {
		t.Errorf("expected 200 OK for root dashboard, got %d", rootResp.StatusCode)
	}

	// 5. Trigger graceful shutdown via context cancellation
	serverCancel()

	select {
	case code := <-doneCh:
		if code != 0 {
			t.Errorf("expected exit code 0 on shutdown, got %d", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for server graceful shutdown")
	}

	// 6. Inspect database to confirm stale job was recovered to INTERRUPTED
	storeReopened, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed reopening database: %v", err)
	}
	defer storeReopened.Close()

	recoveredJob, err := storeReopened.GetJob(ctx, "job-crash-recovery-test")
	if err != nil {
		t.Fatalf("failed to get recovered job: %v", err)
	}
	if recoveredJob.Status != domain.JobStatusInterrupted {
		t.Errorf("expected job status INTERRUPTED, got %s", recoveredJob.Status)
	}
}

func TestRunMain_DirectInvocation(t *testing.T) {
	var stdout, stderr bytes.Buffer
	// Invalid flag causes runMain to return code 2 immediately without blocking
	code := runMain([]string{"--bad-flag"}, &stdout, &stderr)
	if code != 2 {
		t.Errorf("expected exit code 2, got %d", code)
	}
}

func TestMainFunction(t *testing.T) {
	oldExit := exitFunc
	var exitCode int
	exitFunc = func(code int) {
		exitCode = code
	}
	defer func() { exitFunc = oldExit }()

	oldArgs := os.Args
	os.Args = []string{"server", "--invalid-flag-trigger-exit"}
	defer func() { os.Args = oldArgs }()

	main()
	if exitCode != 2 {
		t.Errorf("expected exit code 2, got %d", exitCode)
	}
}
