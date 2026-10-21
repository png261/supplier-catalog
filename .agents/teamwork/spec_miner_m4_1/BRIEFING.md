# BRIEFING — 2026-10-21T13:13:30Z

## Mission
Discover, probe, formalize, and document the complete REST API schemas, SSE protocol specifications, edge cases, and comprehensive automated test suite matrix for Milestone 4 (`pkg/api` and `cmd/server`).

## 🔒 My Identity
- Archetype: teamwork_preview_spec_miner
- Roles: Control Plane Spec Miner
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: Milestone 4 (Web API & UI Dashboard)

## 🔒 Key Constraints
- Define REST schemas, SSE protocol specifications, edge-case behavior, and comprehensive test suite matrix for `pkg/api` and `cmd/server`.
- Statement coverage target >= 90% and race detection (`-race -count=5`).
- Error response format: `{"error": "message", "code": "CODE"}`.
- Enforce HTTP status codes: 200, 201, 202, 400, 404, 409 Conflict when job is already running, 500.
- SSE protocol: event types (`job_started`, `progress`, `job_completed`, `job_interrupted`, `dead_letter`), reconnection retry (`retry: 3000\n`), ping keepalive every 15s (`: ping\n\n`).
- Do NOT implement production code — specification discovery & mining only.

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: 2026-10-21T13:13:30Z

## Task Summary
- **What to build**: Comprehensive formal specification and test matrix for Milestone 4 control plane (`pkg/api`, `cmd/server`, `web`).
- **Success criteria**: Handoff report in `handoff.md` containing Features Discovered table, Edge Cases table, full JSON schemas, SSE protocol specifications, and an exhaustive test matrix covering >= 90% coverage and zero data races.
- **Interface contracts**: `PROJECT.md`, `ORIGINAL_REQUEST.md`, `DISPATCH.md`, `pkg/domain`, `pkg/storage`, `pkg/sync`, `pkg/generator`
- **Code layout**: `PROJECT.md § Code Layout`

## Key Decisions Made
- Established canonical JSON schemas for `JobStartRequest`, `JobResumeRequest`, `SyncJobResponse`, `JobListResponse`, `JobDetailResponse`, `DeadLetterListResponse`, `ProductListResponse`, and `ErrorResponse`.
- Formalized SSE event stream (`GET /api/events`) with initial `retry: 3000\n\n`, 15-second keepalive `: ping\n\n`, and 5 typed events (`job_started`, `progress`, `dead_letter`, `job_completed`, `job_interrupted`).
- Designed non-blocking pub/sub event broker with channel buffer size 32 and zero goroutine leak on client disconnect.
- Specified 34 test cases covering unit tests, SSE streaming, concurrency conflict protection, graceful shutdown, and race detection.
- Mandated `mime.AddExtensionType` registration in `pkg/api` to prevent missing MIME types in minimal Docker containers.

## Artifact Index
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/DISPATCH.md` — Assigned objectives and constraints
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/BRIEFING.md` — Agent state and identity
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/progress.md` — Liveness heartbeat and milestone tracking
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/handoff.md` — Final formal specification & test matrix report
