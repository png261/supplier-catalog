# Explorer Dispatch (Gen 2): Milestone 3 CLI & Streaming I/O Design

## Mission
Design the CLI entry point (`cmd/generator/main.go`) and streaming I/O format for the mock generator:
1. **CLI Flag Specification**:
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
2. **Streaming Execution Flow**:
   - How `main.go` parses flags, sets up buffered I/O, streams records to stdout/pipe with minimal latency and constant memory.
   - Signal handling: clean shutdown on `SIGINT` / `SIGTERM`.
3. **Integration with Sync Engine**:
   - How the generated output feeds into `SyncEngine.RunJob` / `ExecuteBatch` directly or via pipe.

Write your blueprint report to `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_2_gen2/handoff.md`.

## Context Files
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md` (read first)
- `/Users/png/01. Project/coderpush/PROJECT.md`
- `/Users/png/01. Project/coderpush/pkg/domain/models.go`

Send message when done.


## 2026-10-21T12:22:52Z
You are teamwork_preview_explorer (Gen 2). Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_2_gen2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_2_gen2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design CLI entry point cmd/generator/main.go and streaming I/O format. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_2_gen2/handoff.md. Send message when done.
