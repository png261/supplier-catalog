# Explorer Dispatch (Gen 2): Milestone 3 Generator Architecture (PRNG & Bounded Memory)

## Mission
Design the core architecture for the programmatic mock data generator in `pkg/generator/`:
1. **Deterministic PRNG**: Use Go's modern `math/rand/v2` with `rand.NewPCG(seed1, seed2)` to guarantee exact determinism across operating systems and runs.
2. **Infinite Stream Architecture ($O(1)$ Memory)**: Design a bounded ring buffer or sliding reservoir of recently generated SKUs (e.g. 500-1000 items) so the generator can produce realistic cross-batch updates, duplicate records, out-of-order timestamps ($T_{new} < T_{old}$), and `WITHDRAWN` tombstones without retaining an unbounded catalog in heap memory.
3. **Calibrated Edge-Case Ratio Injection**:
   - Updates to previously generated items
   - Duplicate records within same batch and across batches
   - Out-of-order timestamps ($T < T_{prev}$)
   - Tombstone preservation / Withdrawn status
   - Corrupt records: negative price, empty/whitespace SKU, zero timestamp, NaN/Inf prices, invalid status
4. **Interface Contract**: Define clean structs, interfaces, and methods (`Generator`, `Config`, `NextBatch(ctx)` or `Stream(ctx)`).

Write your blueprint report to `/Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2/handoff.md`.

## Context Files
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md` (read first)
- `/Users/png/01. Project/coderpush/PROJECT.md`
- `/Users/png/01. Project/coderpush/pkg/domain/models.go`

Send message when done.

## 2026-10-21T12:22:52Z
[Message] timestamp=2026-10-21T12:22:52Z sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8 priority=MESSAGE_PRIORITY_HIGH content=You are teamwork_preview_explorer (Gen 2). Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Design generator architecture using math/rand/v2 PCG PRNG and bounded ring buffer in O(1) memory. Write report to /Users/png/01. Project/coderpush/.agents/teamwork/explorer_m3_1_gen2/handoff.md. Send message when done.
