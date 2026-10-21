# Milestone 1 Implementation Assignment: Core Domain, SQLite Storage & Synchronization Engine

You are `teamwork_preview_worker`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Explorer Reports & Blueprints to Follow:
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_1/handoff.md`
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2/handoff.md`
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/handoff.md`

MANDATORY INTEGRITY WARNING:
> DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Scope & File Ownership:
You own exclusively:
- `go.mod` & `go.sum`
- `pkg/domain/models.go`
- `pkg/storage/sqlite.go`
- `pkg/storage/sqlite_test.go`
- `pkg/sync/engine.go`
- `pkg/sync/engine_test.go`
- `pkg/sync/engine_race_test.go`

Tasks:
1. Initialize `go.mod` with module `coderpush`, Go 1.25, and `modernc.org/sqlite v1.36.0`. Run `go mod tidy`.
2. Implement `pkg/domain/models.go` with domain entities, statuses, invariants, and validation.
3. Implement `pkg/storage/sqlite.go` with embedded SQLite storage, WAL mode, pragmas, single-writer connection pooling, DDL schemas, `GetProduct`, `GetProductTx`, and `UpsertProductTx`.
4. Implement `pkg/sync/engine.go` with atomic batch execution and enforcement of:
   - Invariant A (Deterministic Idempotency: identical batch produces 0 adds, 0 updates, 0 withdrawals, $N$ skipped)
   - Invariant B (Fast-Forward Timestamps: $T_{new} < T_{old}$ rejected and skipped)
   - Invariant C (Tombstone Preservation: WITHDRAWN is terminal and never restored to ACTIVE)
   - Intra-batch duplicate handling (handling multiple records for same SKU within single batch)
   - Context cancellation & rollback
5. Implement thorough unit and concurrency tests in `pkg/storage/sqlite_test.go`, `pkg/sync/engine_test.go`, and `pkg/sync/engine_race_test.go` covering all 31 edge cases from `spec_miner_m1_1/handoff.md`.
6. Verification commands:
   - `go build ./...`
   - `go test -v -race -count=5 ./pkg/domain/... ./pkg/storage/... ./pkg/sync/...`
   - `go test -v -race -coverprofile=coverage.out ./pkg/domain/... ./pkg/storage/... ./pkg/sync/...`
   - Ensure statement coverage is $\ge 90\%$ and 0 data races.
7. Write your handoff report to `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m1/handoff.md` documenting all files created, tests executed, outputs, and coverage figures.
8. Send a message to the caller upon completion.


## 2026-10-21T08:52:12Z
You are teamwork_preview_worker for Milestone 1. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Read the Explorer blueprints in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_1/handoff.md, /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2/handoff.md, and /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/handoff.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement go.mod, pkg/domain/models.go, pkg/storage/sqlite.go, pkg/storage/sqlite_test.go, pkg/sync/engine.go, pkg/sync/engine_test.go, and pkg/sync/engine_race_test.go.
Run build and test verification (go test -v -race -count=5 ./... and coverage >= 90%).
Write your handoff report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m1/handoff.md.
Send message when done.
