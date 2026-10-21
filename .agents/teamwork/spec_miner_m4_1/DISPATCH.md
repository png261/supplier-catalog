# Spec Miner M4 Dispatch: Control Plane Specification & Test Matrix

## Identity & Role
- Archetype: teamwork_preview_spec_miner
- Role: Control Plane Spec Miner
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md (Requirement R4)
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md (Features 14-20)
- Existing Storage & Sync APIs: `pkg/storage`, `pkg/sync`, `pkg/generator`

## Exploration Objectives
1. Formalize REST API contracts:
   - Request and response JSON schemas for all endpoints.
   - HTTP status codes (200, 201, 202, 400, 404, 409 Conflict when job already running, 500).
   - Error response format: `{"error": "message", "code": "INVALID_REQUEST"}`.
2. Formalize Server-Sent Events (SSE) protocol:
   - Event types: `job_started`, `progress`, `job_completed`, `job_interrupted`, `dead_letter`.
   - Reconnection semantics (`retry: 3000\n`).
   - Ping keepalive every 15s.
3. Design comprehensive automated test matrix for `pkg/api/` and `cmd/server/`:
   - Unit tests for HTTP handlers using `net/http/httptest`.
   - SSE client connection, streaming reception, and disconnect tests.
   - Concurrent job trigger protection (cannot start job while another job is active -> 409 Conflict).
   - Resume job trigger verification.
   - Dead-letter pagination and filtering tests.
   - Embedded static file serving tests (assert 200 OK and valid MIME types for `index.html`, `style.css`, `app.js`).
   - Server CLI flags and graceful shutdown tests (`cmd/server/main_test.go`).
   - Statement coverage target $\ge 90\%$ and race detection (`-race -count=5`).
4. Document all specs and test matrix in `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/handoff.md`.

## 2026-10-21T13:04:35Z
You are teamwork_preview_spec_miner. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Define REST schemas, SSE protocol specifications, edge-case behavior, and comprehensive test suite matrix for pkg/api and cmd/server. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_1/handoff.md. Send message when done.
