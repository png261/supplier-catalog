# DISPATCH — Challenger 1 (Milestone 4 Iteration 2)

## Role & Focus
You are `teamwork_preview_challenger`.
Focus: Empirical stress testing of REST control plane concurrency (TOCTOU job creation/resumption) and DLQ reason filtering.

## Inputs to Read
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- `/Users/png/01. Project/coderpush/PROJECT.md`
- Previous auditor report: `/Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md`
- Worker handoff: `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m4_remed/handoff.md`

## Adversarial Challenges to Execute
Empirically challenge:
1. `POST /api/jobs`: Burst of 20 concurrent requests against idle server -> verify exactly 1 accepted (202) and 19 conflicts (409 `JOB_CONFLICT`).
2. `POST /api/jobs/{id}/resume`: Burst of 10 concurrent resume requests against interrupted job -> verify exactly 1 accepted (202) and 9 conflicts (409 `JOB_CONFLICT`).
3. `GET /api/dead-letter`: Reason filtering with case variations, substring matching, empty string, and nonexistent reasons -> verify exact match count and 0 SQL injection vulnerabilities.
4. Run under `go test -v -race -count=5` to verify 0 data races.

Provide an explicit verdict (**APPROVE** or **CONFIRM_CORRECTNESS_FAILED**) in:
`/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_1/handoff.md`.
Send a message when done.


## 2026-10-21T14:57:26Z
[Message] timestamp=2026-10-21T14:57:26Z sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8 priority=MESSAGE_PRIORITY_HIGH content=You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially challenge REST endpoints: 20 concurrent requests to POST /api/jobs -> exactly 1 accepted, 19 conflicts; 10 concurrent requests to POST /api/jobs/{id}/resume -> exactly 1 accepted, 9 conflicts; and DLQ reason filtering accuracy. Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_1/handoff.md. Send message when done.
