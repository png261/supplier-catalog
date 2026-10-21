package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"coderpush/pkg/domain"
	"coderpush/pkg/generator"
	"coderpush/pkg/storage"
	catalogSync "coderpush/pkg/sync"
)

func TestCLI_ParseFlags_DefaultsAndCustom(t *testing.T) {
	// Defaults
	var stderr bytes.Buffer
	opts, err := ParseFlags([]string{}, &stderr)
	if err != nil {
		t.Fatalf("unexpected error on default flags: %v", err)
	}
	if opts.Seed != 42 {
		t.Errorf("expected default seed 42, got %d", opts.Seed)
	}
	if opts.Batches != 10 {
		t.Errorf("expected default batches 10, got %d", opts.Batches)
	}
	if opts.BatchSize != 100 {
		t.Errorf("expected default batch-size 100, got %d", opts.BatchSize)
	}
	if opts.Format != "json-batches" {
		t.Errorf("expected default format 'json-batches', got %s", opts.Format)
	}
	if opts.Out != "stdout" {
		t.Errorf("expected default out 'stdout', got %s", opts.Out)
	}

	// Custom
	customArgs := []string{
		"--seed", "12345",
		"--batches", "5",
		"--batch-size", "25",
		"--out", "-",
		"--format", "ndjson-records",
		"--ratio-update", "0.15",
		"--ratio-duplicate", "0.08",
		"--ratio-out-of-order", "0.07",
		"--ratio-withdrawn", "0.06",
		"--ratio-corrupt", "0.04",
		"--job-id", "custom-job-1",
		"--verbose",
	}
	optsCustom, err := ParseFlags(customArgs, &stderr)
	if err != nil {
		t.Fatalf("unexpected error on custom flags: %v", err)
	}
	if optsCustom.Seed != 12345 || optsCustom.Batches != 5 || optsCustom.BatchSize != 25 {
		t.Errorf("custom options mismatch: %+v", optsCustom)
	}
	if optsCustom.Format != "ndjson-records" || optsCustom.JobID != "custom-job-1" || !optsCustom.Verbose {
		t.Errorf("custom options mismatch: %+v", optsCustom)
	}
}

func TestCLI_ParseFlags_ValidationErrors(t *testing.T) {
	tests := []struct {
		name   string
		args   []string
		errMsg string
	}{
		{
			name:   "negative batches",
			args:   []string{"--batches", "-1"},
			errMsg: "must be >= 0",
		},
		{
			name:   "zero batch size",
			args:   []string{"--batch-size", "0"},
			errMsg: "must be > 0",
		},
		{
			name:   "negative batch size",
			args:   []string{"--batch-size", "-10"},
			errMsg: "must be > 0",
		},
		{
			name:   "unknown format",
			args:   []string{"--format", "xml"},
			errMsg: "unknown --format",
		},
		{
			name:   "infinite batches with json format",
			args:   []string{"--batches", "0", "--format", "json"},
			errMsg: "cannot be used with infinite stream",
		},
		{
			name:   "negative ratio",
			args:   []string{"--ratio-update", "-0.1"},
			errMsg: "must be between 0.0 and 1.0",
		},
		{
			name:   "ratio greater than 1",
			args:   []string{"--ratio-corrupt", "1.5"},
			errMsg: "must be between 0.0 and 1.0",
		},
		{
			name:   "sum of ratios greater than 1",
			args:   []string{"--ratio-update", "0.5", "--ratio-duplicate", "0.4", "--ratio-out-of-order", "0.3"},
			errMsg: "sum of ratios cannot exceed 1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			_, err := ParseFlags(tt.args, &stderr)
			if err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errMsg) {
				t.Fatalf("expected error containing %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}

func TestCLI_Run_Formats(t *testing.T) {
	// 1. json-batches
	t.Run("json-batches", func(t *testing.T) {
		var out bytes.Buffer
		opts := &Options{
			Seed:      42,
			Batches:   3,
			BatchSize: 5,
			Format:    "json-batches",
		}
		if err := Run(context.Background(), opts, &out, io.Discard); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 3 {
			t.Fatalf("expected 3 lines, got %d", len(lines))
		}
		for i, line := range lines {
			batch, err := generator.UnmarshalBatchJSON([]byte(line))
			if err != nil {
				t.Fatalf("failed to unmarshal batch %d: %v", i, err)
			}
			if batch.BatchIndex != i {
				t.Errorf("expected batch index %d, got %d", i, batch.BatchIndex)
			}
			if len(batch.Records) != 5 {
				t.Errorf("expected 5 records, got %d", len(batch.Records))
			}
		}
	})

	// 2. ndjson-records
	t.Run("ndjson-records", func(t *testing.T) {
		var out bytes.Buffer
		opts := &Options{
			Seed:      42,
			Batches:   2,
			BatchSize: 4,
			Format:    "ndjson-records",
		}
		if err := Run(context.Background(), opts, &out, io.Discard); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 8 {
			t.Fatalf("expected 8 record lines, got %d", len(lines))
		}
		for i, line := range lines {
			var m map[string]any
			if err := json.Unmarshal([]byte(line), &m); err != nil {
				t.Fatalf("failed to unmarshal line %d: %v", i, err)
			}
			if _, ok := m["sku"]; !ok {
				t.Errorf("line %d missing sku: %s", i, line)
			}
		}
	})

	// 3. json array
	t.Run("json", func(t *testing.T) {
		var out bytes.Buffer
		opts := &Options{
			Seed:      42,
			Batches:   3,
			BatchSize: 4,
			Format:    "json",
		}
		if err := Run(context.Background(), opts, &out, io.Discard); err != nil {
			t.Fatalf("Run failed: %v", err)
		}

		raw := out.Bytes()
		if !strings.HasPrefix(string(raw), "[\n") || !strings.HasSuffix(string(raw), "\n]\n") {
			t.Fatalf("expected valid JSON array format, got:\n%s", string(raw))
		}

		var rawBatches []json.RawMessage
		if err := json.Unmarshal(raw, &rawBatches); err != nil {
			t.Fatalf("failed to unmarshal json array: %v", err)
		}
		if len(rawBatches) != 3 {
			t.Fatalf("expected 3 batches in array, got %d", len(rawBatches))
		}
	})
}

func TestCLI_Run_DynamicSeed(t *testing.T) {
	var out bytes.Buffer
	var stderr bytes.Buffer
	opts := &Options{
		Seed:      0, // Dynamic seed requested
		Batches:   1,
		BatchSize: 2,
		Format:    "json-batches",
		Verbose:   true,
	}
	if err := Run(context.Background(), opts, &out, &stderr); err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !strings.Contains(stderr.String(), "resolved dynamic seed") {
		t.Fatalf("expected stderr to contain dynamic seed notice, got: %s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "completed: emitted 1 batches (2 records)") {
		t.Fatalf("expected stderr completion message, got: %s", stderr.String())
	}
}

func TestCLI_Run_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var out bytes.Buffer
	var stderr bytes.Buffer
	opts := &Options{
		Seed:      42,
		Batches:   1000,
		BatchSize: 10,
		Format:    "json-batches",
		Verbose:   true,
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err := Run(ctx, opts, &out, &stderr)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("unexpected error on context cancellation: %v", err)
	}
}

type brokenPipeWriter struct{}

func (w *brokenPipeWriter) Write(p []byte) (n int, err error) {
	return 0, syscall.EPIPE
}

func TestCLI_Run_BrokenPipe(t *testing.T) {
	opts := &Options{
		Seed:      42,
		Batches:   10,
		BatchSize: 10,
		Format:    "json-batches",
	}

	err := Run(context.Background(), opts, &brokenPipeWriter{}, io.Discard)
	if err != nil {
		t.Fatalf("expected nil error on broken pipe, got: %v", err)
	}
}

func TestCLI_Run_FileOutput(t *testing.T) {
	outFile := filepath.Join(t.TempDir(), "output.ndjson")
	opts := &Options{
		Seed:      42,
		Batches:   2,
		BatchSize: 5,
		Out:       outFile,
		Format:    "ndjson-records",
	}

	f, err := os.OpenFile(opts.Out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatalf("failed to open file: %v", err)
	}
	defer f.Close()

	if err := Run(context.Background(), opts, f, io.Discard); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	data, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("failed to read written file: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 10 {
		t.Fatalf("expected 10 records written, got %d", len(lines))
	}
}

func TestCLI_PipedSyncEngineIntegration(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "piped_engine.db")
	store, err := storage.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open storage: %v", err)
	}
	defer store.Close()

	engine := catalogSync.NewEngine(store)

	var buf bytes.Buffer
	opts := &Options{
		Seed:            42,
		Batches:         10,
		BatchSize:       50,
		Format:          "json-batches",
		RatioUpdate:     0.20,
		RatioDuplicate:  0.05,
		RatioOutOfOrder: 0.10,
		RatioWithdrawn:  0.05,
		RatioCorrupt:    0.05,
	}

	if err := Run(context.Background(), opts, &buf, io.Discard); err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	stream, errCh := generator.ReadJSONBatches(context.Background(), &buf)
	job, err := engine.RunJob(context.Background(), "test-piped-job", stream)
	if err != nil {
		t.Fatalf("engine.RunJob failed: %v", err)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("read stream error: %v", err)
	}

	if job.Status != domain.JobStatusCompleted {
		t.Fatalf("expected COMPLETED job status, got %s", job.Status)
	}
	if job.CommittedBatchIndex != 9 {
		t.Fatalf("expected committed batch index 9, got %d", job.CommittedBatchIndex)
	}
	if job.Summary.TotalProcessed() != 500 {
		t.Fatalf("expected 500 total processed items, got %d", job.Summary.TotalProcessed())
	}
}

func TestCLI_RunMain(t *testing.T) {
	var stdout, stderr bytes.Buffer

	// Success exit code 0
	code := runMain([]string{"--seed", "42", "--batches", "2", "--batch-size", "3"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}

	// Flag error exit code 2
	stderr.Reset()
	code = runMain([]string{"--batches", "-5"}, &stdout, &stderr)
	if code != 2 {
		t.Fatalf("expected exit code 2, got %d", code)
	}

	// File error exit code 1
	stderr.Reset()
	code = runMain([]string{"--out", "/non_existent_dir_xyz_123/out.txt"}, &stdout, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}

	// Runtime error exit code 1
	stderr.Reset()
	code = runMain([]string{"--batches", "2", "--batch-size", "2"}, &errWriter{}, &stderr)
	if code != 1 {
		t.Fatalf("expected exit code 1 on write error, got %d", code)
	}
}

func TestCLI_CheckPipeErr(t *testing.T) {
	if err := checkPipeErr(syscall.EPIPE); err != nil {
		t.Fatalf("expected nil for EPIPE, got %v", err)
	}
	if err := checkPipeErr(errors.New("write: broken pipe")); err != nil {
		t.Fatalf("expected nil for broken pipe string, got %v", err)
	}
	other := errors.New("disk full")
	if err := checkPipeErr(other); err != other {
		t.Fatalf("expected other error unchanged, got %v", err)
	}
}

func TestCLI_HandleContextDone(t *testing.T) {
	var buf bytes.Buffer
	var stderr bytes.Buffer
	bw := bufio.NewWriter(&buf)

	// Canceled context returns nil
	ctxCancel, cancel := context.WithCancel(context.Background())
	cancel()
	if err := handleContextDone(ctxCancel, bw, &stderr, true); err != nil {
		t.Fatalf("expected nil on context.Canceled, got %v", err)
	}

	// Deadline exceeded returns error
	ctxDeadline, dCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer dCancel()
	if err := handleContextDone(ctxDeadline, bw, &stderr, false); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
}

func TestCLI_Run_FormatContextCancellations(t *testing.T) {
	// 1. ndjson-records cancellation
	ctx1, cancel1 := context.WithCancel(context.Background())
	cancel1()
	var out1 bytes.Buffer
	opts1 := &Options{Seed: 42, Batches: 100, BatchSize: 10, Format: "ndjson-records"}
	if err := Run(ctx1, opts1, &out1, io.Discard); err != nil {
		t.Fatalf("expected nil error on canceled ndjson, got %v", err)
	}

	// 2. json array cancellation
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	var out2 bytes.Buffer
	opts2 := &Options{Seed: 42, Batches: 100, BatchSize: 10, Format: "json"}
	if err := Run(ctx2, opts2, &out2, io.Discard); err != nil {
		t.Fatalf("expected nil error on canceled json, got %v", err)
	}

	// 3. invalid config error
	opts3 := &Options{Seed: 42, Batches: 10, BatchSize: -1, Format: "json-batches"}
	if err := Run(context.Background(), opts3, &out1, io.Discard); err == nil {
		t.Fatalf("expected error on invalid config in Run, got nil")
	}
}

type errWriter struct{}

func (e *errWriter) Write(p []byte) (n int, err error) {
	return 0, errors.New("simulated disk error")
}

func TestCLI_Run_WriteErrors(t *testing.T) {
	opts := &Options{Seed: 42, Batches: 5, BatchSize: 2, Format: "json-batches"}
	if err := Run(context.Background(), opts, &errWriter{}, io.Discard); err == nil {
		t.Fatalf("expected error on failing writer, got nil")
	}

	optsNDJSON := &Options{Seed: 42, Batches: 5, BatchSize: 2, Format: "ndjson-records"}
	if err := Run(context.Background(), optsNDJSON, &errWriter{}, io.Discard); err == nil {
		t.Fatalf("expected error on failing writer for ndjson, got nil")
	}

	optsJSON := &Options{Seed: 42, Batches: 5, BatchSize: 2, Format: "json"}
	if err := Run(context.Background(), optsJSON, &errWriter{}, io.Discard); err == nil {
		t.Fatalf("expected error on failing writer for json, got nil")
	}
}

func TestCLI_RunMain_FileOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	outFile := filepath.Join(t.TempDir(), "run_main_out.ndjson")
	code := runMain([]string{"--out", outFile, "--batches", "2", "--batch-size", "2", "--format", "ndjson-records", "--verbose"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d. stderr: %s", code, stderr.String())
	}
	data, err := os.ReadFile(outFile)
	if err != nil || len(data) == 0 {
		t.Fatalf("expected file to be written and non-empty")
	}
}

func TestCLI_RunMain_StdoutDash(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runMain([]string{"--out", "-", "--batches", "1", "--batch-size", "1"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if stdout.Len() == 0 {
		t.Fatalf("expected stdout output for --out -")
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
	os.Args = []string{"generator", "--batches", "1", "--batch-size", "2"}
	defer func() { os.Args = oldArgs }()

	main()
	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
}
