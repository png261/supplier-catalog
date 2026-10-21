# Dispatch to Spec Miner M5 (Security Gates & Git Hooks)

## Identity
- Role: Specification Miner / Security Gate Analyst
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1

## Context & Inputs
- Project root: /Users/png/01. Project/coderpush
- Authoritative requirements: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project scope & architecture: /Users/png/01. Project/coderpush/PROJECT.md

## Objectives
Analyze and design complete specifications and file templates for Milestone 5 Requirement R1:
1. Husky v9 configuration (`package.json`, `.husky/pre-commit`, `.husky/pre-push`).
2. Gitleaks configuration (`.gitleaks.toml`) allowing secret scans on staged changes (pre-commit) and full repository history (pre-push and CI).
3. Pre-commit hooks enforcing:
   - Gitleaks scan of staged files (`gitleaks protect --staged` or fallback)
   - Strict `gofmt` (preventing unformatted Go files from being committed)
   - `go vet ./...`
   - `golangci-lint` (if installed, with graceful handling if not installed locally)
4. Pre-push hooks enforcing:
   - `go test -race -count=5 ./...`
   - Project-wide statement coverage threshold calculation ensuring total coverage >= 90.0%, failing the push if below 90.0%.

## Output Requirement
Write your complete analysis and recommended configuration/scripts to:
`/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1/handoff.md`
Send message to orchestrator upon completion.

## 2026-10-21T16:36:23Z
You are Spec Miner M5.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1.
Read your task assignment in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1/DISPATCH.md.
Also read /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md and /Users/png/01. Project/coderpush/PROJECT.md.
Design the complete Git Hooks & Security Gate specifications (Husky v9, Gitleaks, pre-commit, pre-push, coverage enforcement script).
Write your findings and complete file blueprints to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m5_1/handoff.md.
Notify the caller via send_message when complete.
