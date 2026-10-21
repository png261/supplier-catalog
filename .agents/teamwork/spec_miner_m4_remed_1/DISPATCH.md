# Remediation Spec Miner M4 Dispatch: SSE Client ID Generation & Broker Concurrency

## Identity & Role
- Archetype: teamwork_preview_spec_miner
- Role: Broker Concurrency Remediation Spec Miner
- Working Directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1
- Parent Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8

## Inputs & Forensic Audit Evidence
- Original Request: /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- Project Specification: /Users/png/01. Project/coderpush/PROJECT.md
- Forensic Auditor Report: /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md
- Challenger Task Log: `369c6e5f-9af9-436a-835b-4e79175139d8/task-96.log`
- Target File: `pkg/api/broker.go` (lines 193-210)

## Problem Statement & Audit Evidence
The Forensic Auditor and SSE Challenger identified a client ID collision defect:
1. In `pkg/api/broker.go:194`:
   `id: fmt.Sprintf("client-%d", time.Now().UnixNano())`
2. Under concurrent subscriber connections (e.g. 20 concurrent clients connecting simultaneously), `time.Now().UnixNano()` returns identical timestamps across goroutines.
3. In `Broker.Start`, `b.clients[client.id] = client` overwrites existing clients in the map with the same key, causing:
   - Broker client count mismatch (`got 18, expected 20`).
   - Sinking / dropped events for overwritten clients (e.g. Client 18 receiving 0 events).
   - Incomplete cleanup on disconnect resulting in hanging test servers.

## Remediation Objectives
1. Define formal specification for atomic client registration and connection lifecycle:
   - Introduce an atomic monotonically increasing counter (e.g. `atomic.AddUint64(&b.nextClientID, 1)`) or cryptographically secure UUID generator so client IDs are guaranteed globally unique across all goroutines.
   - Specify clean channel close and unregistration semantics ensuring zero goroutine or connection leaks.
2. Provide concrete blueprints and test specifications in `/Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/handoff.md`.


## 2026-10-21T14:21:19Z
You are teamwork_preview_spec_miner. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md, and the auditor report in /Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md. Formulate fix strategy for SSE Broker atomic client ID generation and connection cleanup in pkg/api/broker.go. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/handoff.md. Send message when done.
