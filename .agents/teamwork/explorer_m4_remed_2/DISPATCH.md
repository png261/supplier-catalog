# Remediation Explorer M4.2 Dispatch: Dead-Letter Queue Reason Filter Remediation

## Identity & Role
- Archetype: teamwork_preview_explorer
- Role: DLQ Reason Filter Remediation Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Forensic Audit Evidence
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Forensic Auditor Report: /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md
- Challenger Report: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1/handoff.md
- Target Files: `pkg/storage/sqlite.go`, `pkg/api/handlers.go`

## Problem Statement & Audit Evidence
The Forensic Auditor detected an unverified worker claim and test failure:
1. `TestAdversarial_DeadLetterQueue_PaginationAndFiltering/FilterByReason` failed: `GET /api/dead-letter?reason=negative price` returned Total=65 instead of the expected 25 records matching the reason filter.
2. Root Cause: `handleListDeadLetter` in `pkg/api/handlers.go:586-627` never extracts `q.Get("reason")` from the URL query. Furthermore, `storage.Storage.ListDeadLetterRecords` does not accept a reason filter or query SQLite with a `reason LIKE ?` clause.

## Remediation Objectives
1. Formulate a clean fix strategy:
   - Extend `storage.Storage` interface and `*SQLiteStorage` to support filtering by reason (e.g. `ListDeadLetterRecords(ctx context.Context, jobID string, reason string, limit, offset int)` or an options struct) with parameterization `AND reason LIKE '%' || ? || '%'`.
   - Update `handleListDeadLetter` in `pkg/api/handlers.go` to parse `reason := q.Get("reason")` and pass it to storage.
   - Update all existing calls and tests across the project to maintain 100% buildability and test compatibility.
2. Provide concrete code diffs and blueprints in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/handoff.md`.

## 2026-10-21T14:21:19Z
You are teamwork_preview_explorer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and the auditor report in /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md. Formulate fix strategy for Dead-Letter Queue reason filter in pkg/storage/sqlite.go and pkg/api/handlers.go. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_2/handoff.md. Send message when done.
