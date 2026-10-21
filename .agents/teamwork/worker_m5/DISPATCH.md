# Dispatch: Worker M5 (Security Gates, Docker Packaging & CI/CD)

## MANDATORY INTEGRITY WARNING
> DO NOT CHEAT. All implementations must be genuine. DO NOT hardcode test results, create dummy/facade implementations, or circumvent the intended task. A teamwork_preview_auditor will independently verify your work. Integrity violations WILL be detected and your work WILL be rejected.

## Identity & Working Directory
- Role: Implementation Worker (Milestone 5)
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m5

## Inputs & Authoritative References
- Project root: `/Users/png/01. Project/coderpush`
- Authoritative requirements: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`
- Explorer handoffs:
  - Git Hooks & Security: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1/handoff.md`
  - Docker Packaging: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_docker/handoff.md`
  - GitHub Actions CI/CD: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci/handoff.md`

## File Ownership
You own exclusive write permissions to:
- `package.json`
- `.gitleaks.toml`
- `.husky/pre-commit`
- `.husky/pre-push`
- `scripts/check-coverage.sh`
- `Dockerfile`
- `docker-compose.yml`
- `.dockerignore`
- `.github/workflows/ci-cd.yml` (and `.github/workflows/ci.yml`)
- `cmd/server/main.go` (only for adding `DB_PATH` environment variable support if `$DB_PATH` is set)

## Concrete Implementation Objectives

### 1. Requirement R1: Security Gates & Git Hooks
- Create `package.json` with Husky v9.1.7 (`"prepare": "husky"`). Run `npm install` and `npx husky` if npm is available, or configure git hooks path (`git config core.hooksPath .husky`).
- Create `.gitleaks.toml` extending default rules with allowlists for test fixtures, mock data, and metadata.
- Create executable `.husky/pre-commit`:
  - Run `gitleaks protect --staged`
  - Run `gofmt -l` on staged Go files (filtering out deleted files with `--diff-filter=d`)
  - Run `go vet ./...`
  - Gracefully handle `golangci-lint` if present.
- Create executable `scripts/check-coverage.sh`:
  - Run `go test -coverprofile=coverage.out ./...`
  - Parse total statement coverage using `go tool cover -func=coverage.out` and awk
  - Enforce threshold $\ge 90.0\%$; exit with 1 if below threshold.
- Create executable `.husky/pre-push`:
  - Run `gitleaks detect`
  - Run `go test -race -count=5 ./...`
  - Run `scripts/check-coverage.sh`

### 2. Requirement R2: Docker Packaging & Composition
- Update `cmd/server/main.go` to support `DB_PATH` env var (e.g. `if envDB := os.Getenv("DB_PATH"); envDB != "" { defaultDB = envDB }`).
- Create multi-stage `Dockerfile`:
  - Builder: `golang:1.24-alpine` or `golang:1.25-alpine`, build static binary with `CGO_ENABLED=0`, `-ldflags="-s -w"`, `-trimpath`.
  - Runtime: minimal `alpine:3.21` with `ca-certificates` and `tzdata`.
  - Non-root user `10001:10001`, `/app/data` directory owned by `10001:10001`.
  - Expose port `8080`.
  - Uncompressed image size MUST be $\le 25\text{MB}$.
- Create `.dockerignore` ignoring `.git`, `.agents`, `node_modules`, test binaries, etc.
- Create `docker-compose.yml`:
  - Service `catalog-engine` building from `.`
  - Persistent volume mount `./data:/app/data`
  - Port mapping `8080:8080`
  - Environment variable `DB_PATH=/app/data/catalog.db`

### 3. Requirement R3: Native GitHub Actions CI/CD
- Create `.github/workflows/ci-cd.yml` using official actions (`actions/checkout@v4`, `actions/setup-go@v5`, `gitleaks/gitleaks-action@v2`, `docker/setup-buildx-action@v3`, `docker/metadata-action@v5`, `docker/login-action@v3`, `docker/build-push-action@v6`).
- Include jobs: `security-and-lint`, `test-and-coverage`, `build-and-package`.
- Enforce race tests (`go test -race -count=5 ./...`), coverage gate $\ge 90\%$, Docker build size assertion $\le 25\text{MB}$, and GHCR publishing on `main` push.

## Verification Requirements
You MUST run and document the following verification tests in your report:
1. `CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /tmp/server ./cmd/server` (verify clean static build).
2. `go vet ./...` and `gofmt -l .` (verify 0 formatting or vet issues).
3. `bash scripts/check-coverage.sh` (verify coverage extraction and $\ge 90.0\%$ pass).
4. `go test -race -count=5 ./...` (verify 0 data races).
5. Pre-commit and pre-push hook simulation (verify scripts run and succeed on clean repo).
6. Docker build verification (build local image or verify layer sizing $\le 25\text{MB}$).
7. YAML schema validation on `.github/workflows/ci-cd.yml`.

## Handoff
Write your full implementation and verification report to:
`/Users/png/01. Project/coderpush/.agents/teamwork/worker_m5/handoff.md`
Notify the orchestrator via `send_message` when done.


## 2026-10-21T16:46:25Z
You are Worker M5.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/worker_m5.
Read your task assignment and mandatory integrity warning in /Users/png/01. Project/coderpush/.agents/teamwork/worker_m5/DISPATCH.md.
Also consult:
- /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- /Users/png/01. Project/coderpush/PROJECT.md
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_docker/handoff.md
- /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci/handoff.md

Implement all components for Milestone 5 (Husky, Gitleaks, hooks, coverage script, Dockerfile, docker-compose.yml, .dockerignore, .github/workflows/ci-cd.yml, DB_PATH env support in cmd/server/main.go).
Run all build, format, test, coverage, and validation commands to verify your implementation.
Write your complete report to /Users/png/01. Project/coderpush/.agents/teamwork/worker_m5/handoff.md.
Notify the caller via send_message when complete.
