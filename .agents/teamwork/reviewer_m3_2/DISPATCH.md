# Reviewer M3.2 Dispatch: CLI, Streaming Formats & Reader Adapter

## Identity & Role
- Archetype: teamwork_preview_reviewer
- Role: Generator CLI & Streaming Reviewer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m3_2
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Worker Handoff: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m3/handoff.md
- Target Files: `cmd/generator/main.go`, `cmd/generator/main_test.go`, `pkg/generator/reader.go`

## Review Objectives
1. Examine code quality, correctness, and robustness of CLI and streaming interfaces:
   - CLI flags parsing (`--seed`, `--batches`, `--batch-size`, `--out`, `--format`, `--ratio-*`, `--job-id`, `--verbose`).
   - All supported output formats (`json-batches`, `ndjson-records`, `json`).
   - Verification that infinite mode (`--batches 0`) rejects `--format json` gracefully.
   - Signal handling (`signal.NotifyContext` with `SIGINT`/`SIGTERM`) and broken pipe resiliency (`syscall.EPIPE` exits code 0).
   - Buffered I/O (64KB buffer, flush per batch) for low streaming latency.
   - Reader adapter `ReadJSONBatches` with 10MB line buffer and IEEE 754 non-finite number serialization (`"NaN"`, `"+Infinity"`, `"-Infinity"`).
2. Execute builds and tests:
   - `go build ./cmd/generator`
   - `go test -v -race -count=5 ./cmd/generator/...`
   - Statement coverage check for `cmd/generator` ($\ge 90\%$).
3. Output verdict in `/Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m3_2/handoff.md`:
   - Either `APPROVE` or `REQUEST_CHANGES` with detailed evidence.


## 2026-10-21T12:49:07Z
You are teamwork_preview_reviewer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m3_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m3_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and worker handoff /Users/png/01. Project/coderpush/.agents/teamwork/worker_m3/handoff.md. Review cmd/generator CLI, streaming I/O formats, signal handling, and reader adapter. Run builds and tests. Provide explicit verdict (APPROVE or REQUEST_CHANGES) in /Users/png/01. Project/coderpush/.agents/teamwork/reviewer_m3_2/handoff.md. Send message when done.
