# Architectural Blueprint: CLI Entry Point (`cmd/generator/main.go`) & Streaming I/O Design

## 1. Observation

Direct observations from the codebase, project specifications, and existing interfaces:

1. **Project Specification (`PROJECT.md:18-21, 188-210`)**:
   - `PROJECT.md:18-21`: "Infinite streaming generator with $O(1)$ memory footprint using `math/rand/v2` PCG PRNG. Seedable deterministic generation with configurable batch volume and sizes. Calibrated anomaly injection: duplicates, out-of-order timestamps ($T_{new} < T_{old}$), withdrawn items, and corrupt records (negative prices, missing SKUs)."
   - `PROJECT.md:197-205`: Defines `generator.Config`:
     ```go
     type Config struct {
         Seed            uint64  `json:"seed"`
         TotalBatches    int     `json:"total_batches"`
         BatchSize       int     `json:"batch_size"`
         DuplicateRatio  float64 `json:"duplicate_ratio"`
         OutOfOrderRatio float64 `json:"out_of_order_ratio"`
         WithdrawnRatio  float64 `json:"withdrawn_ratio"`
         InvalidRatio    float64 `json:"invalid_ratio"`
     }
     ```
   - `PROJECT.md:207-210`: Defines `generator.Generator`:
     ```go
     type Generator interface {
         StreamBatches(ctx context.Context, jobID string) <-chan domain.Batch
         GenerateBatch(jobID string, batchIndex int) domain.Batch
     }
     ```

2. **Domain Models (`pkg/domain/models.go:44-51, 70-75`)**:
   - `SupplierUpdate`:
     ```go
     type SupplierUpdate struct {
         SKU       string        `json:"sku"`
         Name      string        `json:"name"`
         Price     float64       `json:"price"`
         Status    ProductStatus `json:"status"`
         Timestamp time.Time     `json:"timestamp"`
     }
     ```
   - `Batch`:
     ```go
     type Batch struct {
         JobID      string           `json:"job_id"`
         BatchIndex int              `json:"batch_index"`
         Records    []SupplierUpdate `json:"records"`
     }
     ```

3. **Sync Engine Streaming Contract (`pkg/sync/engine.go:50-56, 313-351, 402-464`)**:
   - `SyncEngine.RunJob(ctx context.Context, jobID string, batchStream <-chan domain.Batch) (*domain.SyncJob, error)`
   - Processes batches iteratively from `batchStream`. Commits each batch atomically within SQLite WAL transactions, recording `batch_checkpoints` and `dead_letter_issues`.
   - Sequential batch indexing requirement (`pkg/sync/engine.go:334-340`):
     ```go
     if batch.BatchIndex > job.CommittedBatchIndex+1 {
         err := fmt.Errorf("gap detected in batch sequence: received batch index %d, expected %d", batch.BatchIndex, job.CommittedBatchIndex+1)
         e.markInterrupted(job)
         return job, err
     }
     ```
   - Batches must be strictly sequential starting at index 0 without gaps.

4. **Milestone Dispatch Specifications (`DISPATCH.md:5-20`)**:
   - Flags required:
     - `--seed <int64>`: Deterministic seed (default: random or 42)
     - `--batches <int>`: Total batches to generate (default: 10; 0 for infinite stream)
     - `--batch-size <int>`: Number of records per batch (default: 100)
     - `--out <string>`: Output destination (`stdout`, or file path)
     - `--format <string>`: Output format (`json-batches`, `ndjson-records`, or `json`)
     - `--ratio-update <float>`: Ratio of updates to existing SKUs (default: 0.20)
     - `--ratio-duplicate <float>`: Ratio of duplicate records (default: 0.05)
     - `--ratio-out-of-order <float>`: Ratio of out-of-order timestamps (default: 0.10)
     - `--ratio-withdrawn <float>`: Ratio of withdrawn tombstones (default: 0.05)
     - `--ratio-corrupt <float>`: Ratio of corrupt records violating Invariant D (default: 0.05)

5. **Environment & Runtime (`go.mod:3`)**:
   - Go `1.25.0` standard library includes `math/rand/v2` and `os/signal.NotifyContext`.
   - `CGO_ENABLED=0` static binary compatibility. No external CLI dependencies (like cobra/viper) are needed or desired; Go's standard library `flag` package satisfies all requirements with 0 dependencies.

---

## 2. Logic Chain

From these observations, we derive the design decisions step by step:

1. **CLI Flag Architecture & Determinism (Observations 1 & 4)**:
   - Setting `--seed 42` as default ensures that any test or manual invocation without arguments is 100% deterministic and reproducible across machines.
   - When a user explicitly passes `--seed 0`, the generator interprets this as "random seed requested" and samples an entropy source (`time.Now().UnixNano()`), printing the chosen seed to `os.Stderr` so the user can reproduce the run if needed.
   - Ratios: Five distinct ratios are supported: `--ratio-update`, `--ratio-duplicate`, `--ratio-out-of-order`, `--ratio-withdrawn`, `--ratio-corrupt`. Each ratio must be strictly validated in the range $[0.0, 1.0]$.
   - Negative batch sizes or negative batch counts must be rejected immediately during flag validation before any I/O or generator initialization occurs.

2. **Memory Footprint & Latency Architecture (Observations 1 & 3)**:
   - If generating 1,000,000 batches or running an infinite stream (`--batches 0`), accumulating batches in an in-memory slice would cause an $O(N)$ memory leak and eventual OOM crash.
   - Therefore, `cmd/generator/main.go` must stream batches one-by-one: generate batch $i \rightarrow$ serialize $\rightarrow$ write to `bufio.Writer` $\rightarrow$ flush $\rightarrow$ discard batch for garbage collection. Heap memory remains strictly $O(1)$.
   - Wrapping output in a 64 KB `bufio.Writer` avoids high system call overhead per line while calling `Flush()` after each batch guarantees low-latency delivery when piped to downstream consumers (`curl`, `cmd/server`, or `SyncEngine`).

3. **Output Format Semantics (Observation 4)**:
   - **`json-batches`** (Default): Emits one complete `domain.Batch` JSON object per line terminated by `\n`. Highly streaming-friendly, directly maps to `SyncEngine` batch boundaries, preserves `batch_index` and `job_id`.
   - **`ndjson-records`**: Emits one `domain.SupplierUpdate` JSON object per line terminated by `\n`. Standard flat NDJSON format compatible with general data ingestion tools (`jq`, line readers, Kafka).
   - **`json`**: Formats output as a standard JSON array `[ <batch>, <batch>, ... ]`.
     - *Crucial constraint*: If `--batches 0` (infinite mode), a standard closed JSON array is impossible. The CLI must reject `--format json` when `--batches 0` with a clean error message directing the user to `json-batches` or `ndjson-records`. For finite runs (`--batches > 0`), the generator streams array elements incrementally (`[\n`, items separated by `,\n`, `\n]\n`) without buffering all batches in RAM.

4. **Signal Handling & Unix Pipeline Resiliency**:
   - Long-running or infinite streams (`--batches 0`) must terminate cleanly when the operator presses `Ctrl+C` (`SIGINT`) or when a process manager sends `SIGTERM`.
   - Using `signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)` allows propagation of cancellation to generator routines.
   - When piped into utilities like `head` (e.g. `cmd/generator | head -n 10`), the consumer closes the pipe early. Go returns `syscall.EPIPE`. The CLI must intercept broken pipe errors and exit cleanly with status code `0`, suppressing stack traces or error dumps.
   - All diagnostics, progress information, and warnings are routed strictly to `os.Stderr`, guaranteeing that `os.Stdout` remains 100% clean JSON/NDJSON data.

5. **SyncEngine Integration Architecture (Observations 2 & 3)**:
   - Two seamless integration pathways:
     1. **In-Memory Channel Integration**: Passing `gen.StreamBatches(ctx, jobID)` directly into `SyncEngine.RunJob(ctx, jobID, stream)`. Channel backpressure naturally synchronizes generator pace with SQLite disk commit speed.
     2. **Piped / Stream Reader Integration**: A dedicated streaming decoder `ReadJSONBatches(ctx, io.Reader)` reads `json-batches` from `os.Stdin` (or an HTTP request body) and converts it into `<-chan domain.Batch`, allowing zero-code piping between the CLI generator and server/sync runners.

---

## 3. Detailed Design Specification

### 3.1. CLI Flags & Configuration Struct

```go
type GeneratorCLIOptions struct {
	Seed            int64   // Deterministic seed (default 42; 0 triggers time-based random)
	Batches         int     // Batches to generate (default 10; 0 for infinite stream)
	BatchSize       int     // Records per batch (default 100; must be > 0)
	Out             string  // Output destination: "stdout", "-", or file path
	Format          string  // Format: "json-batches", "ndjson-records", "json"
	RatioUpdate     float64 // Ratio of updates to existing SKUs (default 0.20)
	RatioDuplicate  float64 // Ratio of duplicate records (default 0.05)
	RatioOutOfOrder float64 // Ratio of out-of-order timestamps (default 0.10)
	RatioWithdrawn  float64 // Ratio of withdrawn tombstones (default 0.05)
	RatioCorrupt    float64 // Ratio of corrupt records violating Invariant D (default 0.05)
	JobID           string  // Optional job ID prefix (default "job-gen-<timestamp>")
	Verbose         bool    // Print operational stats to stderr
}
```

#### Validation Rules & Error Codes
| Flag | Validation Rule | Error Message / Action |
|------|-----------------|------------------------|
| `--seed` | Any `int64`. If `0`, resolve to `time.Now().UnixNano()` | If resolved from 0, log `Resolved dynamic seed: <seed>` to stderr |
| `--batches` | Must be $\ge 0$ | Error: `invalid --batches %d: must be >= 0 (0 for infinite)` (Exit code 2) |
| `--batch-size` | Must be $> 0$ | Error: `invalid --batch-size %d: must be > 0` (Exit code 2) |
| `--format` | Must be one of `json-batches`, `ndjson-records`, `json` | Error: `unknown --format %q: allowed formats are json-batches, ndjson-records, json` (Exit code 2) |
| Conflict | `--batches 0` AND `--format json` | Error: `--format json (array) cannot be used with infinite stream (--batches 0); use json-batches or ndjson-records` (Exit code 2) |
| `--ratio-*` | Each ratio must satisfy $0.0 \le r \le 1.0$ and not NaN/Inf | Error: `invalid %s %f: ratio must be between 0.0 and 1.0` (Exit code 2) |
| `--out` | If not `stdout`/`-`, directory must be writable | Error: `failed to open output file %q: <err>` (Exit code 1) |

---

### 3.2. Streaming I/O & Output Formats

#### Format 1: `json-batches` (Default)
Each line is a complete, single-line JSON serialized `domain.Batch`:
```json
{"job_id":"job-gen-1729512000","batch_index":0,"records":[{"sku":"SKU-10001","name":"Ergonomic Steel Table","price":84.50,"status":"ACTIVE","timestamp":"2026-10-21T10:00:00Z"}]}
{"job_id":"job-gen-1729512000","batch_index":1,"records":[{"sku":"SKU-10002","name":"Rustic Bronze Chair","price":42.00,"status":"ACTIVE","timestamp":"2026-10-21T10:01:00Z"}]}
```
- Serialization: `json.Marshal(batch)` followed by `\n`.
- Flush interval: Immediately after every batch write (`bufWriter.Flush()`).

#### Format 2: `ndjson-records`
Each line is a single-line JSON serialized `domain.SupplierUpdate`:
```json
{"sku":"SKU-10001","name":"Ergonomic Steel Table","price":84.50,"status":"ACTIVE","timestamp":"2026-10-21T10:00:00Z"}
{"sku":"SKU-10002","name":"Rustic Bronze Chair","price":42.00,"status":"ACTIVE","timestamp":"2026-10-21T10:01:00Z"}
```
- Serialization: `json.Marshal(record)` followed by `\n`.
- Flush interval: After every batch of records is processed (`bufWriter.Flush()`).

#### Format 3: `json` (Canonical Array)
Outputs a valid JSON array of batches:
```json
[
{"job_id":"job-gen-1729512000","batch_index":0,"records":[{"sku":"SKU-10001","name":"...","price":84.5,"status":"ACTIVE","timestamp":"..."}]},
{"job_id":"job-gen-1729512000","batch_index":1,"records":[{"sku":"SKU-10002","name":"...","price":42.0,"status":"ACTIVE","timestamp":"..."}]}
]
```
- Implementation:
  1. Write `[\n`
  2. For batch $0 \dots N-1$:
     - If $i > 0$, write `,\n`
     - Write compact or formatted batch JSON
  3. Write `\n]\n`
  4. Flush once complete.

---

### 3.3. Signal Handling, Broken Pipe, and Execution Flow

```
                      +-----------------------------+
                      |       cmd/generator         |
                      |  Parse Flags & Validate     |
                      +--------------+--------------+
                                     |
                      +--------------v--------------+
                      |   signal.NotifyContext      |
                      |   (SIGINT, SIGTERM)         |
                      +--------------+--------------+
                                     |
                      +--------------v--------------+
                      |   Initialize Output Sink    |
                      |   Stdout vs File Handle     |
                      |   bufio.NewWriter(64KB)     |
                      +--------------+--------------+
                                     |
                      +--------------v--------------+
                      |     Initialize Generator    |
                      |     pkg/generator.New()     |
                      +--------------+--------------+
                                     |
         +---------------------------v---------------------------+
         | StreamBatches(ctx, jobID) -> <-chan domain.Batch       |
         +---------------------------+---------------------------+
                                     |
                           For each batch received:
                                     |
             +-----------------------+-----------------------+
             |                       |                       |
     [json-batches]          [ndjson-records]             [json]
     json.Marshal(batch)     for record in records:      if i > 0: write ",\n"
     write "\n"              json.Marshal(record)        write batch JSON
     bufWriter.Flush()       write "\n"                  bufWriter.Flush()
                             bufWriter.Flush()
             |                       |                       |
             +-----------------------+-----------------------+
                                     |
               Check for ctx.Done() / broken pipe (EPIPE)
                                     |
                      +--------------v--------------+
                      |      Clean Shutdown         |
                      |    - bufWriter.Flush()      |
                      |    - Close File Handle      |
                      |    - Log Summary to Stderr  |
                      |    - os.Exit(0)             |
                      +-----------------------------+
```

#### Broken Pipe (`EPIPE`) Handling
When piped to a downstream command that terminates early (e.g. `cmd/generator | head -n 5`):
```go
if errors.Is(err, syscall.EPIPE) || isBrokenPipe(err) {
    // Normal pipeline termination; flush may fail, exit cleanly with 0
    return nil
}
```

---

### 3.4. Sync Engine Integration Patterns

#### Pattern A: Direct Go API Integration (In-Memory Channel)
`SyncEngine.RunJob` accepts `<-chan domain.Batch`.
```go
// Direct streaming from generator to SyncEngine
cfg := generator.Config{
    Seed:            42,
    TotalBatches:    100,
    BatchSize:       250,
    UpdateRatio:     0.20,
    DuplicateRatio:  0.05,
    OutOfOrderRatio: 0.10,
    WithdrawnRatio:  0.05,
    CorruptRatio:    0.05,
}

gen, err := generator.New(cfg)
if err != nil {
    return err
}

ctx, cancel := context.WithCancel(context.Background())
defer cancel()

jobID := "sync-run-" + uuid.NewString()
batchStream := gen.StreamBatches(ctx, jobID)

// Engine runs with backpressure: generator pauses when engine commits transaction
job, err := engine.RunJob(ctx, jobID, batchStream)
```

#### Pattern B: Unix CLI Pipe Integration (`io.Reader` Ingestion Adapter)
To enable command-line piping:
```bash
./bin/generator --batches 50 --batch-size 100 --format json-batches | ./bin/sync-ingest --job-id "cli-pipe-1"
```

Ingestion Reader helper function in `pkg/sync` or `pkg/generator`:
```go
// ReadJSONBatches reads newline-delimited batches from an io.Reader and delivers them on a channel.
func ReadJSONBatches(ctx context.Context, r io.Reader) (<-chan domain.Batch, <-chan error) {
    ch := make(chan domain.Batch, 4)
    errCh := make(chan error, 1)

    go func() {
        defer close(ch)
        defer close(errCh)

        scanner := bufio.NewScanner(r)
        // Allow up to 10MB per batch line for large batch payloads
        buf := make([]byte, 64*1024)
        scanner.Buffer(buf, 10*1024*1024)

        for scanner.Scan() {
            line := bytes.TrimSpace(scanner.Bytes())
            if len(line) == 0 {
                continue
            }

            var batch domain.Batch
            if err := json.Unmarshal(line, &batch); err != nil {
                errCh <- fmt.Errorf("failed to decode batch JSON: %w", err)
                return
            }

            select {
            case <-ctx.Done():
                errCh <- ctx.Err()
                return
            case ch <- batch:
            }
        }

        if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
            errCh <- fmt.Errorf("scanner error reading batch stream: %w", err)
        }
    }()

    return ch, errCh
}
```

---

## 4. Proposed Code Blueprint for `cmd/generator/main.go`

```go
package main

import (
	"bufio"
	"context"
	"encoding/json"
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

	"coderpush/pkg/domain"
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
		Seed:            resolvedSeed,
		TotalBatches:    opts.Batches,
		BatchSize:       opts.BatchSize,
		UpdateRatio:     opts.RatioUpdate,
		DuplicateRatio:  opts.RatioDuplicate,
		OutOfOrderRatio: opts.RatioOutOfOrder,
		WithdrawnRatio:  opts.RatioWithdrawn,
		CorruptRatio:    opts.RatioCorrupt,
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
				data, err := json.Marshal(batch)
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
					data, err := json.Marshal(rec)
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
				_ = bw.WriteString("\n]\n")
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

				data, err := json.Marshal(batch)
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

func main() {
	opts, err := ParseFlags(os.Args[1:], os.Stderr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var out io.Writer = os.Stdout
	if opts.Out != "stdout" && opts.Out != "-" && opts.Out != "" {
		f, err := os.OpenFile(opts.Out, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening output file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	if err := Run(ctx, opts, out, os.Stderr); err != nil {
		if !errors.Is(err, context.Canceled) {
			fmt.Fprintf(os.Stderr, "Runtime error: %v\n", err)
			os.Exit(1)
		}
	}
}
```

---

## 5. Caveats

1. **Decoupling with `pkg/generator`**:
   The CLI `cmd/generator` directly imports `pkg/generator`. Milestone 3 implementation worker will implement `pkg/generator` in tandem with `cmd/generator`. The field naming in `generator.Config` (`UpdateRatio` vs `RatioUpdate`, `CorruptRatio` vs `InvalidRatio`) should use canonical aliases so both are accepted without breakage.
2. **Infinite Stream Memory on Long Runs**:
   The constant memory guarantee depends entirely on `pkg/generator` using a fixed-size ring buffer for historical SKUs (e.g. 500-1000 items) rather than an unbounded map. As confirmed in `explorer_m3_1_gen2`, the ring buffer ensures strictly $O(1)$ memory.
3. **JSON Array Mode Limitation**:
   The canonical `json` array format (`[ batch1, batch2, ... ]`) is deliberately restricted to finite runs (`--batches > 0`) because streaming infinite array JSON violates standard JSON parsers that require an EOF closing bracket `]`.

---

## 6. Conclusion

The CLI entry point `cmd/generator/main.go` and streaming I/O design provides:
1. **Deterministic by Default**: A default seed of 42 ensures reproducible testing and CI execution out of the box; dynamic random generation is enabled via `--seed 0`.
2. **Flexible High-Performance Formats**: Full support for `json-batches` (default, newline-delimited `domain.Batch`), `ndjson-records` (individual `domain.SupplierUpdate`), and `json` (finite canonical array).
3. **Production-Grade Streaming Resiliency**: 64 KB buffered I/O with per-batch flushes for near-zero latency, constant $O(1)$ memory consumption, graceful signal cancellation (`SIGINT`/`SIGTERM`), and silent, clean termination on broken Unix pipes (`EPIPE`).
4. **Seamless Sync Engine Interoperability**: Direct channel ingestion (`gen.StreamBatches` into `SyncEngine.RunJob`) and streaming reader adapters (`ReadJSONBatches`) for piping Unix streams directly into batch transactions.

---

## 7. Verification Method

To independently verify this design during implementation:

1. **Flag Parsing & Validation Unit Tests**:
   Create `cmd/generator/main_test.go` and test table-driven scenarios:
   - Default flag values
   - Negative batches rejected with error
   - Zero/negative batch-size rejected with error
   - Invalid formats rejected
   - Infinite `--batches 0` + `--format json` rejected
   - Ratios $< 0$ or $> 1$ or NaN rejected

2. **Deterministic Output Verification**:
   Execute generator twice with identical seed:
   ```bash
   go run cmd/generator/main.go --seed 42 --batches 5 --batch-size 10 --out /tmp/run1.ndjson
   go run cmd/generator/main.go --seed 42 --batches 5 --batch-size 10 --out /tmp/run2.ndjson
   diff -u /tmp/run1.ndjson /tmp/run2.ndjson
   ```
   *Expected result*: Exit status 0, zero difference.

3. **Broken Pipe Resiliency Test**:
   Pipe infinite generator into `head -n 5`:
   ```bash
   go run cmd/generator/main.go --batches 0 --format ndjson-records | head -n 5
   ```
   *Expected result*: Exactly 5 lines output, exit code 0, no broken pipe panics on stderr.

4. **Integration Test with Sync Engine**:
   Test piping `cmd/generator` output into `SyncEngine.RunJob` using `ReadJSONBatches`:
   ```go
   func TestCLI_PipedSyncEngineIntegration(t *testing.T) {
       store := setupTestStorage(t)
       engine := sync.NewEngine(store)
       
       var buf bytes.Buffer
       opts := &Options{Seed: 42, Batches: 10, BatchSize: 50, Format: "json-batches"}
       err := Run(context.Background(), opts, &buf, io.Discard)
       require.NoError(t, err)
       
       stream, errCh := sync.ReadJSONBatches(context.Background(), &buf)
       job, err := engine.RunJob(context.Background(), "test-piped-job", stream)
       require.NoError(t, err)
       require.Equal(t, domain.JobStatusCompleted, job.Status)
       require.Equal(t, 10, job.CommittedBatchIndex + 1)
   }
   ```

5. **Project Test Suite Regression**:
   Verify no regressions on existing packages:
   ```bash
   go test -race -v -count=1 ./...
   ```
