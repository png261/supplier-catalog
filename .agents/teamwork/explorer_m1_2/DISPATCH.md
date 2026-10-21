# Milestone 1 Exploration: Synchronization Engine & Invariants A, B, C

You are `teamwork_preview_explorer`.
Working directory: `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2`.
Authoritative source: `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`.
Project specification: `/Users/png/01. Project/coderpush/PROJECT.md`.

Scope for Milestone 1:
1. Synchronization Engine in `pkg/sync/engine.go`:
   - `ExecuteBatch(ctx, batch)` transaction management.
2. Invariant A (Deterministic Idempotency):
   - Compare existing record $P$ with incoming record $R$.
   - If identical (all fields match including timestamp/price/name/status), perform NO database write, increment `Skipped`.
   - Re-running identical batch produces 0 Added, 0 Updated, 0 Withdrawn, $N$ Skipped.
3. Invariant B (Fast-Forward Timestamps):
   - If incoming $T_{incoming} < T_{local}$: record rejected and skipped (`Skipped++`), catalog unchanged.
4. Invariant C (Tombstone Preservation):
   - If existing $P.status == "WITHDRAWN"$:
     - Monotonic rule: status can NEVER flip back to "ACTIVE".
     - Incoming update attempting to reactivate is rejected (`Skipped++`), status remains "WITHDRAWN".
5. Change counters calculation:
   - +Added: new product inserted.
   - ~Updated: existing product updated with newer valid timestamp.
   - -Withdrawn: product transitioned to WITHDRAWN.
   - ↷Skipped: idempotent duplicate, out-of-order drop, or forbidden resurrection.

Deliverables:
- Detailed algorithmic design and Go code blueprints in `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2/handoff.md`.
- Keep heartbeat in `progress.md`.
- Send message upon completion.


## 2026-10-21T08:42:39Z
You are teamwork_preview_explorer. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Provide detailed algorithmic design and code blueprints for pkg/sync/engine.go enforcing Invariants A, B, and C. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m1_2/handoff.md. Update progress.md. Send message when done.
