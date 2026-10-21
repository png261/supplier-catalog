# BRIEFING — 2026-10-21T08:52:00Z

## Mission
Provide detailed algorithmic design and code blueprints for pkg/sync/engine.go enforcing Invariants A, B, and C for Milestone 1.

## 🔒 My Identity
- Archetype: explorer
- Roles: read-only investigation, algorithmic design, blueprint synthesis
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: milestone_1

## 🔒 Key Constraints
- Read-only investigation — do NOT implement
- Enforce Invariant A (Deterministic Idempotency), Invariant B (Fast-Forward Timestamps), and Invariant C (Tombstone Preservation)
- Output detailed algorithmic design and Go blueprints in handoff.md
- All files written must stay inside /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2/

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: not yet

## Investigation State
- **Explored paths**:
  - `PROJECT.md` & `ORIGINAL_REQUEST.md` (Domain interfaces, Sync Engine contracts, Acceptance Criteria)
  - `explorer_m1_1/handoff.md` (SQLite storage layer, connection pooling, `GetProductTx`, `UpsertProductTx`)
  - `spec_miner_m1_1/handoff.md` (Test specifications, 26 features, 31 edge cases, race test harness)
- **Key findings**:
  - Intra-batch caching via `batchState map[string]*domain.Product` is critical to prevent SQLite connection pool exhaustion / deadlocks when `db.SetMaxOpenConns(1)` is active.
  - Invariant evaluation order must be: Invariant A (Idempotency) -> Invariant C (Tombstone lockout) -> Invariant B (Timestamp ordering) -> Valid Upsert.
  - `SyncEngine.mu` mutex serializes batch write transactions across concurrent goroutines, completely eliminating `SQLITE_BUSY` errors while permitting concurrent WAL reads.
- **Unexplored areas**: Milestone 2 batch checkpointing and crash recovery (`RunJob`, `ResumeJob`).

## Key Decisions Made
- Designed `pkg/sync/engine.go` with full implementation of `ExecuteBatch`, atomic transaction lifecycle (`BeginTx` / `Commit` / deferred `Rollback`), and forward-compatible stubs for Milestone 2 methods.
- Standardized change counter arithmetic: Added (+), Updated (~), Withdrawn (-), Skipped (↷), DeadLetter (⚠).
- Designed complete unit test suite (`pkg/sync/engine_test.go`) and race condition suite (`pkg/sync/engine_race_test.go`) targeted for `go test -race -count=5 ./...`.

## Artifact Index
- `DISPATCH.md` — Task assignment and instructions
- `progress.md` — Liveness heartbeat and milestone tasks
- `BRIEFING.md` — Working memory and context index
- `handoff.md` — Complete 5-component deliverable report with Go code blueprints
