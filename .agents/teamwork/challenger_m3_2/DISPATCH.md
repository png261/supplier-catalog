# Challenger M3.2 Dispatch: Statistical Ratios & E2E Sync Engine Pipeline Challenge

## Identity & Role
- Archetype: teamwork_preview_challenger
- Role: Ratio Calibration & Pipeline Challenger
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m3_2
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Specifications
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Worker Handoff: /Users/png/01. Project/coderpush/.agents/teamwork/worker_m3/handoff.md
- Target Packages: `pkg/generator`, `cmd/generator`, `pkg/sync`, `pkg/storage`

## Challenge Objectives
1. Implement independent adversarial stress tests:
   - **Binomial $3\sigma$ Ratio Validation**: Over a generated dataset of $N = 25,000$ records with custom configured ratios (e.g. `UpdateRatio: 0.15`, `DuplicateRatio: 0.10`, `OutOfOrderRatio: 0.10`, `WithdrawnRatio: 0.05`, `InvalidRatio: 0.05`), count occurrences of each defect/mutation type. Calculate expected $\mu = N \cdot p$ and standard deviation $\sigma = \sqrt{N \cdot p \cdot (1 - p)}$. Assert that all observed counts lie strictly within $[\mu - 3\sigma, \mu + 3\sigma]$.
   - **Invariant D Defect Distribution**: Verify that invalid records inject ALL 6 defect types:
     1. Empty or whitespace-only SKU
     2. Negative price ($< 0$)
     3. IEEE 754 NaN price
     4. IEEE 754 Inf price
     5. Invalid product status enum
     6. Zero timestamp (`time.Time{}`)
   - **Full E2E Sync Engine Pipe & Accounting Conservation**: Pipe generator output (`pkg/generator.StreamBatches` or `ReadJSONBatches`) directly into `sync.SyncEngine.RunJob`. Verify:
     - All invalid records are quarantined in `dead_letter_issues` with proper reason.
     - Out-of-order records are skipped and counted in `Skipped`.
     - Duplicate records are idempotent ($0$ drift).
     - Accounting conservation holds exactly: $\text{Added} + \text{Updated} + \text{Withdrawn} + \text{Skipped} + \text{DeadLetter} == N_{total}$.
2. Execute tests under `go test -v -race -count=5`.
3. Output verdict in `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m3_2/handoff.md`:
   - Either `APPROVE` or `CONFIRM_CORRECTNESS_FAILED` with detailed empirical logs.

## 2026-10-21T12:49:07Z
You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m3_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m3_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially challenge binomial 3-sigma ratio calibration (updates, duplicates, out-of-order, withdrawn, corrupt across all 6 defect types) and E2E pipeline feeding into SyncEngine.RunJob. Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m3_2/handoff.md. Send message when done.
