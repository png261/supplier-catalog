# Dispatch: Worker M6 (Project Tooling, Fixtures & Documentation)

## MANDATORY INTEGRITY WARNING
> DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Identity & Working Directory
- Role: Implementation Worker (Milestone 6)
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m6

## Inputs & Authoritative References
- Project root: `/Users/png/01. Project/coderpush`
- Authoritative requirements: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`
- Explorer handoffs:
  - Makefile Tooling: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling/handoff.md`
  - Demo Fixtures: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures/handoff.md`
  - README Documentation: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_docs/handoff.md`

## File Ownership
You own exclusive write permissions to:
- `Makefile`
- `README.md`
- `mock_data/` (all batch json files and ndjson)
- `scripts/ingest-fixtures.sh` (executable `chmod +x`)
- `PROJECT.md` (at project root, to update milestone status tables and metrics)

## Concrete Implementation Objectives

### 1. Makefile (Requirement R4)
Create `/Users/png/01. Project/coderpush/Makefile` using the blueprint from `explorer_m6_tooling/handoff.md`:
- Standard targets: `run-server`, `run-generator`, `test`, `test-race`, `coverage` (HTML + gate), `lint`, `docker-build`, `docker-up`, `docker-down`, `clean`, `help`.
- Ensure TAB indentation is preserved in the Makefile.
- Ensure `make test`, `make coverage`, and `make lint` execute cleanly with 0 errors.

### 2. Sample Demo Fixtures (Requirement R4)
Create sample fixture batches in `/Users/png/01. Project/coderpush/mock_data/`:
- `mock_data/batch_001_initial.json` (baseline catalog items)
- `mock_data/batch_002_updates_and_idempotency.json` (updates, Invariant A replay, Invariant C tombstones)
- `mock_data/batch_003_anomalies_and_deadletter.json` (Invariant B clock-skew, Invariant D negative prices/empty SKU)
- `mock_data/batch_004_tombstone_reactivation_attempt.json` (attempt to reactivate tombstone)
- `mock_data/all_batches.ndjson`
- Create `scripts/ingest-fixtures.sh` (`chmod +x scripts/ingest-fixtures.sh`) to automate batch ingestion against the HTTP server.

### 3. Production-Grade README.md (Requirement R4)
Create `/Users/png/01. Project/coderpush/README.md` using the blueprint from `explorer_m6_docs/handoff.md`:
- 1-Click execution commands (Go, Docker Compose with `./data:/app/data` volume persistence, GHCR package, generator CLI)
- Architecture & Mathematical Invariant Formalizations (Invariants A, B, C, D, E)
- Out-of-the-Box Demo walkthrough with curl commands
- Developer Tooling & Verification Guide (`make test`, `make coverage`, `make lint`, Husky, CI/CD)
- Assessment 1 mandatory sections:
  - Assumptions (architectural trade-offs)
  - Unfinished Work / Production Roadmap
  - Transparent AI Tool Disclosure

### 4. Project State Update
Update `/Users/png/01. Project/coderpush/PROJECT.md` to mark Milestones 1–5 as `DONE` and Milestone 6 as `IN_PROGRESS` or `DONE`.

## Verification Requirements
Run and document the following verification tests:
1. `make help`
2. `make lint` (must pass with 0 errors)
3. `make test` (must pass with 0 errors)
4. `make coverage` (must pass with 0 errors, generate `coverage.html`, statement coverage >= 90%)
5. Validate fixture files (JSON syntax check via `jq .` or `python3 -m json.tool`)
6. Test running server and ingesting fixtures or test runner to ensure fixtures function cleanly.

## Handoff
Write your full report to:
`/Users/png/01. Project/coderpush/.agents/teamwork/worker_m6/handoff.md`
Notify the orchestrator via `send_message` when done.


## 2026-10-21T17:24:11Z
[Message] sender=f4c20978-44e2-48fc-8b99-f95316237925
You are Worker M6.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m6.
Read your task assignment and mandatory integrity warning in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m6/DISPATCH.md.
Also consult:
- /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- /Users/png/01. Project/coderpush/PROJECT.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_docs/handoff.md

Implement Makefile, mock_data/ sample fixture batches, scripts/ingest-fixtures.sh, production-grade README.md, and update PROJECT.md.
Run verification tests (make lint, make test, make coverage, fixture validation, README checks).
Write your complete report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m6/handoff.md.
Notify the caller via send_message when complete.
