# BRIEFING — 2026-10-21T13:13:00Z

## Mission
Design pkg/api architecture, REST endpoints, SSE pub/sub broker, and cmd/server standalone binary for Milestone M4.

## 🔒 My Identity
- Archetype: teamwork_preview_explorer
- Roles: API & SSE Architecture Explorer
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: M4

## 🔒 Key Constraints
- Read-only investigation — do NOT implement production code in pkg/ or cmd/ directly.
- Pure Go standard library for networking, HTTP, and SSE (zero external web frameworks like gin/fiber/echo, zero external SSE libraries).
- Concurrency safety: thread-safe SSE broker, race-free channels, graceful shutdown.
- Strict layout compliance with PROJECT.md and compatibility with pkg/domain, pkg/storage, pkg/sync, pkg/generator.

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: not yet

## Investigation State
- **Explored paths**:
  - `pkg/domain/models.go` (Product, SupplierUpdate, Batch, ChangeSummary, SyncJob, BatchCheckpoint, DeadLetterRecord)
  - `pkg/storage/sqlite.go` (SQLiteStorage, schema, transaction methods, existing query methods)
  - `pkg/sync/engine.go` (SyncEngine, processBatchTx, executeJob, RunJob, ResumeJob, RecoverOnStartup)
  - `pkg/generator/config.go`, `generator.go`, `reader.go` (Config, Generator, StreamBatches, UnmarshalBatchJSON)
  - `cmd/generator/main.go` (CLI flags, stream handling, signal cancellation pattern)
  - `web/embed.go` integration path with `explorer_m4_2`
- **Key findings**:
  - Storage is currently missing `ListJobs` and `ListProducts` query methods required for REST endpoints.
  - SyncEngine requires a non-invasive `WithProgressHook` functional option to emit per-batch metrics to the SSE broker.
  - Pure Go SSE broker can be implemented using an actor pattern or RWMutex with buffered client channels and non-blocking drops for slow consumers.
  - Heartbeat pings (`: ping\n\n`) every 15s prevent reverse proxy timeouts.
  - Server `http.Server.WriteTimeout` must be 0 to allow long-lived SSE connections.
- **Unexplored areas**: None. All architectural facets are thoroughly analyzed.

## Key Decisions Made
- Use standard library `net/http` with Go 1.22+ `http.ServeMux` method routing and path parameter extraction (`req.PathValue("id")`).
- Implement in-memory `JobManager` inside `pkg/api.Handler` to synchronize active job cancellation and prevent concurrent job runs (returning 409 Conflict).
- Propose two new methods in `storage.Storage`: `ListJobs(ctx, limit, offset)` and `ListProducts(ctx, status, search, limit, offset)`.
- Use functional option `sync.WithProgressHook` to wire `SyncEngine` with the SSE broker without modifying existing callers or tests.

## Artifact Index
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/DISPATCH.md` — Task instructions
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/progress.md` — Liveness and step tracking
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/handoff.md` — Complete architectural specification
