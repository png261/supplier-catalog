# Milestone 1 Adversarial Challenge: Idempotency & Out-of-Order Stress

You are `teamwork_preview_challenger`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Adversarial Objectives:
1. Empirically verify Invariant A (Deterministic Idempotency):
   - Generate complex batches, ingest them, snapshot catalog state, replay identical batches, verify 0 mutations and identical catalog checksums.
2. Empirically verify Invariant B (Fast-Forward Timestamps):
   - Generate out-of-order streams with randomly shuffled timestamps, sub-millisecond timestamps, distant past/future timestamps, ensuring no older timestamp ever overwrites newer state.
3. Write test harnesses/oracles (e.g. in temporary test files or test runner) and execute with `go test -v -race`.
4. Provide explicit verdict: `APPROVE` or `CONFIRM_CORRECTNESS_FAILED`.
5. Write full report to `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_1/handoff.md`.
6. Send message to caller when done.

## 2026-10-21T09:05:07Z
You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially test Invariant A (Idempotency) and Invariant B (Fast-forward timestamps). Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_1/handoff.md. Send message when done.
