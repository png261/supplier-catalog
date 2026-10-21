# Milestone 2 Exploration: Startup Crash Recovery & Dead-Letter Queue

You are `teamwork_preview_explorer`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.
Completed M1 baseline: `pkg/domain`, `pkg/storage`, `pkg/sync`.

Milestone 2 Scope:
1. Design `RecoverOnStartup(ctx)` in `pkg/sync/engine.go` & `storage.RecoverInterruptedJobs(ctx)`:
   - On startup, scan for any jobs with status `RUNNING` or `PENDING` left over from an interrupted or crashed process.
   - Transition them to `INTERRUPTED` status and record in logs/audit.
   - Guarantee that an interrupted job is NEVER reported as `COMPLETED`.
2. Design Dead-Letter Queue isolation (Invariant D):
   - Fully integrate `RecordDeadLetterTx` and `ListDeadLetterRecords` in `pkg/storage/sqlite.go`.
   - Quarantine invalid records (negative prices, empty/whitespace SKUs, invalid timestamp, NaN/Inf prices) into `dead_letter_issues` table.
   - Verify that dead-lettered records do NOT halt or fail valid records in the batch.
3. Provide concrete Go blueprints and architecture recommendations in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/handoff.md`.
4. Keep heartbeat in `progress.md`. Send message when done.

## 2026-10-21T09:16:45Z
You are teamwork_preview_explorer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design RecoverOnStartup and Dead-Letter queue isolation. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/handoff.md. Send message when done.
