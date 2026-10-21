# Dispatch: Challenger M6.2 (Demo Fixtures & Invariants Adversary)

## Identity
- Role: Fixtures & Invariant Verification Adversary
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m6_2

## Inputs & Authoritative References
- Project root: `/Users/png/01. Project/coderpush`
- Authoritative requirements: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`
- Sample fixtures: `mock_data/`
- Ingestion script: `scripts/ingest-fixtures.sh`

## Adversarial Challenge Plan
Adversarially challenge the sample fixture batches and data invariants:
1. **JSON Validation**:
   - Parse each fixture file (`batch_001_initial.json`, `batch_002_updates_and_idempotency.json`, `batch_003_anomalies_and_deadletter.json`, `batch_004_tombstone_reactivation_attempt.json`, `all_batches.ndjson`) to verify valid JSON formatting.
2. **Invariant Verification via Data Audit**:
   - Verify Batch 1 contains 10 ACTIVE records.
   - Verify Batch 2 contains updates, replays (Invariant A), and 2 WITHDRAWN records (Invariant C).
   - Verify Batch 3 contains clock skew records with $T_{new} < T_{old}$ (Invariant B), negative prices and empty SKUs (Invariant D), and valid records.
   - Verify Batch 4 attempts to reactivate WITHDRAWN items, proving Invariant C preservation.
3. **Execution Verification**:
   - Run a temporary test instance of the server or execute `pkg/sync` tests against these fixtures to verify that all 5 invariants hold exactly as claimed.

## Verdict Requirement
Write your challenge report to `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m6_2/handoff.md`.
End your report with a clear verdict line:
`Verdict: APPROVE` or `Verdict: REQUEST_CHANGES`
Notify the orchestrator via `send_message` when done.


## 2026-10-21T17:34:42Z
[Message] timestamp=2026-10-21T17:34:42Z sender=f4c20978-44e2-48fc-8b99-f95316237925 priority=MESSAGE_PRIORITY_HIGH
You are Challenger M6.2.
Your working directory is /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m6_2.
Read your task assignment in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m6_2/DISPATCH.md.
Also read /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/worker_m6/handoff.md.
Adversarially challenge the mock_data/ sample fixture batches and scripts/ingest-fixtures.sh. Validate JSON syntax, verify exact invariant assertions (Invariants A, B, C, D, E).
Write your challenge report to /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m6_2/handoff.md.
End your report with "Verdict: APPROVE" or "Verdict: REQUEST_CHANGES".
Notify the caller via send_message when complete.
