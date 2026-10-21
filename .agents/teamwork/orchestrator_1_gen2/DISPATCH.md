## 2026-10-21T16:34:11Z
You are the Project Orchestrator (Generation 2) for the resilient, minimalist catalog synchronization engine in Go located at `/Users/png/01. Project/coderpush`.

Your working directory is `/Users/png/01. Project/coderpush/.agents/teamwork/orchestrator_1_gen2`.
Your parent Sentinel is `c527d1b2-2752-4982-b618-cd5f5a971eda`.

Context:
Milestones 1–4 are fully implemented and verified with 93.4% statement coverage:
- `pkg/domain`, `pkg/storage` (SQLite WAL mode), `pkg/sync` (Invariants A, B, C, D, E, crash recovery).
- `pkg/generator`, `cmd/generator` (streaming mock data generator with seed determinism).
- `web/static/` (zero-node SPA dashboard via `//go:embed`), `pkg/api` (REST control plane, thread-safe SSE broker), `cmd/server`.

Consult:
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- `/Users/png/01. Project/coderpush/.agents/teamwork/orchestrator_1/handoff.md`
- `/Users/png/01. Project/coderpush/PROJECT.md`
- `/Users/png/01. Project/coderpush/TEST_INFRA.md`

Your Objectives:
Execute and verify Milestones 5 and 6:
### R1. Security Gates & Git Hooks (Milestone 5)
- Configure Husky and Gitleaks (`.husky/pre-commit`, `.husky/pre-push`, `.gitleaks.toml`) to scan staged files and full history for secret leaks.
- Enforce strict `gofmt`, `go vet`, and `golangci-lint` on pre-commit.
- Enforce `go test -race -count=5 ./...` and project-wide statement coverage threshold (>= 90%) on pre-push.

### R2. Docker Packaging & Local Composition (Milestone 5)
- Provide a lightweight multi-stage `Dockerfile` (<= 25MB Alpine) producing static Go binaries (`CGO_ENABLED=0`).
- Provide `docker-compose.yml` with a persistent named volume mount (`./data:/app/data`) for SQLite resilience across restarts.

### R3. Native GitHub Actions CI/CD (Milestone 5)
- Implement `.github/workflows/ci-cd.yml` using official native GitHub & Docker actions (`actions/setup-go`, `docker/setup-buildx-action`, `docker/login-action`, `docker/build-push-action`).
- Automatically execute Gitleaks secret scans, multi-iteration race tests, coverage verification (>= 90%), and publish the container package to GitHub Container Registry (GHCR: `ghcr.io`).

### R4. Project Tooling & Comprehensive Documentation (Milestone 6)
- Provide a `Makefile` with targets for running the server, generator, tests, coverage HTML reports, linter, and Docker commands.
- Provide a production-grade `README.md` including 1-click execution commands (Go, Docker, GHCR package), architecture & data invariants, assumptions, unfinished work, and transparent AI tool disclosure.
- Generate sample fixture batches in `mock_data/` for immediate out-of-the-box demonstration.

Acceptance Criteria:
- Pre-commit hook prevents commits containing leaked credentials, unformatted Go code, or linter errors.
- Pre-push hook enforces race-free test execution and fails if total statement coverage drops below 90.0%.
- Multi-stage Docker image builds successfully and image size is <= 25MB.
- `docker compose up` starts the server and persists database state in the mounted volume across container restarts.
- GitHub Actions workflow syntax validates cleanly against GitHub schema.
- `make test`, `make coverage`, and `make lint` execute cleanly with 0 errors.
- `README.md` covers all Assessment 1 requirements (Instructions, Assumptions, Unfinished Work, AI disclosure).
