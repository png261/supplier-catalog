# Dispatch to Explorer M5 CI (Native GitHub Actions CI/CD)

## Identity
- Role: CI/CD Pipeline Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci

## Context & Inputs
- Project root: /Users/png/01. Project/coderpush
- Authoritative requirements: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project scope & architecture: /Users/png/01. Project/coderpush/PROJECT.md

## Objectives
Analyze and design specifications and exact workflow definition for Milestone 5 Requirement R3:
1. `.github/workflows/ci-cd.yml` (and/or `ci.yml`):
   - Triggers: push to `main`, pull requests to `main`.
   - Native GitHub and Docker actions:
     - `actions/checkout@v4` (with `fetch-depth: 0` for Gitleaks scan)
     - `actions/setup-go@v5` (using go-version-file: 'go.mod' or matching Go version)
     - `gitleaks/gitleaks-action` or native gitleaks binary execution
     - `docker/setup-buildx-action@v3`
     - `docker/login-action@v3` (logging into `ghcr.io` with `${{ secrets.GITHUB_TOKEN }}`)
     - `docker/build-push-action@v6`
   - Jobs:
     - `security-and-lint`: Gitleaks full scan, `gofmt` check, `go vet ./...`
     - `test-and-coverage`: `go test -race -count=5 ./...`, project-wide statement coverage calculation with failure if < 90.0%
     - `build-and-package`: Docker build, image size assertion (<= 25MB), push to GHCR on `main` branch pushes.
2. Ensure GitHub Actions schema compatibility and permission configuration (`contents: read`, `packages: write`).

## Output Requirement
Write your complete analysis and recommended workflow file content to:
`/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci/handoff.md`
Send message to orchestrator upon completion.


## 2026-10-21T16:36:23Z
You are Explorer M5 CI.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci.
Read your task assignment in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci/DISPATCH.md.
Also read /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md and /Users/png/01. Project/coderpush/PROJECT.md.
Analyze and design the complete native GitHub Actions CI/CD workflow (.github/workflows/ci-cd.yml) using official actions, running Gitleaks, race tests, >= 90% coverage enforcement, and GHCR container publishing.
Write your findings and complete file blueprints to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m5_ci/handoff.md.
Notify the caller via send_message when complete.
