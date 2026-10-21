# Task Assignment: Storage & Synchronization Architecture Exploration

You are `teamwork_preview_explorer`.
Your working directory is `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.

Investigate and evaluate technical architecture for:
1. Embedded Database Selection in Go:
   - Compare modern pure-Go or CGO-free options (e.g. `modernc.org/sqlite`, `go.etcd.io/bbolt`) versus CGO sqlite. Consider Docker packaging (<=25MB image size, cross-compilation, CGO_ENABLED=0 friendliness), ACID transactions, and race detector compatibility (`go test -race`).
2. Schema & Storage Design:
   - Products table / bucket (SKU as PK, name, price, status: ACTIVE/WITHDRAWN, supplier_timestamp, updated_at).
   - Sync jobs and batch checkpoints table / bucket (job_id, status: PENDING/RUNNING/INTERRUPTED/COMPLETED, total_batches, committed_batch_index, created_at, updated_at).
   - Dead-letter / issues table (issue_id, job_id, batch_index, raw_payload, reason, created_at).
3. Synchronization Engine & Invariants:
   - Batch ingestion transaction boundaries (atomic per batch).
   - Invariant enforcement logic:
     - Deduplication & idempotency check.
     - Timestamp comparison (T_new >= T_old to apply; T_new < T_old rejected & counted as skipped).
     - Tombstone preservation (if local is WITHDRAWN, ignore status updates back to ACTIVE).
     - Validation (reject negative price, empty SKU, malformed payload to dead-letter queue without failing batch).
4. Crash Resumability & Checkpointing:
   - Checkpoint persistence protocol.
   - Recovery procedure on engine startup: detect incomplete/interrupted jobs, mark INTERRUPTED, resume from last uncommitted batch.
5. Concurrency & Race Safety:
   - In-memory synchronization, mutexes, channel pipelines to pass `go test -race -count=5 ./...`.
6. Write your comprehensive report to `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_1/handoff.md`.
7. Include a progress heartbeat in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_1/progress.md`.
8. Once finished, use `send_message` to notify the Project Orchestrator.


## 2026-10-21T08:31:20Z
[Message] sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8
You are teamwork_preview_explorer. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_1. Read the instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_1/DISPATCH.md and the authoritative request in /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Investigate embedded storage, batch transaction atomicity, crash recovery, and synchronization engine architecture. Write your report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_1/handoff.md. Update progress.md regularly. When complete, send a message to the caller.
