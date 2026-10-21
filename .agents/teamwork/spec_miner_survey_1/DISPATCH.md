# Task Assignment: Requirement & Domain Spec Mining

You are `teamwork_preview_spec_miner`.
Your working directory is `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.

Extract and document:
1. Complete inventory of requirements (R1 through R5) and acceptance criteria.
2. Exact domain model (Product, Supplier Update, Batch, Job, Checkpoint, Dead-Letter Record, Change Summary).
3. Core invariants:
   - Invariant A: Deterministic idempotency (ingesting identical batch produces 0 inserts/updates, 0 drift).
   - Invariant B: Fast-forward out-of-order resolution (T_new < T_old rejected & skipped).
   - Invariant C: Tombstone preservation (WITHDRAWN is terminal and never flipped back to ACTIVE).
   - Invariant D: Dead-letter isolation (corrupt/invalid records like negative price or missing SKU go to dead-letter queue without failing batch).
   - Invariant E: Atomic batch transactions & crash resumability (committed batches intact, uncommitted rolled back, resume continues from uncommitted batch).
4. Edge cases, boundary values, and failure modes to guide E2E test tier design.
5. Write your comprehensive report to `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1/handoff.md`.
6. Include a progress heartbeat in `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1/progress.md`.
7. Once finished, use `send_message` to notify the Project Orchestrator.

## 2026-10-21T08:31:20Z
You are teamwork_preview_spec_miner. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1. Read the instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1/DISPATCH.md and the authoritative request in /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Extract and document all specifications, domain invariants, edge cases, and acceptance criteria into /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_survey_1/handoff.md. Update progress.md regularly. When complete, send a message to the caller.
