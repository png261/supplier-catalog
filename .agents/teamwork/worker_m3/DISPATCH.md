# Worker Dispatch: Milestone 3 Implementation (Programmatic Mock Data Generator)

## Mission
Implement the complete programmatic mock data generator library in `pkg/generator/` and the CLI binary in `cmd/generator/main.go`, along with comprehensive unit, determinism, memory benchmark, and statistical ratio calibration test suites.

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Context Files (Read First)
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md` (authoritative requirements R3)
- `/Users/png/01. Project/coderpush/PROJECT.md` (project architecture & interface contracts)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2/handoff.md` (core generator engine architecture & code blueprint)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_2_gen2/handoff.md` (CLI entry point & streaming I/O blueprint)
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m3_1_gen2/handoff.md` (test specifications, binomial 3-sigma calibration, memory benchmarks)
- `/Users/png/01. Project/coderpush/pkg/domain/models.go` (domain models)
- `/Users/png/01. Project/coderpush/pkg/sync/engine.go` (sync engine integration)

## Assigned Files (Exclusive Write Ownership)
- `pkg/generator/config.go`
- `pkg/generator/ring_buffer.go`
- `pkg/generator/generator.go`
- `pkg/generator/reader.go` (or `ReadJSONBatches` streaming helper)
- `pkg/generator/generator_test.go`
- `cmd/generator/main.go`
- `cmd/generator/main_test.go`

## Specific Requirements
1. **Core Generator (`pkg/generator/`)**:
   - `math/rand/v2` PCG PRNG with SplitMix64 128-bit seed derivation (`deriveSeeds(seed, seed2)`).
   - Fixed-capacity circular ring buffer (`RingBuffer`) for $O(1)$ memory consumption across infinite streams.
   - Categorical distribution for calibrated anomaly injection:
     - Updates to existing SKUs (`UpdateRatio`, default 0.20)
     - Duplicate records (`DuplicateRatio`, default 0.05)
     - Out-of-order timestamps (`OutOfOrderRatio`, default 0.10)
     - Withdrawn tombstones (`WithdrawnRatio`, default 0.05)
     - Invariant D corrupt records (`InvalidRatio`, default 0.05) rotating across all 6 defect types (empty/whitespace SKU, negative price, NaN price, Inf price, invalid status, zero timestamp)
     - Brand new products (remainder probability)
   - Prices rounded to 2 decimal places (`math.Round(val*100)/100`).
   - Thread-safe `GenerateBatch` via `sync.Mutex`.
   - Streaming channel `StreamBatches(ctx, jobID)` with clean context cancellation.
   - Streaming helper `ReadJSONBatches(ctx, r io.Reader) (<-chan domain.Batch, <-chan error)` for piping streams to `SyncEngine`.
2. **CLI Entry Point (`cmd/generator/main.go`)**:
   - Standard library `flag` CLI parsing: `--seed`, `--batches`, `--batch-size`, `--out`, `--format`, `--ratio-update`, `--ratio-duplicate`, `--ratio-out-of-order`, `--ratio-withdrawn`, `--ratio-corrupt`, `--job-id`, `--verbose`.
   - Formats: `json-batches` (default), `ndjson-records`, and `json` (finite array only; reject on `--batches 0`).
   - 64KB `bufio.Writer` with per-batch flushing.
   - `signal.NotifyContext` handling `SIGINT`/`SIGTERM`.
   - Clean broken pipe (`syscall.EPIPE`) termination with exit code 0.
   - Structured `Run(ctx, opts, out, stderr)` for clean unit testing in `main_test.go`.
3. **Comprehensive Test Suites**:
   - **Determinism**: Assert same seed produces byte-for-byte identical batch sequences across 100 batches (10,000 items). Assert seeds 42 vs 43 diverge immediately at batch 0, record 0.
   - **Bounded Memory ($O(1)$ Heap)**: Assert heap delta $\le 2\text{MB}$ when generating 100,000 items through `StreamBatches`.
   - **Statistical Ratio Calibration**: Assert anomaly counts over $N = 20,000$ items fall within binomial $3\sigma$ bounds ($[\mu - 3\sigma, \mu + 3\sigma]$).
   - **Corruption Coverage**: Verify all 6 Invariant D defect types are produced and validated.
   - **E2E Pipeline Feeding**: Feed `StreamBatches` into `SyncEngine.RunJob` and verify atomic commits, checkpoints, and accounting conservation ($\text{Added} + \text{Updated} + \text{Withdrawn} + \text{Skipped} + \text{DeadLetter} \equiv N_{total}$).
   - **CLI Tests**: Table-driven tests for flag parsing, boundary validation, invalid options, and format outputs.
4. **Verification Commands**:
   - `go build ./cmd/generator`
   - `go test -v -race -count=5 ./pkg/generator/... ./cmd/generator/...`
   - `go test -v -race -count=5 ./...` (0 data races project-wide)
   - `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` (must achieve >= 90% statement coverage across all packages, including `pkg/generator`)
   - `go vet ./...` (0 warnings)
   - `gofmt -w .`

Write your handoff report to `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m3/handoff.md` and send a message when done.

## 2026-10-21T12:30:56Z
[Message] timestamp=2026-10-21T12:30:56Z sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8 priority=MESSAGE_PRIORITY_HIGH content=You are teamwork_preview_worker for Milestone 3 (Programmatic Mock Data Generator). Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m3. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m3/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Read the Explorer blueprints in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2/handoff.md, /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_2_gen2/handoff.md, and /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m3_1_gen2/handoff.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement pkg/generator (config.go, ring_buffer.go, generator.go, reader.go, generator_test.go) and cmd/generator (main.go, main_test.go).
Run build and test verification:
1. go build ./cmd/generator
2. go test -v -race -count=5 ./pkg/generator/... ./cmd/generator/...
3. go test -v -race -count=5 ./...
4. go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out (must achieve >= 90% statement coverage)
5. go vet ./...
Write your handoff report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m3/handoff.md.
Send message when done.
