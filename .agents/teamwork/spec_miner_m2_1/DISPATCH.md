# Milestone 2 Exploration: Test Strategy & Crash/Recovery Oracles

You are `teamwork_preview_spec_miner`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.
Completed M1 baseline: `pkg/domain`, `pkg/storage`, `pkg/sync`.

Milestone 2 Scope:
1. Define test cases for Acceptance Criteria:
   - "Simulating an interruption midway through a multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back."
   - "Resuming an interrupted sync continues directly from the uncommitted batch to completion without re-processing completed batches."
   - "Interrupted jobs are marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish."
   - "Records with negative prices or empty SKUs are logged to the issues table without halting the batch."
2. Design crash simulation harnesses:
   - Mid-batch context cancellation.
   - Sudden failure injection during batch $k$ of $N$.
   - Verifying database WAL rollback of uncommitted batch $k$, while batches $0 \dots k-1$ remain durably in SQLite.
   - Resuming job and verifying final state integrity and change metrics.
3. Provide concrete test designs and edge cases in `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/handoff.md`.
4. Keep heartbeat in `progress.md`. Send message when done.

## 2026-10-21T09:16:45Z
You are teamwork_preview_spec_miner. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design test specifications and crash simulation oracles for Milestone 2. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/handoff.md. Send message when done.
