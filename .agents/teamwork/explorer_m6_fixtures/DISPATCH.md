# Dispatch: Explorer M6 Fixtures (Sample Data & Out-of-the-Box Demo)

## Identity
- Role: Fixtures & Demo Data Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures

## Inputs & Authoritative References
- Project root: `/Users/png/01. Project/coderpush`
- Authoritative requirements: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`
- Domain models: `pkg/domain/models.go`

## Objectives
Analyze and design comprehensive sample fixture batches in `mock_data/` for immediate out-of-the-box demonstration (Requirement R4):
1. Format: clean JSON or NDJSON files matching `domain.Batch` / `domain.SupplierUpdate` structure.
2. Narrative Progression demonstrating all 5 Core Invariants:
   - `batch_001_initial.json`: Baseline catalog insertion (10 items, Status: `ACTIVE`, valid timestamps, diverse prices). Demonstrates standard ingestion.
   - `batch_002_updates_and_idempotency.json`: Contains 5 updated items ($T_{new} > T_{old}$, price changes), 3 identical items replayed to demonstrate Invariant A (Zero Drift), and 2 withdrawn items (`WITHDRAWN`) to demonstrate Invariant C (Tombstone).
   - `batch_003_anomalies_and_deadletter.json`: Contains 2 out-of-order records ($T_{new} < T_{old}$) demonstrating Invariant B (Skip Counter), 2 negative price records ($< 0$) demonstrating Invariant D (Dead-Letter Isolation), 1 empty/whitespace SKU demonstrating Invariant D quarantine, and 2 valid records to demonstrate that valid items still succeed within the batch.
   - `batch_004_tombstone_reactivation_attempt.json`: Attempt to reactivate a `WITHDRAWN` item to `ACTIVE`, proving Invariant C (tombstone preservation cannot be overwritten).
3. Provide instructions or utility scripts for ingesting these fixture batches via CLI or API curl commands.

## Output
Write your findings and complete fixture file specifications to:
`/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures/handoff.md`
Notify the orchestrator via `send_message` when done.


## 2026-10-21T17:13:31Z
You are Explorer M6 Fixtures.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures.
Read your task assignment in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures/DISPATCH.md.
Also read /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, /Users/png/01. Project/coderpush/PROJECT.md, and pkg/domain/models.go.
Design comprehensive sample fixture batches in mock_data/ demonstrating all 5 Core Invariants (baseline ingestion, updates, idempotency replay, tombstones, out-of-order timestamps, and dead-letter quarantine).
Write your findings and fixture blueprints to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m6_fixtures/handoff.md.
Notify the caller via send_message when complete.
