# Milestone 2 Independent Review (Crash Recovery & Dead-Letter Isolation Focus)

You are `teamwork_preview_reviewer`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m2_2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.
Worker handoff: `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2/handoff.md`.

Review Focus:
1. Inspect `pkg/sync/engine.go` and `pkg/sync/resumability_test.go`.
2. Verify Startup Crash Recovery:
   - `RecoverOnStartup` transitions stale `RUNNING` or `PENDING` jobs to `INTERRUPTED`.
   - An interrupted job is never reported as `COMPLETED`.
3. Verify Dead-Letter Isolation (Invariant D):
   - Invalid records (negative prices, empty/whitespace SKUs, NaN/Inf, invalid statuses, zero timestamps) logged to `dead_letter_issues` via `RecordDeadLetterTx` without halting batch.
4. Run verification commands:
   - `CGO_ENABLED=0 go build ./...`
   - `go test -v -race -coverprofile=coverage.out ./...` and `go tool cover -func=coverage.out` (verify statement coverage >= 90%).
5. Provide explicit verdict: `APPROVE` or `REQUEST_CHANGES`.
6. Write full report to `/Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m2_2/handoff.md`.
7. Send message to caller when done.

## 2026-10-21T11:24:16Z
You are teamwork_preview_reviewer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m2_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m2_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and worker handoff /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2/handoff.md. Review RecoverOnStartup and Invariant D in pkg/sync/engine.go and tests. Run coverage check. Provide explicit verdict (APPROVE or REQUEST_CHANGES) in /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m2_2/handoff.md. Send message when done.
