# Worker Dispatch: Milestone 2 Remediation (Zero-Timestamp Dead-Letter Quarantine)

## Mission
Remediate the Invariant D (Dead-Letter Isolation) defect in `pkg/sync/engine.go` where zero-timestamp records bypassed quarantine, and update the test suites accordingly.

## Mandatory Integrity Warning
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Context Files
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md` (authoritative user requirements)
- `/Users/png/01. Project/coderpush/PROJECT.md` (project scope & architecture)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_remed_1/handoff.md` (fix strategy & patch blueprint)
- `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_remed_2/handoff.md` (regression analysis & test fixture guide)
- `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_remed_1/handoff.md` (test specifications)
- `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m2_2/handoff.md` (defect report)

## Assigned Files (Exclusive Write Ownership)
- `pkg/sync/engine.go`
- `pkg/sync/engine_test.go`
- `pkg/sync/resumability_test.go`

## Specific Tasks
1. In `pkg/sync/engine.go`:
   - In `isInvalidRecord(r domain.SupplierUpdate) bool`:
     Ensure `r.Timestamp.IsZero()` is checked and causes `isInvalidRecord` to return `true`.
   - In `getInvalidReason(r domain.SupplierUpdate) string`:
     Ensure when `r.Timestamp.IsZero()`, `"zero or uninitialized timestamp"` is appended to reasons.
2. In `pkg/sync/engine_test.go` (`TestEngine_DeadLetter_InvalidRecords`):
   - Add a test record with `Timestamp: time.Time{}` to verify zero-timestamp dead-lettering.
   - Update expectations to reflect the additional dead-letter item.
3. In `pkg/sync/resumability_test.go` (`TestAC4_DeadLetter_IsolationAndNonHalting`):
   - Add an 11th record with `Timestamp: time.Time{}` to verify dead-letter isolation.
   - Update expected DeadLetter count from 7 to 8.
   - Verify `"timestamp"` is present in expected reason substrings.
4. Run Verification Commands:
   - `go test -v -run TestAC4_Adversarial ./pkg/sync`
   - `go test -v -race -count=5 ./...`
   - `go test -coverprofile=coverage.out ./... && go tool cover -func=coverage.out` (must achieve >= 90% statement coverage)
5. Write your handoff report to `.agents/teamwork/worker_m2_remed/handoff.md` and send a message when done.


## 2026-10-21T11:52:26Z
You are teamwork_preview_worker for Milestone 2 Remediation. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_remed. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_remed/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Read the Explorer blueprints in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_remed_1/handoff.md, /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m2_remed_2/handoff.md, and /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_remed_1/handoff.md.

MANDATORY INTEGRITY WARNING:
DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

Remediate isInvalidRecord and getInvalidReason in pkg/sync/engine.go to properly quarantine zero-timestamp records. Update the test suites in pkg/sync/engine_test.go and pkg/sync/resumability_test.go per the blueprints.
Run build and test verification (go test -v -run TestAC4_Adversarial ./pkg/sync, go test -v -race -count=5 ./..., and coverage >= 90%).
Write your handoff report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2_remed/handoff.md.
Send message when done.
