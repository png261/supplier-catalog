# BRIEFING — 2026-10-21T09:23:30Z

## Mission
Implement RunJob, ResumeJob, RecoverOnStartup, dead-letter quarantine, and atomic batch checkpointing in pkg/sync/engine.go with a comprehensive test suite in pkg/sync/resumability_test.go.

## 🔒 My Identity
- Archetype: teamwork_preview_worker
- Roles: implementer, qa, specialist
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: Milestone 2: Resumability, Batch Checkpointing & Dead-Letter Isolation

## 🔒 Key Constraints
- DO NOT CHEAT. All implementations must be genuine.
- Exclusively own: pkg/sync/engine.go, pkg/sync/resumability_test.go, pkg/sync/engine_test.go, pkg/storage/sqlite_test.go.
- Do not modify files in .agents/teamwork/ other than worker_m2/.
- Atomic batch transactions, dead-letter quarantine (Invariant D), Invariant E.
- Verification: CGO_ENABLED=0 go build ./..., go vet ./..., gofmt -s -l ., go test -v -race -count=5 ./..., coverage >= 90%.

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: not yet

## Task Summary
- **What to build**: Implement RunJob, ResumeJob, RecoverOnStartup, dead-letter quarantine, atomic batch commits in pkg/sync/engine.go, and comprehensive crash & resumability test suite in pkg/sync/resumability_test.go covering AC1-AC4.
- **Success criteria**: All tests pass with -race -count=5, coverage >= 90%, all ACs satisfied.
- **Interface contracts**: PROJECT.md, DISPATCH.md, explorer blueprints.
- **Code layout**: pkg/sync/engine.go, pkg/sync/resumability_test.go.

## Change Tracker
- **Files modified**: None yet
- **Build status**: pending
- **Pending issues**: None

## Quality Status
- **Build/test result**: pending
- **Lint status**: clean
- **Tests added/modified**: pending

## Loaded Skills
- None specified

## Key Decisions Made
- Initial setup

## Artifact Index
- /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2/progress.md — liveness heartbeat and progress tracking
- /Users/png/01. Project/coderpush/.agents/teamwork/worker_m2/handoff.md — final handoff report
