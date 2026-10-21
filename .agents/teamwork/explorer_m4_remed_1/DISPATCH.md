# Remediation Explorer M4.1 Dispatch: Concurrency TOCTOU Remediation

## Identity & Role
- Archetype: teamwork_preview_explorer
- Role: Concurrency Remediation Explorer
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Forensic Audit Evidence
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Forensic Auditor Report: /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md
- Challenger Report: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_1/handoff.md
- Target File: `pkg/api/handlers.go` (lines 88-230, 334-428)

## Problem Statement & Audit Evidence
The Forensic Auditor detected a critical TOCTOU (Time-of-Check to Time-of-Use) concurrency vulnerability:
1. `TestAdversarial_JobConcurrency_Race` failed: A burst of 20 concurrent `POST /api/jobs` requests resulted in 7 jobs accepted (202 Accepted) instead of exactly 1 accepted and 19 rejected with 409 Conflict (`JOB_CONFLICT`).
2. `TestAdversarial_InvalidResumptionScenarios/ConcurrentResumeRace` failed: A burst of concurrent resume requests on the same interrupted job accepted 7 resumes instead of exactly 1.
3. Root Cause: In `handleStartJob` and `handleResumeJob`, checking `h.storage.GetActiveJob(ctx)` and subsequent job creation/resumption are completely unsynchronized. In SQLite, concurrent goroutines both see `GetActiveJob == nil` before either creates the job in SQLite.

## Remediation Objectives
1. Formulate a robust fix strategy in `pkg/api/handlers.go`:
   - Mutex synchronization (`h.mu.Lock()` / `defer h.mu.Unlock()`) or atomic in-memory active job locking around job initiation and resumption.
   - Immediate synchronous creation/transition of job state to `RUNNING` before dispatching background work.
   - Comprehensive edge-case analysis: cancellation, errors, and background completion unlocking.
2. Provide concrete code diffs and blueprints in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handoff.md`.


## 2026-10-21T14:21:19Z
[Message] timestamp=2026-10-21T14:21:19Z sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8 priority=MESSAGE_PRIORITY_HIGH content=You are teamwork_preview_explorer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and the auditor report in /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md. Formulate fix strategy for Concurrency TOCTOU on POST /api/jobs and POST /api/jobs/{id}/resume in pkg/api/handlers.go. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m4_remed_1/handoff.md. Send message when done.
