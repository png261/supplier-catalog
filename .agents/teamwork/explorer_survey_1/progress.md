# Progress — Explorer Survey 1 (Storage & Engine Architecture)

Last visited: 2026-10-21T08:39:30Z

## Status
- [x] Initialized DISPATCH.md, BRIEFING.md, and progress tracking
- [x] Investigate Go environment, toolchain, and existing repo state (Go 1.25.6 darwin/arm64, disk space constraint noted)
- [x] Investigate embedded storage candidates:
  - modernc.org/sqlite: Pure Go (CGO_ENABLED=0), 6.1MB binary, passed `go test -race -count=5`, full SQL/ACID/WAL support. Pinned to v1.36.0 (Go >= 1.21).
  - go.etcd.io/bbolt: Pure Go (CGO_ENABLED=0), 1.7MB binary, key-value only, lacks SQL aggregation/filtering for dashboard.
  - mattn/go-sqlite3: CGO required, disqualifying for lightweight CGO_ENABLED=0 container builds.
- [x] Designed storage schema:
  - `products` (sku PK, name, price, status, supplier_timestamp, updated_at)
  - `sync_jobs` (job_id PK, status, total_batches, committed_batch_index, change summary counters, timestamps)
  - `batch_checkpoints` (job_id, batch_index PK, items_count, added, updated, withdrawn, skipped, dead_letter, committed_at)
  - `dead_letter_issues` (issue_id PK, job_id, batch_index, sku, raw_payload, reason, created_at)
- [x] Defined invariant enforcement logic (idempotency, out-of-order rejection T_new < T_old, tombstone preservation WITHDRAWN terminal, dead-letter isolation)
- [x] Defined crash resumability & checkpoint protocol (atomic per-batch tx, startup recovery marking INTERRUPTED, resume from committed_batch_index + 1)
- [x] Defined concurrency & race safety architecture (WAL mode, busy_timeout=5000, write mutex / single-writer worker pipeline, safe concurrent readers, race clean)
- [x] Author comprehensive handoff.md report
- [x] Update BRIEFING.md
- [x] Notify caller via send_message
