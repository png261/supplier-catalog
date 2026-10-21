# Milestone 2 Implementation Assignment: Resumability, Batch Checkpointing & Dead-Letter Isolation

You are `teamwork_preview_worker`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Explorer Reports & Blueprints to Follow:
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_1/handoff.md` (RunJob, ResumeJob, batch checkpointing blueprints)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/handoff.md` (Startup recovery & dead-letter queue blueprints)
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/handoff.md` (Crash test specifications, oracles & harnesses)

MANDATORY INTEGRITY WARNING:
> DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Scope & File Ownership:
You own exclusively:
- `pkg/sync/engine.go`
- `pkg/sync/resumability_test.go`
- `pkg/sync/engine_test.go` (if updates needed)
- `pkg/storage/sqlite_test.go` (if updates needed)

Tasks:
1. Update `pkg/sync/engine.go`:
   - Expand `Storage` interface in `engine.go` with `CreateJob`, `GetJob`, `GetActiveJob`, `UpdateJobStatus`, `RecordBatchCheckpointTx`, and `UpdateJobProgressTx`.
   - Implement `RunJob(ctx, jobID, batchStream)`: streaming batches, foreign key safe job creation, skipping already committed batches, atomic batch commits, and transition to `COMPLETED` on stream close.
   - Implement `ResumeJob(ctx, jobID, batchStream)`: verifies existing job, checks status not completed, skips batches with `batch_index <= committed_batch_index`, resumes streaming execution to `COMPLETED`.
   - Implement cancellation handling: if `ctx` cancelled, active batch transaction rolls back, and detached context updates job status to `INTERRUPTED`.
   - Implement `RecoverOnStartup(ctx)` delegating to `storage.RecoverInterruptedJobs(ctx)`.
   - Implement Invariant D: record validation (empty SKU, negative price, invalid status, zero timestamp, NaN/Inf) routes to `dead_letter_issues` via `RecordDeadLetterTx` without halting the batch.
2. Implement comprehensive crash & resumability test suite in `pkg/sync/resumability_test.go`:
   - AC1: Interruption midway through multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back.
   - AC2: Resuming continues directly from uncommitted batch to completion without re-processing completed batches.
   - AC3: Interrupted jobs marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish.
   - AC4: Records with negative prices or empty SKUs logged to issues table without halting batch.
3. Verification commands:
   - `CGO_ENABLED=0 go build ./...`
   - `go vet ./...` & `gofmt -s -l .`
   - `go test -v -race -count=5 ./pkg/domain/... ./pkg/storage/... ./pkg/sync/...`
   - `go test -v -race -coverprofile=coverage.out ./...` and `go tool cover -func=coverage.out` (verify statement coverage >= 90%).
4. Write your handoff report to `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m2/handoff.md`.
5. Send message when done.

## 2026-10-21T09:23:01Z
You are teamwork_preview_worker for Milestone 2. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Read Explorer blueprints in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_1/handoff.md, /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/handoff.md, and /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/handoff.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement RunJob, ResumeJob, RecoverOnStartup, dead-letter quarantine (Invariant D), and atomic batch transaction commits (Invariant E) in pkg/sync/engine.go. Implement comprehensive crash & resumability test suite in pkg/sync/resumability_test.go covering all Milestone 2 acceptance criteria.
Run build and test verification (go test -v -race -count=5 ./... and coverage >= 90%).
Write your handoff report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2/handoff.md.
Send message when done.
