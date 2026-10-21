# BRIEFING — 2026-10-21T14:28:30Z

## Mission
Formulate authoritative specification and fix strategy for SSE Broker atomic client ID generation and connection lifecycle cleanup in pkg/api/broker.go to eliminate race conditions, ID collisions, and subscriber leaks.

## 🔒 My Identity
- Archetype: teamwork_preview_spec_miner
- Roles: Broker Concurrency Remediation Spec Miner
- Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1
- Original parent: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Milestone: Milestone 4 Remediation

## 🔒 Key Constraints
- Read-only probe: Do NOT modify implementation code directly; formulate the authoritative specification, fix strategy, blueprints, and verification methods in handoff.md.
- Prioritize authoritative sources and exact code evidence.
- Ensure strict compliance with Project layouts and Teamwork protocols.

## Current Parent
- Conversation ID: e6900bb2-6639-41ea-b0fb-48d815c2c1c8
- Updated: 2026-10-21T14:21:19Z

## Task Summary
- **What to build**: Specification miner report with features discovered, edge cases, fix strategy, concrete code blueprints, and verification test suite for SSE Broker.
- **Success criteria**: Detailed specification resolving timestamp-based client ID collisions (e.g. 20 concurrent clients) and ensuring safe unregistration/channel cleanup without hangs or panics.
- **Interface contracts**: /Users/png/01. Project/coderpush/PROJECT.md, /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md
- **Code layout**: pkg/api/broker.go

## Key Decisions Made
- Confirmed root cause of `TestAdversarial_MultiClientFanout_And_ZeroLeak` failure: `time.Now().UnixNano()` produces identical IDs across concurrent goroutines, causing overwrites in `b.clients` map.
- Identified secondary race condition in `ServeHTTP`: flushing headers before registration creates a window where client sees 200 OK before broker finishes map registration.
- Selected atomic uint64 counter (`atomic.AddUint64(&b.nextClientID, 1)`) combined with timestamp (`client-%d-%d`) as the collision-proof ID generation strategy.
- Designed registration order fix: register client before `flusher.Flush()`, buffer `register`/`unregister` channels (size 128), and add pointer-identity check `c == client` during unregistration.

## Artifact Index
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/DISPATCH.md — Incoming assignment
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/progress.md — Liveness & progress tracker
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/BRIEFING.md — Working memory
- /Users/png/01. Project/coderpush/.agents/teamwork/spec_miner_m4_remed_1/handoff.md — Final handoff report
