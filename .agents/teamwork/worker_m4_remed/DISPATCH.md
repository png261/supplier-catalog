# DISPATCH — Milestone 4 Remediation Worker (worker_m4_remed)

## Objective
Implement all three verified remediation blueprints for Milestone 4:
1. Concurrency TOCTOU on `POST /api/jobs` and `POST /api/jobs/{id}/resume` in `pkg/api/handlers.go`.
2. Dead-Letter Queue `reason` filter across `pkg/storage/sqlite.go`, `pkg/api/handlers.go`, `pkg/sync/engine.go`, `PROJECT.md`, `pkg/storage/sqlite_test.go`, and all test call sites. Also enhance `web/static/index.html` and `web/static/app.js`.
3. SSE Broker atomic client ID generation and connection lifecycle in `pkg/api/broker.go`.

## References & Authoritative Reports
Subagents MUST read these authoritative blueprints:
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- `/Users/png/01. Project/coderpush/PROJECT.md`
- `/Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md` (Integrity audit evidence)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handoff.md` & `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch`
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/handoff.md` & `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/dlq_reason_filter.patch`
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/handoff.md`

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Tasks & Files Owned
You own and must modify:
1. `pkg/api/handlers.go` (concurrency TOCTOU serialization & DLQ reason extraction)
2. `pkg/api/broker.go` (atomic client ID generation, connection registration ordering, unregister pointer guard)
3. `pkg/storage/sqlite.go` & `pkg/storage/sqlite_test.go` (`ListDeadLetterRecords` signature, parameterized `reason LIKE '%' || ? || '%'`, new test cases)
4. `pkg/sync/engine.go` (mirror `Storage` interface)
5. `PROJECT.md` (interface contract update)
6. All test call sites:
   - `pkg/sync/engine_test.go`
   - `pkg/sync/resumability_test.go`
   - `pkg/sync/engine_ac4_stress_test.go`
   - `pkg/sync/engine_deep_adversarial_test.go`
   - `pkg/sync/engine_ac4_remed_challenger_test.go`
   - `pkg/sync/engine_resumability_challenger_test.go`
   - `pkg/generator/generator_test.go`
   - `pkg/generator/generator_challenger_test.go`
7. `web/static/index.html` & `web/static/app.js` (reason filter input in Staff Dashboard)

## Verification Commands
Worker MUST execute and document verbatim:
1. `go build ./cmd/server`
2. `go build ./cmd/generator`
3. `go test -v -race -count=5 ./pkg/api/...`
4. `go test -v -race -count=5 ./pkg/storage/...`
5. `go test -v -race -count=5 ./pkg/sync/...`
6. `go test -v -race -count=5 ./pkg/generator/...`
7. `go test -v -race -count=5 ./...`
8. `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` (must achieve >= 90% statement coverage project-wide)
9. `go vet ./...`
10. `gofmt -s -w .`

Write your comprehensive completion report to `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m4_remed/handoff.md`.
Send a message when done.

## 2026-10-21T14:36:04Z
You are teamwork_preview_worker for Milestone 4 Remediation. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4_remed.
Read instructions in:
- /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4_remed/DISPATCH.md
- /Users/png/01. Project/coderpush/PROJECT.md
- /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handoff.md & /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handlers_concurrency_fix.patch
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/handoff.md & /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/dlq_reason_filter.patch
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/handoff.md

Implement the three verified remediation fixes:
1. Concurrency TOCTOU on POST /api/jobs and POST /api/jobs/{id}/resume in pkg/api/handlers.go.
2. Dead-Letter Queue reason filter in pkg/storage/sqlite.go, pkg/api/handlers.go, pkg/sync/engine.go, PROJECT.md, and all 18 test call sites across pkg/sync/ and pkg/generator/. Enhance web/static/index.html and web/static/app.js.
3. SSE Broker atomic client ID generation, connection registration ordering, and safe unregistration in pkg/api/broker.go.
