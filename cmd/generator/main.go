package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"coderpush/pkg/generator"
)

// Options holds validated CLI options.
type Options struct {
	Seed            int64
	Batches         int
	BatchSize       int
	Out             string
	Format          string
	RatioUpdate     float64
	RatioDuplicate  float64
	RatioOutOfOrder float64
	RatioWithdrawn  float64
	RatioCorrupt    float64
	JobID           string
	Verbose         bool
}

// ParseFlags parses and strictly validates CLI arguments.
func ParseFlags(args []string, stderr io.Writer) (*Options, error) {
	fs := flag.NewFlagSet("generator", flag.ContinueOnError)
	fs.SetOutput(stderr)

	opts := &Options{}
	fs.Int64Var(&opts.Seed, "seed", 42, "Deterministic seed (default 42; 0 triggers random seed)")
	fs.IntVar(&opts.Batches, "batches", 10, "Total batches to generate (default 10; 0 for infinite stream)")
	fs.IntVar(&opts.BatchSize, "batch-size", 100, "Number of records per batch (default 100)")
	fs.StringVar(&opts.Out, "out", "stdout", "Output destination ('stdout', '-', or file path)")
	fs.StringVar(&opts.Format, "format", "json-batches", "Output format ('json-batches', 'ndjson-records', 'json')")
	fs.Float64Var(&opts.RatioUpdate, "ratio-update", 0.20, "Ratio of updates to existing SKUs (default 0.20)")
	fs.Float64Var(&opts.RatioDuplicate, "ratio-duplicate", 0.05, "Ratio of duplicate records (default 0.05)")
	fs.Float64Var(&opts.RatioOutOfOrder, "ratio-out-of-order", 0.10, "Ratio of out-of-order timestamps (default 0.10)")
	fs.Float64Var(&opts.RatioWithdrawn, "ratio-withdrawn", 0.05, "Ratio of withdrawn tombstones (default 0.05)")
	fs.Float64Var(&opts.RatioCorrupt, "ratio-corrupt", 0.05, "Ratio of corrupt records violating Invariant D (default 0.05)")
	fs.StringVar(&opts.JobID, "job-id", "", "Job ID prefix (defaults to auto-generated)")
	fs.BoolVar(&opts.Verbose, "verbose", false, "Print operational statistics to stderr")

	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	// Validate batches
	if opts.Batches < 0 {
		return nil, fmt.Errorf("invalid --batches %d: must be >= 0 (0 for infinite stream)", opts.Batches)
	}

	// Validate batch size
	if opts.BatchSize <= 0 {
		return nil, fmt.Errorf("invalid --batch-size %d: must be > 0", opts.BatchSize)
	}

	// Validate format
	opts.Format = strings.ToLower(strings.TrimSpace(opts.Format))
	switch opts.Format {
	case "json-batches", "ndjson-records", "json":
		// valid
	default:
		return nil, fmt.Errorf("unknown --format %q: allowed formats are 'json-batches', 'ndjson-records', 'json'", opts.Format)
	}

	// Format vs Infinite Stream conflict
	if opts.Batches == 0 && opts.Format == "json" {
		return nil, errors.New("--format 'json' (array) cannot be used with infinite stream (--batches 0); use 'json-batches' or 'ndjson-records'")
	}

	// Validate ratios
	ratios := []struct {
		name string
		val  float64
	}{
		{"ratio-update", opts.RatioUpdate},
		{"ratio-duplicate", opts.RatioDuplicate},
		{"ratio-out-of-order", opts.RatioOutOfOrder},
		{"ratio-withdrawn", opts.RatioWithdrawn},
		{"ratio-corrupt", opts.RatioCorrupt},
	}
	for _, r := range ratios {
		if math.IsNaN(r.val) || math.IsInf(r.val, 0) || r.val < 0.0 || r.val > 1.0 {
			return nil, fmt.Errorf("invalid --%s %f: must be between 0.0 and 1.0", r.name, r.val)
		}
	}

	sumRatios := opts.RatioUpdate + opts.RatioDuplicate + opts.RatioOutOfOrder + opts.RatioWithdrawn + opts.RatioCorrupt
	if sumRatios > 1.0 {
		return nil, fmt.Errorf("sum of ratios cannot exceed 1.0, got %f", sumRatios)
	}

	return opts, nil
}

// Run executes the streaming generator loop.
func Run(ctx context.Context, opts *Options, out io.Writer, stderr io.Writer) error {
	// Resolve seed
	resolvedSeed := uint64(opts.Seed)
	if opts.Seed == 0 {
		resolvedSeed = uint64(time.Now().UnixNano())
		if opts.Verbose {
			fmt.Fprintf(stderr, "[generator] resolved dynamic seed: %d\n", resolvedSeed)
		}
	}

	jobID := opts.JobID
	if strings.TrimSpace(jobID) == "" {
		jobID = fmt.Sprintf("job-gen-%d", time.Now().Unix())
	}

	cfg := generator.Config{
		Seed:              resolvedSeed,
		TotalBatches:      opts.Batches,
		BatchSize:         opts.BatchSize,
		UpdateRatio:       opts.RatioUpdate,
		DuplicateRatio:    opts.RatioDuplicate,
		OutOfOrderRatio:   opts.RatioOutOfOrder,
		WithdrawnRatio:    opts.RatioWithdrawn,
		InvalidRatio:      opts.RatioCorrupt,
		CorruptRatio:      opts.RatioCorrupt,
		ReservoirCapacity: 1000,
	}

	gen, err := generator.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to initialize generator: %w", err)
	}

	bw := bufio.NewWriterSize(out, 64*1024)
	defer bw.Flush()

	batchStream := gen.StreamBatches(ctx, jobID)

	batchCount := 0
	recordCount := 0

	switch opts.Format {
	case "json-batches":
		for {
			select {
			case <-ctx.Done():
				return handleContextDone(ctx, bw, stderr, opts.Verbose)
			case batch, ok := <-batchStream:
				if !ok {
					goto finished
				}
				data, err := generator.MarshalBatchJSON(batch)
				if err != nil {
					return fmt.Errorf("failed to marshal batch %d: %w", batch.BatchIndex, err)
				}
				if _, err := bw.Write(data); err != nil {
					return checkPipeErr(err)
				}
				if err := bw.WriteByte('\n'); err != nil {
					return checkPipeErr(err)
				}
				if err := bw.Flush(); err != nil {
					return checkPipeErr(err)
				}
				batchCount++
				recordCount += len(batch.Records)
			}
		}

	case "ndjson-records":
		for {
			select {
			case <-ctx.Done():
				return handleContextDone(ctx, bw, stderr, opts.Verbose)
			case batch, ok := <-batchStream:
				if !ok {
					goto finished
				}
				for _, rec := range batch.Records {
					data, err := generator.MarshalRecordJSON(rec)
					if err != nil {
						return fmt.Errorf("failed to marshal record: %w", err)
					}
					if _, err := bw.Write(data); err != nil {
						return checkPipeErr(err)
					}
					if err := bw.WriteByte('\n'); err != nil {
						return checkPipeErr(err)
					}
				}
				if err := bw.Flush(); err != nil {
					return checkPipeErr(err)
				}
				batchCount++
				recordCount += len(batch.Records)
			}
		}

	case "json":
		if _, err := bw.WriteString("[\n"); err != nil {
			return checkPipeErr(err)
		}
		first := true
		for {
			select {
			case <-ctx.Done():
				_, _ = bw.WriteString("\n]\n")
				return handleContextDone(ctx, bw, stderr, opts.Verbose)
			case batch, ok := <-batchStream:
				if !ok {
					if _, err := bw.WriteString("\n]\n"); err != nil {
						return checkPipeErr(err)
					}
					goto finished
				}
				if !first {
					if _, err := bw.WriteString(",\n"); err != nil {
						return checkPipeErr(err)
					}
				}
				first = false

				data, err := generator.MarshalBatchJSON(batch)
				if err != nil {
					return fmt.Errorf("failed to marshal batch %d: %w", batch.BatchIndex, err)
				}
				if _, err := bw.Write(data); err != nil {
					return checkPipeErr(err)
				}
				if err := bw.Flush(); err != nil {
					return checkPipeErr(err)
				}
				batchCount++
				recordCount += len(batch.Records)
			}
		}
	}

finished:
	if err := bw.Flush(); err != nil {
		return checkPipeErr(err)
	}
	if opts.Verbose {
		fmt.Fprintf(stderr, "[generator] completed: emitted %d batches (%d records)\n", batchCount, recordCount)
	}
	return nil
}

func checkPipeErr(err error) error {
	if errors.Is(err, syscall.EPIPE) || strings.Contains(err.Error(), "broken pipe") {
		return nil // Graceful exit on broken pipe
	}
	return err
}

func handleContextDone(ctx context.Context, bw *bufio.Writer, stderr io.Writer, verbose bool) error {
	_ = bw.Flush()
	if verbose {
		fmt.Fprintf(stderr, "[generator] stream terminated: %v\n", ctx.Err())
	}
	if errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}
	return ctx.Err()
}

func runMain(args []string, stdout, stderr io.Writer) int {
	opts, err := ParseFlags(args, stderr)
	if err != nil {
		fmt.Fprintf(stderr, "Error: %v\n", err)
		return 2
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var out io.Writer = stdout
	if opts.Out != "stdout" && opts.Out != "-" && opts.Out != "" {
		f, err := os.OpenFile(opts.Out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Fprintf(stderr, "Error opening output file: %v\n", err)
			return 1
		}
		defer f.Close()
		out = f
	}

	if err := Run(ctx, opts, out, stderr); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(stderr, "Runtime error: %v\n", err)
			return 1
		}
	}
	return 0
}

var exitFunc = os.Exit

func main() {
	exitFunc(runMain(os.Args[1:], os.Stdout, os.Stderr))
}
