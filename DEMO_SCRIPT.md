# Technical Demonstration & Narration Script
## Assessment 1: Supplier Catalog Updates (Resilient Go Engine)

> **Generated Walkthrough Video**: [`demo_walkthrough.mp4`](file:///Users/png/01.%20Project/coderpush/demo_walkthrough.mp4) (1080p, H.264/AAC, ~2.2 MB)  
> **Source Repository**: [https://github.com/png261/supplier-catalog](https://github.com/png261/supplier-catalog)  
> **Container Registry Package**: `ghcr.io/png261/supplier-catalog:latest`

---

### Executive Overview & Storyboard

This video walkthrough provides a comprehensive technical demonstration of the catalog synchronization engine, detailing its mathematical invariants, zero-broker crash recovery architecture, live embedded web monitoring, and automated production CI/CD verification.

```
+----------------------------------------------------------------------------------------------------+
| SCENE 01 [0:00 - 0:19]  Title & Architectural Mission                                             |
| SCENE 02 [0:19 - 0:46]  Mathematical Data Invariants (A, B, C, D, E)                              |
| SCENE 03 [0:46 - 1:04]  Live Web Dashboard & Real-Time SSE Event Streaming                         |
| SCENE 04 [1:04 - 1:24]  Fault Tolerance: Abrupt Crash Recovery & Resumption Hook (AC1 & AC2)       |
| SCENE 05 [1:24 - 1:46]  Quality Rigor: 93.5% Coverage, 0 Races & 21.3MB GHCR Docker Container       |
+----------------------------------------------------------------------------------------------------+
```

---

### Scene-by-Scene English Narration Script

#### Scene 1: Architecture & System Overview (0:00 - 0:19)
- **Visual**: Dieter Rams / Swiss minimalist title canvas displaying project title, architecture pillars (Deterministic Invariants, Zero-Broker SQLite WAL, Production Rigor).
- **Spoken Audio**:
  > *"Welcome to the technical demonstration of the Supplier Catalog Updates engine. Built with pure Go and embedded SQLite in Write-Ahead Log mode, this engine provides deterministic data synchronization under out-of-order, duplicate, and interrupted stream conditions, without external message broker bloat."*

#### Scene 2: Mathematical Data Invariants (0:19 - 0:46)
- **Visual**: Structured breakdown of the five fundamental system invariants:
  - **Invariant A**: Idempotency (content-hash deduplication).
  - **Invariant B**: Fast-Forward Timestamps (reject stale chronological anomalies).
  - **Invariant C**: Tombstone Preservation (withdrawn products cannot be resurrected).
  - **Invariant D**: Dead-Letter Queue (isolate malformed records without halting stream).
  - **Invariant E**: Atomic Batch Commit (all-or-nothing transactions with crash recovery).
- **Spoken Audio**:
  > *"The synchronization engine strictly enforces five mathematical invariants: Invariant A ensures strict idempotency through content-hash deduplication. Invariant B rejects stale out-of-order updates while advancing newer timestamps. Invariant C guarantees that withdrawn items remain permanent tombstones. Invariant D isolates invalid records into an unblocking dead-letter queue, and Invariant E commits each batch atomically."*

#### Scene 3: Live Embedded Dashboard & Streaming SSE (0:46 - 1:04)
- **Visual**: High-resolution capture of the live web UI (`http://localhost:8080`) during an active synchronization job streaming from the 128-bit PCG pseudo-random generator. Displays real-time progress bar, latency metrics, active job status, and metrics cards.
- **Spoken Audio**:
  > *"Here is the embedded web dashboard, served directly from Go memory without node runtime dependencies. As batches stream from our pseudo-random generator, Server-Sent Events push live progress, batch indices, and latency metrics directly to the browser interface in real time."*

#### Scene 4: Crash Recovery & Resumption Hook (1:04 - 1:24)
- **Visual**: Interactive terminal walkthrough illustrating an abrupt ungraceful process kill (`SIGKILL`), followed by the startup recovery hook discarding uncommitted WAL frames and resuming the job from its exact last checkpoint (`Batch 2/10`).
- **Spoken Audio**:
  > *"Resilience is demonstrated when a running process experiences abrupt termination. Upon restart, the engine's startup hook inspects the SQLite Write-Ahead Log, discards dirty uncommitted frames, and resumes execution seamlessly from the exact last checkpoint, guaranteeing zero duplicate processing."*

#### Scene 5: Quality Gates, Docker Packaging & CI/CD (1:24 - 1:46)
- **Visual**: Presentation metrics card showing:
  - Statement Coverage: **93.5%**
  - Concurrency: **0 Data Races** across 5 iterations
  - Image Size: **21.3 MB** static Alpine container
  - GitHub Actions CI/CD: **100% Green** with automated GHCR publish
- **Spoken Audio**:
  > *"Every component is backed by rigorous automated verification: zero secret leaks, zero race conditions across five test iterations, and ninety-three point five percent test coverage. The engine is packaged into an ultra-lightweight twenty-one megabyte Docker image, published directly to GitHub Container Registry. Thank you for watching."*

---

### How to Play or Regenerate the Video

#### Play the Video Locally:
```bash
# Open directly on macOS
open demo_walkthrough.mp4
```

#### Re-generate Video Anytime:
```bash
python3 scripts/generate_demo.py
```
*(The script automatically starts the local Go engine, captures live web UI screenshots via Chrome headless, synthesizes pristine English voiceover audio tracks via macOS `say`, and encodes the final H.264/AAC MP4 video with `ffmpeg`).*
