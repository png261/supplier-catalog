# Spec Miner Dispatch: Milestone 3 Test Specifications & Invariant Verification

## Mission
Mine behavioral specifications and design comprehensive test matrices for the Mock Data Generator (`pkg/generator` and `cmd/generator`):
1. **Seed Determinism Invariant**:
   - Given the same seed and configuration, two generator instances MUST produce byte-for-byte identical batch sequences across 100+ batches.
   - Given different seeds, sequences must diverge immediately.
2. **Infinite Stream & Bounded Memory Invariant ($O(1)$ Memory)**:
   - Define benchmark/test asserting heap memory does not grow proportionally with generated batch count when generating 100,000+ items.
3. **Statistical Ratio Calibration Verification**:
   - Formulate statistical tolerance checks (e.g. within binomial 3-sigma confidence intervals) for each configured ratio:
     - Updates to existing SKUs
     - Duplicates
     - Out-of-order timestamps
     - Withdrawn status
     - Invariant D corrupt records (negative prices, empty SKUs, zero timestamps, NaN/Inf, invalid statuses)
4. **End-to-End Pipeline Feeding Test**:
   - Streaming generator output directly into `SyncEngine.RunJob` and verifying change summary counters match expected metrics within statistical boundaries.

Write your report to `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m3_1/handoff.md`.

## Context Files
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md` (read first)
- `/Users/png/01. Project/coderpush/PROJECT.md`
- `/Users/png/01. Project/coderpush/pkg/domain/models.go`

Send message when done.


## 2026-10-21T12:16:39Z
You are teamwork_preview_spec_miner. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m3_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m3_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design comprehensive test specifications, determinism checks, and O(1) memory benchmarks. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m3_1/handoff.md. Send message when done.
