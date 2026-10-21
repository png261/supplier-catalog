# E2E Test Infra: Resilient Catalog Synchronization Engine

## Test Philosophy
- Opaque-box, requirement-driven. No dependency on internal implementation details.
- Methodology: Category-Partition + Boundary Value Analysis (BVA) + Pairwise Combinatorial Testing + Real-World Workload Testing.
- Progressive testability: Verification mechanisms use standard Go testing and external interfaces (database inspection, HTTP endpoints, CLI stdout/NDJSON).

## Feature Inventory & Test Coverage
| # | Feature | Requirement | Tier 1 (>=5) | Tier 2 (>=5) | Tier 3 (Pairwise) | Tier 4 (Workload) |
|---|---------|-------------|:------------:|:------------:|:-----------------:|:-----------------:|
| 1 | Embedded SQLite Storage | R1 | 5 | 5 | ✓ | ✓ |
| 2 | Product Catalog Repository | R1 | 5 | 5 | ✓ | ✓ |
| 3 | Batch Ingestion Pipeline | R1 | 5 | 5 | ✓ | ✓ |
| 4 | Invariant A: Deterministic Idempotency | R1 | 5 | 5 | ✓ | ✓ |
| 5 | Invariant B: Fast-Forward Timestamps | R1 | 5 | 5 | ✓ | ✓ |
| 6 | Invariant C: Tombstone Preservation | R1 | 5 | 5 | ✓ | ✓ |
| 7 | Invariant D: Dead-Letter Isolation | R2 | 5 | 5 | ✓ | ✓ |
| 8 | Invariant E: Atomic Batch Transactions | R2 | 5 | 5 | ✓ | ✓ |
| 9 | Checkpoint Persistence | R2 | 5 | 5 | ✓ | ✓ |
| 10 | Startup Crash Recovery | R2 | 5 | 5 | ✓ | ✓ |
| 11 | Resumable Synchronization | R2 | 5 | 5 | ✓ | ✓ |
| 12 | Deterministic Data Generator | R3 | 5 | 5 | ✓ | ✓ |
| 13 | Edge-Case Ratio Injection | R3 | 5 | 5 | ✓ | ✓ |
| 14 | Infinite Stream Architecture | R3 | 5 | 5 | ✓ | ✓ |
| 15 | Embedded Staff Web UI | R4 | 5 | 5 | ✓ | ✓ |
| 16 | Real-Time Sync Status & SSE API | R4 | 5 | 5 | ✓ | ✓ |
| 17 | Job Control & Resume API | R4 | 5 | 5 | ✓ | ✓ |
| 18 | Dead-Letter Queue Viewer API | R4 | 5 | 5 | ✓ | ✓ |
| 19 | Secret Leak Prevention (Gitleaks) | R5 | 5 | 5 | ✓ | ✓ |
| 20 | Git Hooks & Code Formatting | R5 | 5 | 5 | ✓ | ✓ |
| 21 | Multi-Stage Docker Container | R5 | 5 | 5 | ✓ | ✓ |
| 22 | Docker Compose Volume Persistence | R5 | 5 | 5 | ✓ | ✓ |
| 23 | GitHub Actions CI/CD Workflow | R5 | 5 | 5 | ✓ | ✓ |

## Test Architecture
- Test Runner: Native Go test framework (`go test -v ./test/e2e/...`).
- Concurrency & Race Safety: `go test -race -count=5 ./...`.
- Directory Layout:
  ```
  test/
  ├── e2e/
  │   ├── e2e_test.go             # Common harness, database init, test server helpers
  │   ├── tier1_feature_test.go   # Tier 1: Feature isolation tests
  │   ├── tier2_boundary_test.go  # Tier 2: Boundary, extremes & edge cases
  │   ├── tier3_pairwise_test.go  # Tier 3: Combinatorial & interaction tests
  │   └── tier4_workload_test.go  # Tier 4: Real-world crash, resume & full pipeline scenarios
  ```

## Real-World Application Scenarios (Tier 4)
| # | Scenario | Features Exercised | Complexity |
|---|----------|--------------------|------------|
| 1 | Full Lifecycle Sync & Idempotent Replay | F1-F6, F12, F16 | Medium |
| 2 | High-Concurrency Mid-Batch Crash & Resumption | F7-F11 | High |
| 3 | Adversarial Chaos Injection (All Anomalies) | F4-F7, F12-F14, F18 | High |
| 4 | Staff Dashboard Control & Dead-Letter Resolution | F15-F18 | Medium |
| 5 | Multi-Stage Docker & Persistent Volume Recovery | F19-F22 | High |

## Coverage Thresholds
- Tier 1: ≥ 115 test cases (≥5 per feature across 23 features)
- Tier 2: ≥ 115 test cases (≥5 boundary/negative cases per feature)
- Tier 3: ≥ 23 cross-feature pairwise interaction test cases
- Tier 4: ≥ 5 realistic application-level end-to-end scenarios
- Total Target: ≥ 258 test cases
