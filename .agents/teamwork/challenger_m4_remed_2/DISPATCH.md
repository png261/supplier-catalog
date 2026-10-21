# DISPATCH — Challenger 2 (Milestone 4 Iteration 2)

## Role & Focus
You are `teamwork_preview_challenger`.
Focus: Empirical stress testing of SSE Broker multi-client fanout, atomic client ID uniqueness, sudden client disconnects, slow consumer backpressure isolation, and zero-leak shutdown.

## Inputs to Read
- `/Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md`
- `/Users/png/01. Project/coderpush/PROJECT.md`
- Previous auditor report: `/Users/png/01. Project/coderpush/.agents/teamwork/auditor_m4/handoff.md`
- Worker handoff: `/Users/png/01. Project/coderpush/.agents/teamwork/worker_m4_remed/handoff.md`

## Adversarial Challenges to Execute
Empirically challenge:
1. Multi-client fanout: 20 simultaneous client connections hitting `GET /api/events` at the exact same nanosecond -> verify all 20 unique client IDs, `broker.ClientCount() == 20`, and 100% of broadcast events received by all 20 clients.
2. Abrupt disconnects: Half of subscribers disconnecting mid-broadcast -> verify surviving subscribers receive all subsequent events without panic, hang, or error.
3. Slow consumer backpressure: Stalled client socket with high-throughput event broadcast -> verify fast client receives all events within <50ms without blocking, while stalled client buffer drops frames.
4. Clean shutdown: Verify `httptest.Server.Close()` terminates without hanging or leaking goroutines.
5. Run under `go test -v -race -count=5` to verify 0 data races.

Provide an explicit verdict (**APPROVE** or **CONFIRM_CORRECTNESS_FAILED**) in:
`/Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_2/handoff.md`.
Send a message when done.

## 2026-10-21T14:57:26Z
[Message] sender=e6900bb2-6639-41ea-b0fb-48d815c2c1c8
You are teamwork_preview_challenger. Working directory: /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_2. Read instructions in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_2/DISPATCH.md, /Users/png/01. Project/coderpush/PROJECT.md, and /Users/png/01. Project/coderpush/.agents/teamwork/ORIGINAL_REQUEST.md. Adversarially challenge SSE Broker: 20 simultaneous connections at exact same nanosecond -> verify 20 unique IDs and 100% event fanout; abrupt disconnects; slow consumer backpressure buffer isolation; clean server shutdown. Provide explicit verdict (APPROVE or CONFIRM_CORRECTNESS_FAILED) in /Users/png/01. Project/coderpush/.agents/teamwork/challenger_m4_remed_2/handoff.md. Send message when done.
