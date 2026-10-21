# Challenger M4.1 Dispatch: REST Control Plane & DLQ Adversarial Stress Test

## Identity & Role
- Archetype: teamwork_preview_challenger
- Role: REST Control Plane Challenger
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Worker Handoff: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m4/handoff.md
- Target Packages: `pkg/api`, `pkg/storage`, `pkg/sync`

## Challenge Objectives
1. Implement independent adversarial tests:
   - **Concurrency Control & 409 Conflict**: Attempt to trigger multiple concurrent sync jobs via `POST /api/jobs`. Assert that exactly one job transitions to `RUNNING` and subsequent requests return `409 Conflict` (`JOB_ALREADY_RUNNING`).
   - **Invalid Resumption Scenarios**: Attempt to resume non-existent jobs (assert 404), completed jobs (assert 400), and running jobs (assert 409).
   - **Dead-Letter Queue Pagination & Filtering**: Generate a batch with 50+ mixed corrupt records. Test pagination boundaries (`limit`, `offset`), filtering by `job_id` and error `reason`. Assert that raw payload and specific error reason match Invariant D.
   - **Catalog Browser Filtering**: Test `GET /api/products` with status filters (`ACTIVE` vs `WITHDRAWN`), search query text matching, and pagination limits.
2. Execute tests under `go test -v -race -count=5`.
3. Output verdict in `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1/handoff.md`:
   - Either `APPROVE` or `CONFIRM_CORRECTNESS_FAILED` with detailed empirical logs.


## 2026-10-21T14:03:34Z
You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially challenge REST endpoints, 409 Conflict concurrency protection on running jobs, invalid resumption, and dead-letter queue pagination/filtering. Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1/handoff.md. Send message when done.
