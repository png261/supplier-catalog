# BRIEFING — 2026-10-21T08:48:00Z

## Mission
Discover, design, and document comprehensive test specifications and edge case matrices for Milestone 1: Invariants A, B, and C across pkg/storage and pkg/sync.

## 🔒 My Identity
- Archetype: teamwork_preview_spec_miner
- Roles: Specification Miner
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: M1 (Core Domain, Storage & Synchronization Engine)

## 🔒 Key Constraints
- Read-only on codebase: do NOT implement source code, only design specifications and tests.
- Design comprehensive unit test suites for pkg/storage and pkg/sync.
- Invariant A: Deterministic idempotency (batch replay -> 0 adds, 0 updates, 0 deletes, exact catalog preservation, Skipped count increments).
- Invariant B: Strict timestamp ordering ($T_{new} < T_{old}$ dropped, $T_{new} == T_{old}$ deduplicated, $T_{new} > T_{old}$ applied).
- Invariant C: Tombstone immutability (ACTIVE -> WITHDRAWN allowed; WITHDRAWN -> ACTIVE strictly prohibited even with future timestamps).
- Handle intra-batch duplicates (multiple records for same SKU in single batch).
- Concurrency test specifications for `go test -race -count=5 ./...`.
- Code layout and test case table adhering to PROJECT.md and TEST_INFRA.md.

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: not yet

## Task Summary
- **What to build**: Test specifications, invariant verification rules, edge-case test matrices, and concurrency test suites for Milestone 1.
- **Success criteria**: Exhaustive enumeration of features, inputs, outputs, error conditions, edge-case behaviors, and race-safety verification methods for M1.
- **Interface contracts**: /Users/png/01. Project/coderpush/PROJECT.md § Interface Contracts
- **Code layout**: /Users/png/01. Project/coderpush/PROJECT.md § Code Layout

## Key Decisions Made
- Partition of unit test responsibilities between `pkg/storage` (schema, transactions, ACID, connection management) and `pkg/sync` (invariant logic, change counters, intra-batch deduplication, fast-forwarding, tombstone monotonicity).
- Detail explicit intra-batch duplicate resolution semantics: later record in batch wins if $T_{later} > T_{earlier}$; identical drops; tombstone in earlier record locks out later reactivation.
- Concurrency test harness specification using `go test -race -count=5` with 20 concurrent readers and 5 writers to verify zero race conditions.
- Documented 26 discrete features and 31 edge case scenarios in `handoff.md`.

## Artifact Index
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/DISPATCH.md — Task assignment and instructions
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/BRIEFING.md — Working memory and identity
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/progress.md — Liveness heartbeat and milestone tracking
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m1_1/handoff.md — Final specification mining report

## Loaded Skills
- None provided in dispatch prompt
