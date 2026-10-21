# Task Assignment: Ecosystem, UI & Packaging Architecture Exploration

You are `teamwork_preview_explorer`.
Your working directory is `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.

Investigate and evaluate technical architecture for:
1. Programmatic Mock Data Generator (Infinite Scale):
   - Standalone CLI/package in Go for streaming supplier product batches.
   - Deterministic seedable PRNG (e.g. `math/rand/v2` or `math/rand`).
   - Configurable batch counts, batch sizes, edge-case injection rates:
     - Duplicate SKUs / duplicate batches.
     - Out-of-order timestamps (T_new < T_old).
     - Withdrawn / tombstone records.
     - Invalid / corrupt records (negative prices, missing SKUs, malformed JSON).
2. Minimalist Staff Interface & Status Reporting:
   - Single-page dashboard embedded directly into Go binary (`//go:embed`).
   - Clean, lightweight frontend (HTML5/CSS/minimal JS, no massive node_modules runtime).
   - Real-time or polling sync progress, change summary stats (+Added, ~Updated, -Withdrawn, ↷Skipped), issues/dead-letter table inspection, and button/API to trigger or resume interrupted sync jobs.
3. Security, Linting & Git Hooks:
   - Husky + Gitleaks configuration in the repo.
   - Pre-commit and pre-push hooks for secret leak detection, code formatting (`gofmt`), strict linting (`golangci-lint` or `go vet`).
4. Docker Packaging & Compose:
   - Multi-stage Dockerfile compiling Go binary with `CGO_ENABLED=0` targeting alpine or scratch/distroless to ensure final image size <= 25MB.
   - `docker-compose.yml` with persistent volume mounting for the database file.
5. CI/CD:
   - GitHub Actions workflow (`.github/workflows/ci.yml`) compiling Go application, running tests with race detector and coverage verification (>=90%), and publishing image to GHCR.
6. Write your comprehensive report to `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2/handoff.md`.
7. Include a progress heartbeat in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2/progress.md`.
8. Once finished, use `send_message` to notify the Project Orchestrator.


## 2026-10-21T08:31:20Z

[Message] timestamp=2026-10-21T08:31:20Z sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8 priority=MESSAGE_PRIORITY_HIGH content=You are teamwork_preview_explorer. Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2. Read the instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2/DISPATCH.md and the authoritative request in /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Investigate the mock generator, embedded staff UI dashboard, Husky/Gitleaks security, Docker packaging (<=25MB), and GitHub Actions CI/CD. Write your report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_survey_2/handoff.md. Update progress.md regularly. When complete, send a message to the caller.
