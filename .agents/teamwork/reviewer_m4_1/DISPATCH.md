# Reviewer M4.1 Dispatch: REST APIs, SSE Broker & Sync Engine Hooks

## Identity & Role
- Archetype: teamwork_preview_reviewer
- Role: API & Broker Reviewer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m4_1
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Worker Handoff: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md
- Target Files: `pkg/api/`, `pkg/sync/engine.go`, `pkg/storage/sqlite.go`

## Review Objectives
1. Examine code quality, correctness, and security of `pkg/api/`:
   - REST endpoints (`POST /api/jobs`, `POST /api/jobs/{id}/resume`, `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/dead-letter`, `GET /api/products`).
   - 409 Conflict handling when a job is already in progress.
   - SSE pub/sub broker (`broker.go`): thread safety, bounded client channels (`clientCap`), non-blocking drop for slow consumers, 15s keepalive ping (`: ping\n\n`), subscription cleanup on disconnect.
   - Engine hook `WithProgressHook` in `pkg/sync/engine.go` (emits progress only after batch transaction commits).
   - Storage query extensions in `pkg/storage/sqlite.go` (`ListJobs`, `ListProducts`, `GetBatchCheckpoints`).
2. Execute builds and tests:
   - `go test -v -race -count=5 ./pkg/api/... ./pkg/storage/... ./pkg/sync/...`
   - Statement coverage check for `pkg/api` ($\ge 90\%$).
3. Output verdict in `/Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m4_1/handoff.md`:
   - Either `APPROVE` or `REQUEST_CHANGES` with detailed evidence.


## 2026-10-21T14:03:34Z
You are teamwork_preview_reviewer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m4_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m4_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and worker handoff /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md. Review pkg/api (REST endpoints, SSE broker, middleware, server routing) and engine/storage hooks. Run builds and tests. Provide explicit verdict (APPROVE or REQUEST_CHANGES) in /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m4_1/handoff.md. Send message when done.
