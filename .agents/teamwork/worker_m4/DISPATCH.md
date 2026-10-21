# Milestone 4 Worker Dispatch: Embedded Staff Web UI & Control Plane

## Identity & Role
- Archetype: teamwork_preview_worker
- Role: Milestone 4 Implementation Worker
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Blueprints
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Technical Blueprints:
  - API & SSE Architecture: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/handoff.md
  - UI & Static Asset Design: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2/handoff.md
  - API Contract & Test Matrix: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/handoff.md

## Implementation Scope
1. **`pkg/storage/`**:
   - In `pkg/storage/sqlite.go`, implement:
     - `ListJobs(ctx context.Context, limit, offset int) ([]domain.SyncJob, int, error)`
     - `ListProducts(ctx context.Context, status string, search string, limit, offset int) ([]domain.Product, int, error)`
     - `GetBatchCheckpoints(ctx context.Context, jobID string) ([]domain.BatchCheckpoint, error)`
   - Add unit tests in `pkg/storage/sqlite_test.go`.
2. **`pkg/sync/`**:
   - In `pkg/sync/engine.go`, add optional `ProgressHook` functional option:
     ```go
     type ProgressHook func(jobID string, batchIndex int, totalBatches int, delta domain.ChangeSummary, cumulative domain.ChangeSummary)
     func WithProgressHook(hook ProgressHook) Option { ... }
     ```
   - In `processBatchTx`, invoke `e.progressHook` after committing the batch transaction.
3. **`pkg/api/`**:
   - `models.go`: Request & response models per `spec_miner_m4_1` schemas (`JobStartRequest`, `JobResumeRequest`, `JobListResponse`, `JobDetailResponse`, `DeadLetterListResponse`, `ProductListResponse`, `ErrorResponse`).
   - `broker.go`: Thread-safe pure Go Server-Sent Events (SSE) pub/sub broker with bounded client channels (`chan Event`), slow client non-blocking drop, 15s keepalive ping (`: ping\n\n`), and clean termination.
   - `handlers.go`: Handlers for `POST /api/jobs`, `POST /api/jobs/{id}/resume`, `GET /api/jobs`, `GET /api/jobs/{id}`, `GET /api/dead-letter`, `GET /api/products`, `GET /api/events`.
   - `server.go`: Router registration on standard library `http.ServeMux` (Go 1.22+ method and path routing).
   - `api_test.go`: Comprehensive unit and concurrency tests covering all endpoints, 409 conflict detection, SSE streaming, dead-letter filtering, and error handling.
4. **`web/`**:
   - `embed.go`: `//go:embed static/*` declaration, `AssetHandler(staticDir string)` with SPA fallback to `index.html`, and `init()` with `mime.AddExtensionType` for `.js`, `.css`, etc.
   - `embed_test.go`: Verification of static file serving, correct MIME types, and SPA fallback.
   - `static/index.html`: Minimalist, responsive single-page dashboard per `explorer_m4_2/handoff.md`.
   - `static/style.css`: Clean modern dark/light CSS styling, design tokens, cards, and tables.
   - `static/app.js`: Vanilla ES6+ frontend logic with `EventSource` live updates, animated metric cards, sync controls, and dead-letter modal.
5. **`cmd/server/`**:
   - `main.go`: Standalone server binary parsing `--port`, `--db`, `--static-dir` flags; runs startup crash recovery via `engine.RecoverOnStartup`; graceful shutdown on `SIGINT`/`SIGTERM`.
   - `main_test.go`: Flag parsing tests, lifecycle tests, and server integration tests.

## Verification Mandate
The worker MUST run:
1. `go build ./cmd/server`
2. `go test -v -race -count=5 ./pkg/api/... ./web/... ./cmd/server/...`
3. `go test -v -race -count=5 ./...` (must pass with 0 data races)
4. `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` (must achieve statement coverage $\ge 90\%$ project-wide and on new packages)
5. `go vet ./...`
6. `gofmt -s -w .`

Write comprehensive handoff report to `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md`.
Send message upon completion.

## 2026-10-21T13:38:53Z
You are teamwork_preview_worker for Milestone 4 (Embedded Staff Web UI & Control Plane). Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Read the Explorer blueprints in:
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_2/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/handoff.md

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement:
1. pkg/storage/ extensions (ListJobs, ListProducts, GetBatchCheckpoints) and tests.
2. pkg/sync/ WithProgressHook option and invocation in processBatchTx.
3. pkg/api/ (models.go, broker.go, handlers.go, server.go, api_test.go).
4. web/ (embed.go, embed_test.go, static/index.html, static/style.css, static/app.js).
5. cmd/server/ (main.go, main_test.go).

Run verification:
1. go build ./cmd/server
2. go test -v -race -count=5 ./pkg/api/... ./web/... ./cmd/server/...
3. go test -v -race -count=5 ./...
4. go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out (must achieve >= 90% statement coverage project-wide and in new packages)
5. go vet ./...
6. gofmt -s -w .

Write your handoff report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md.
Send message when done.
