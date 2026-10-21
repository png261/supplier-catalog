# Explorer M4.1 Dispatch: API & SSE Architecture Design

## Identity & Role
- Archetype: teamwork_preview_explorer
- Role: API & SSE Architecture Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Existing Codebases: `pkg/domain`, `pkg/storage`, `pkg/sync`, `pkg/generator`

## Exploration Objectives
1. Design `pkg/api/` package structure:
   - REST endpoints:
     - `POST /api/jobs`: Trigger synchronization job with optional mock generator parameters or supplier updates payload.
     - `POST /api/jobs/{id}/resume`: Resume interrupted job directly from uncommitted batch.
     - `GET /api/jobs`: List past and active sync jobs with progress and change summaries.
     - `GET /api/jobs/{id}`: Detailed job inspection.
     - `GET /api/dead-letter`: Paginated list of quarantined corrupt records with error reasons and payloads.
     - `GET /api/products`: Paginated list of catalog products with status and prices.
     - `GET /api/events`: Server-Sent Events (SSE) streaming live job updates and counter changes.
2. Design SSE broadcasting mechanism:
   - Thread-safe event broker (pub/sub) in pure Go (zero external dependencies).
   - Handles client connect, disconnect, buffer backpressure, and heartbeat ping (`: ping\n\n`) to prevent proxy timeouts.
   - Event formatting: `event: progress\ndata: {"job_id":"...","added":10,...}\n\n`.
3. Design standalone server binary: `cmd/server/main.go` supporting `--port`, `--db`, `--static-dir` flags, graceful shutdown on `SIGINT`/`SIGTERM`, and CORS/logging middleware.
4. Document full technical specification and interface contracts in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/handoff.md`.

## 2026-10-21T13:04:35Z
You are teamwork_preview_explorer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design pkg/api architecture, REST endpoints, SSE pub/sub broker, and cmd/server. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_1/handoff.md. Send message when done.
