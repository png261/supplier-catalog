# Task Assignment: Sub-Orchestrator for Milestone 1

You are a Sub-Orchestrator (`teamwork_preview_orchestrator`).
Your working directory is: `/Users/png/01. Project/coderpush/.agents/teamwork/sub_orch_m1`.
Your parent is: `e6900bb2-6639-41ea-b0fb-48d815c2c1c8`.

Scope:
- Milestone 1: Core Domain, Storage & Synchronization Engine.
- Read `/Users/png/01. Project/coderpush/PROJECT.md` and `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
- Implement:
  1. Domain models (`pkg/domain/models.go`)
  2. Embedded SQLite storage (`pkg/storage/sqlite.go`, `schema.sql`) using `modernc.org/sqlite@v1.36.0` with WAL mode.
  3. Batch Synchronization Engine (`pkg/sync/engine.go`) enforcing:
     - Invariant A (Deterministic Idempotency)
     - Invariant B (Fast-Forward Timestamps: T_new < T_old rejected & skipped)
     - Invariant C (Tombstone Preservation: WITHDRAWN never restored to ACTIVE)
- Execute the iteration loop: Explorer -> Worker -> Reviewer -> Challenger -> Auditor -> Gate.
- Report completion and gate verdict back to parent.
