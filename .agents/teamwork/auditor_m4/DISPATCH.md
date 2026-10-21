# Forensic Auditor M4 Dispatch: Milestone 4 Integrity Forensics

## Identity & Role
- Archetype: teamwork_preview_auditor
- Role: Milestone 4 Forensic Integrity Auditor
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Worker Handoff: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md
- Implementation Code: `pkg/api/`, `web/`, `cmd/server/`, `pkg/storage/sqlite.go`, `pkg/sync/engine.go`

## Forensic Audit Protocol
Execute comprehensive forensic integrity analysis across Milestone 4 deliverables:
1. **Hardcoding & Pre-computed Constants Check**:
   - Inspect `pkg/api/`, `web/`, and `cmd/server/` for hardcoded job statuses, dummy counter values, fake SSE streams, or mock responses.
   - Verify that REST responses genuinely query SQLite database tables (`sync_jobs`, `batch_checkpoints`, `dead_letter_issues`, `products`).
2. **Fake Streams & Facades**:
   - Verify that `GET /api/events` streams live events from the sync engine via `WithProgressHook` and `Broker`, rather than emitting simulated progress from fixed timers or static loops.
   - Check that `web/embed.go` genuinely embeds physical assets from `web/static/` using standard Go 1.16+ `embed.FS`.
3. **Frontend Authenticity**:
   - Verify that `web/static/app.js` genuinely opens an `EventSource`, updates the DOM dynamically based on parsed SSE event payloads, and provides functional form controls for triggering/resuming jobs.
4. **Test Integrity**:
   - Inspect `pkg/api/*_test.go`, `web/*_test.go`, and `cmd/server/*_test.go` to ensure tests do not bypass assertions or test mock objects instead of real storage/engine components.
5. **Static & Execution Validation**:
   - Run `go vet ./pkg/api/... ./web/... ./cmd/server/...`
   - Run `go test -v -race -count=5 ./pkg/api/... ./web/... ./cmd/server/...`
6. Output binary verdict in `/Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md`:
   - Either `CLEAN` or `INTEGRITY VIOLATION` with full forensic evidence.


## 2026-10-21T14:03:34Z
From: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
Content: You are teamwork_preview_auditor. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and worker handoff /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md. Perform forensic integrity audit on Milestone 4 deliverables (pkg/api, web, cmd/server, storage/sync extensions). Check for hardcoded responses, fake SSE streams, dummy dashboard counters, or bypassed validation. Provide binary verdict (CLEAN or INTEGRITY VIOLATION) in /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md. Send message when done.
