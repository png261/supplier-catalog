# Milestone 1 Adversarial Challenge: Tombstone Immutability & Race Stress

You are `teamwork_preview_challenger`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Adversarial Objectives:
1. Empirically verify Invariant C (Tombstone Preservation):
   - Attempt repeated aggressive resurrections of `WITHDRAWN` products with timestamps years in the future, interleaved with status updates and price changes.
   - Verify that `WITHDRAWN` is strictly absorbing and never flips back to `ACTIVE`.
2. Empirically verify Concurrency & Thread-Safety:
   - Run high-concurrency stress test with multiple goroutines invoking `ExecuteBatch` and `GetProduct` simultaneously under `go test -race -count=5`.
3. Provide explicit verdict: `APPROVE` or `CONFIRM_CORRECTNESS_FAILED`.
4. Write full report to `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_2/handoff.md`.
5. Send message to caller when done.

## 2026-10-21T09:05:07Z
You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially test Invariant C (Tombstone immutability) and Concurrency under race detector. Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m1_2/handoff.md. Send message when done.
