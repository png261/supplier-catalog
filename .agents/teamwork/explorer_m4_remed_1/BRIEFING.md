# BRIEFING — 2026-10-21T14:31:00Z

## Mission
Formulate a robust fix strategy for Concurrency TOCTOU on POST /api/jobs and POST /api/jobs/{id}/resume in pkg/api/handlers.go.

## 🔒 My Identity
- Archetype: teamwork_preview_explorer
- Roles: Concurrency Remediation Explorer
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: M4 Remediation 1

## 🔒 Key Constraints
- Read-only investigation — do NOT implement directly in source code
- Produce structured report at /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handoff.md
- Use send_message to notify parent upon completion

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: 2026-10-21T14:21:19Z

## Investigation State
- **Explored paths**:
  - `pkg/api/handlers.go` (lines 85-275, 325-450)
  - `pkg/api/adversarial_test.go` (`TestAdversarial_JobConcurrency_Race`, `TestAdversarial_InvalidResumptionScenarios`)
  - `pkg/storage/sqlite.go` (`sync_jobs` schema, `CreateJob`, `GetActiveJob`, `UpdateJobStatus`)
  - `pkg/sync/engine.go` (`RunJob`, `ResumeJob`, `executeJob`)
  - `.agents/teamwork/auditor_m4/handoff.md`
  - `.agents/teamwork/challenger_m4_1/handoff.md`
- **Key findings**:
  - Root Cause 1 (`handleStartJob`): Active job check (`h.storage.GetActiveJob(ctx)`) and job creation (`h.storage.CreateJob(ctx, job)`) are completely unsynchronized. Under concurrent bursts, multiple goroutines see `GetActiveJob == nil`, create distinct jobs in SQLite, and return HTTP 202.
  - Root Cause 2 (`handleResumeJob`): Checking target job status and resumption dispatch are unsynchronized, and `handleResumeJob` never synchronously updated job status to `RUNNING` in SQLite before returning HTTP 202. Multiple resume requests read `INTERRUPTED` status simultaneously and return 202.
  - Remediation Architecture:
    1. Parse and validate request bodies outside `h.mu` to minimize lock contention.
    2. Synchronize active job check, storage mutation (`CreateJob` or `UpdateJobStatus` to `RUNNING`), and in-memory registration (`h.activeJobID`, `h.activeCancel`, `h.activeConfigs`) under `h.mu.Lock()`.
    3. Perform HTTP response writing, SSE broadcast, and background goroutine execution outside the critical section.
    4. Clean deferred unlock in background goroutines clearing `h.activeJobID` and `h.activeCancel`.
- **Unexplored areas**: None for concurrency TOCTOU scope; DLQ reason filter and broker client ID are handled by peer agents.

## Key Decisions Made
- Generated unified git diff patch `handlers_concurrency_fix.patch`.
- Validated patch with dry-run test: `TestAdversarial_JobConcurrency_Race` and `TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace` passed 100% cleanly (Accepted=1, Conflict=19 and Accepted=1, Conflict=9).
- Reverted source tree to ensure strict adherence to read-only investigation constraint.

## Artifact Index
- DISPATCH.md — Dispatch instructions and inputs
- BRIEFING.md — Persistent working memory
- progress.md — Liveness heartbeat
- handlers_concurrency_fix.patch — Verified unified git diff patch for `pkg/api/handlers.go`
- handoff.md — 5-component comprehensive handoff report
