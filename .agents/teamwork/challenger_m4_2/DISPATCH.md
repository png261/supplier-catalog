# Challenger M4.2 Dispatch: SSE Pub/Sub, Multi-Client Fanout & Telemetry Challenge

## Identity & Role
- Archetype: teamwork_preview_challenger
- Role: SSE Telemetry & Broker Challenger
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_2
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Worker Handoff: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md
- Target Packages: `pkg/api`, `web`, `cmd/server`

## Challenge Objectives
1. Implement independent adversarial tests:
   - **Multi-Client Fanout & Zero Leak**: Connect 20 concurrent SSE clients (`GET /api/events`). Trigger a sync job with multiple batches. Verify all 20 clients receive identical progression of events (`job_started`, `progress`, `job_completed`). Verify that when 10 clients disconnect abruptly, the broker cleans up their subscriptions without panics, deadlocks, or goroutine leaks.
   - **Slow Consumer Isolation (Backpressure)**: Introduce a mock client that deliberately pauses or does not read from its channel. Stream 100+ events. Verify that the slow client's events are dropped non-blockingly without delaying normal clients or blocking the sync engine.
   - **Keepalive Ping Verification**: Verify that the broker emits `: ping\n\n` comments on idle streams without breaking W3C client parsing.
   - **Metric Counters Fidelity**: Verify that the cumulative change metrics emitted over SSE (+Added, ~Updated, -Withdrawn, ↷Skipped, ⚠Dead-Lettered) match the database `sync_jobs.summary` and SQLite catalog rows with 100% exact numerical agreement.
2. Execute tests under `go test -v -race -count=5`.
3. Output verdict in `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_2/handoff.md`:
   - Either `APPROVE` or `CONFIRM_CORRECTNESS_FAILED` with detailed empirical logs.


## 2026-10-21T14:03:34Z
You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially challenge SSE pub/sub multi-client fanout, slow consumer isolation (non-blocking drops), 15s keepalive ping, and exact numerical fidelity of live metric counters against SQLite database. Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_2/handoff.md. Send message when done.
