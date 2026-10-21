# Milestone 2 Remediation: Zero-Timestamp Test Specifications

You are `teamwork_preview_spec_miner`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_remed_1`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.
Challenger failure report: `/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m2_2/handoff.md`.

Failure Context:
Challenger M2.2 reported `CONFIRM_CORRECTNESS_FAILED`:
In `pkg/sync/engine.go` (line 558), `isInvalidRecord` omitted checking `r.Timestamp.IsZero()`.

Scope:
1. Define formal test cases for zero-timestamp records:
   - Zero-timestamp record quarantined to `dead_letter_issues` with reason `"zero or uninitialized timestamp"`.
   - Batch containing zero-timestamp records + valid records commits valid records and dead-letters invalid.
   - Batch containing 100% zero-timestamp records commits 0 products and $N$ dead-letters.
   - Replay of zero-timestamp batch is idempotent (dead letters recorded, 0 catalog drift).
2. Document test specifications in `handoff.md`. Send message when done.

## 2026-10-21T11:46:57Z
You are teamwork_preview_spec_miner. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_remed_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_remed_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and challenger failure report /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m2_2/handoff.md. Define comprehensive test specifications for zero-timestamp records. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m2_remed_1/handoff.md. Send message when done.
