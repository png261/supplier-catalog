# REST API Reference & Dashboard Workflows

This document covers the interactive staff dashboard workflows and the complete REST API & Server-Sent Events (SSE) specification.

---

## 1. Interactive Workflows (Sequence Diagrams)

### A. Pre-Sync Dry-Run Diff Preview & Live Batch Execution

Before any supplier update mutates the database, clicking **Sync** runs an in-memory dry-run simulation against the current catalog and displays the row-by-row `Before -> After` diff modal.

```mermaid
sequenceDiagram
    actor Staff
    participant UI as Staff Dashboard (React)
    participant API as Sync Engine API
    participant DB as SQLite WAL / D1

    Staff->>UI: Click "Sync" on Catalog Update
    UI->>API: POST /api/jobs/preview (seed, batches, ratios)
    API->>DB: Read current products map (Read-Only)
    API-->>UI: Return dry-run row-by-row deltas (+Added, ~Updated, -Withdrawn, DLQ)
    UI-->>Staff: Display "Preview Catalog Update" Diff Modal

    Staff->>UI: Click "Confirm & Sync"
    UI->>API: POST /api/jobs
    loop For Each Batch B_i (1..N)
        API->>DB: Commit Batch B_i + Checkpoint i (Atomic Transaction)
        API-->>UI: Broadcast SSE Event (progress, batch_index, throughput)
    end
    API->>DB: Record Catalog Version Milestone + Item Deltas
    API-->>UI: Broadcast SSE Event (COMPLETED)
```

### B. Independent Single-Update Revert with Diff Preview & Live Progress

Each synced update can be reverted independently without undoing other updates or resurrecting `WITHDRAWN` tombstones.

```mermaid
sequenceDiagram
    actor Staff
    participant UI as Staff Dashboard (React)
    participant API as Sync Engine API
    participant DB as SQLite WAL / D1

    Staff->>UI: Click "Revert" on a Synced Update
    UI->>API: GET /api/versions/{id}/diff
    API->>DB: Load recorded item deltas for that update
    API-->>UI: Return SKU changes
    UI-->>Staff: Display "Preview Revert" Diff Modal

    Staff->>UI: Click "Confirm & Revert"
    UI->>API: POST /api/versions/{id}/rollback (revert_update_id)
    API->>DB: Restore pre-update prices/names, delete post-baseline additions, keep WITHDRAWN tombstones
    API-->>UI: Stream Revert Batch Progress & Mark Update Pending
```

### C. Quarantine (DLQ) Inspection & Staff Remediation

```mermaid
sequenceDiagram
    actor Staff
    participant UI as Quarantine View
    participant API as Sync Engine API
    participant DB as SQLite WAL / D1

    Staff->>UI: Filter Quarantine by Reason (e.g. NEGATIVE_OR_ZERO_PRICE)
    UI->>API: GET /api/dead-letter?reason=NEGATIVE_OR_ZERO_PRICE
    API-->>UI: Return quarantined records + raw supplier JSON

    Staff->>UI: Click "Review & Fix" -> Enter valid name & positive price
    UI->>API: POST /api/dead-letter/{id}/resolve
    API->>DB: Upsert remediated product + Delete from dead_letter_issues + Create PATCH Version
    API-->>UI: Return 200 OK (Product active in catalog)
```

---

## 2. REST API & SSE Endpoints

Interactive Swagger UI documentation is available at:
- **Cloud**: [https://supplier-catalog-api.pages.dev/swagger/index.html](https://supplier-catalog-api.pages.dev/swagger/index.html) (or `/docs`)
- **Local / Docker**: [http://localhost:8080/swagger/index.html](http://localhost:8080/swagger/index.html) (or `/docs`)

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/jobs` | Start a new synchronization job (`generator` or `batches` mode) |
| `POST` | `/api/jobs/preview` | Dry-run simulation returning row-by-row `Before -> After` diffs before syncing |
| `POST` | `/api/jobs/{id}/resume` | Resume an `INTERRUPTED` job strictly from `committed_batch_index + 1` |
| `POST` | `/api/jobs/{id}/cancel` | Halt an active job at the current batch checkpoint boundary |
| `GET` | `/api/jobs` | List synchronization jobs with status and summary counts |
| `GET` | `/api/jobs/{id}` | Retrieve job status, summary, and committed batch checkpoints |
| `GET` | `/api/products` | Query catalog products with SKU/name search and status filters (`ACTIVE`, `WITHDRAWN`) |
| `GET` | `/api/products/{sku}/history` | Retrieve chronological modification history (`Before -> After` audit trail) for a SKU |
| `GET` | `/api/dead-letter` | List quarantined supplier records with filtering by `reason` and `job_id` |
| `POST` | `/api/dead-letter/{id}/resolve` | Remediate a quarantined record and import it into the live catalog |
| `GET` | `/api/versions` | List immutable catalog version milestones (`SYNC`, `PATCH`, `ROLLBACK`) |
| `GET` | `/api/versions/{id}/diff` | Retrieve row-by-row `Before -> After` SKU changes for a version |
| `POST` | `/api/versions/{id}/rollback` | Revert a specific catalog update (`revert_update_id`) or rollback to a version |
| `GET` | `/api/metrics` | Query aggregated KPIs, catalog distribution, and time-series sync activity |
| `POST` | `/api/database/reset` | Reset the active tenant's database to a clean initial state |
| `GET` | `/api/events` | Server-Sent Events (SSE) stream for real-time batch progress and alerts |
