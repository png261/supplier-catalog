# Milestone 1 Exploration: Test Strategy & Invariant Edge Cases

You are `teamwork_preview_spec_miner`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Scope for Milestone 1:
1. Define comprehensive unit test suites for `pkg/storage` and `pkg/sync`:
   - Unit tests for Invariant A: batch replay producing 0 adds, 0 updates, 0 deletes, exact catalog state preservation.
   - Unit tests for Invariant B: strict timestamp ordering ($T_{new} < T_{old}$ dropped, $T_{new} == T_{old}$ deduplicated, $T_{new} > T_{old}$ applied).
   - Unit tests for Invariant C: tombstone immutability (ACTIVE -> WITHDRAWN allowed; WITHDRAWN -> ACTIVE strictly prohibited even with future timestamps).
   - Intra-batch duplicate handling (multiple records for same SKU in single batch).
2. Concurrency test specifications for `go test -race -count=5 ./...`.
3. Code layout and test case table.

Deliverables:
- Detailed test cases and verification strategy in `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/handoff.md`.
- Keep heartbeat in `progress.md`.
- Send message upon completion.


## 2026-10-21T08:42:39Z
You are teamwork_preview_spec_miner. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design comprehensive test specifications and edge case matrices for Invariants A, B, and C for Milestone 1. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/handoff.md. Update progress.md. Send message when done.
