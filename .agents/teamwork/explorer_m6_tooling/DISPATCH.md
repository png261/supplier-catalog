# Dispatch: Explorer M6 Tooling (Makefile & Developer Automation)

## Identity
- Role: Tooling & Automation Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling

## Inputs & Authoritative References
- Project root: `/Users/png/01. Project/coderpush`
- Authoritative requirements: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`

## Objectives
Analyze and design complete specifications and file blueprints for the `Makefile` (Requirement R4):
1. Target requirements:
   - `make run-server`: build and run the HTTP server (`cmd/server`) with defaults.
   - `make run-generator`: run the CLI generator (`cmd/generator`) with configurable flags.
   - `make test`: execute unit & integration tests (`go test -race ./...`).
   - `make test-race`: execute 5-iteration race tests (`go test -race -count=5 ./...`).
   - `make coverage`: run coverage check (`scripts/check-coverage.sh`) ensuring $\ge 90.0\%$, and generate `coverage.html` via `go tool cover -html=coverage.out -o coverage.html`.
   - `make lint`: run `gofmt -l`, `go vet ./...`, and `golangci-lint` (if installed, with graceful skip message).
   - `make docker-build`: build the lightweight multi-stage Docker image (`Dockerfile`).
   - `make docker-up`: start the compose environment (`docker compose up -d`).
   - `make docker-down`: stop compose environment (`docker compose down`).
   - `make clean`: remove temporary build artifacts, database files, coverage profiles, and logs.
   - `make help`: self-documenting help target.
2. Verify that `make test`, `make coverage`, and `make lint` execute cleanly with 0 errors.

## Output
Write your findings and complete `Makefile` blueprint to:
`/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling/handoff.md`
Notify the orchestrator via `send_message` when done.


## 2026-10-21T17:13:31Z
You are Explorer M6 Tooling.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling.
Read your task assignment in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling/DISPATCH.md.
Also read /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md and /Users/png/01. Project/coderpush/PROJECT.md.
Design the complete production Makefile blueprint (make run-server, run-generator, test, test-race, coverage, lint, docker-build, docker-up, docker-down, clean, help).
Ensure make test, make coverage, and make lint execute cleanly with 0 errors.
Write your findings and blueprint to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_tooling/handoff.md.
Notify the caller via send_message when complete.
