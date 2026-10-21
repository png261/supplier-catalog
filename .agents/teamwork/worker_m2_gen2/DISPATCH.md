# Milestone 2 Implementation Assignment (Replacement Worker - Gen 2)

You are `teamwork_preview_worker`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Previous Worker Status:
Previous worker `76e2d682-5d63-48a5-b583-acc90c06d254` crashed due to a TCP stream read timeout. It had updated the `Storage` interface in `pkg/sync/engine.go` (lines 17-40), but had not yet implemented `RunJob`, `ResumeJob`, or `pkg/sync/resumability_test.go`.

Explorer Reports & Concrete Blueprints:
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_1/handoff.md` (Full implementation blueprints for RunJob, ResumeJob, executeJob, batch checkpoint progression, uncancelled context)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/handoff.md` (RecoverOnStartup & Dead-letter quarantine blueprints)
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/handoff.md` (Complete test specifications & crash simulation harnesses)

MANDATORY INTEGRITY WARNING:
> DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Scope & File Ownership:
You own exclusively:
- `pkg/sync/engine.go`
- `pkg/sync/resumability_test.go`
- `pkg/sync/engine_test.go` (if adaptations needed)
- `pkg/storage/sqlite_test.go` (if adaptations needed)

Tasks:
1. Finish implementing `RunJob(ctx, jobID, batchStream)`, `ResumeJob(ctx, jobID, batchStream)`, `executeJob`, and enhanced Invariant D dead-letter validation in `pkg/sync/engine.go` per the blueprints in `explorer_m2_1/handoff.md` and `explorer_m2_2/handoff.md`.
2. Implement comprehensive crash & resumability test suite in `pkg/sync/resumability_test.go` verifying:
   - AC1: Interruption midway through multi-batch sync leaves prior committed batches intact and uncommitted batch changes rolled back.
   - AC2: Resuming continues directly from uncommitted batch to completion without re-processing completed batches.
   - AC3: Interrupted jobs marked INTERRUPTED upon restart and only transition to COMPLETED when all batches finish.
   - AC4: Records with negative prices or empty SKUs logged to issues table without halting batch.
3. Verification commands:
   - `CGO_ENABLED=0 go build ./...`
   - `go vet ./...` & `gofmt -s -l .`
   - `go test -v -race -count=5 ./pkg/domain/... ./pkg/storage/... ./pkg/sync/...`
   - `go test -v -race -coverprofile=coverage.out ./...` and `go tool cover -func=coverage.out` (verify statement coverage >= 90%).
4. Write your handoff report to `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2/handoff.md`.
5. Send message when done.


## 2026-10-21T11:05:47Z
You are teamwork_preview_worker (Replacement Gen 2) for Milestone 2. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and explorer blueprints in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_1/handoff.md, /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_2/handoff.md, and /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_1/handoff.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Implement RunJob, ResumeJob, RecoverOnStartup, dead-letter quarantine (Invariant D), and atomic batch transaction commits (Invariant E) in pkg/sync/engine.go. Implement comprehensive crash & resumability test suite in pkg/sync/resumability_test.go covering all Milestone 2 acceptance criteria.
Run build and test verification (go test -v -race -count=5 ./... and coverage >= 90%).
Write your handoff report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_gen2/handoff.md.
Send message when done.
