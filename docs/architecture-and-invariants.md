# System Architecture & The 5 Core Invariants

This document details the internal architecture, data model, state transitions, and formal synchronization invariants of the **Supplier Catalog Synchronization Engine**.

---

## 1. End-to-End Batch Ingestion Pipeline

Each incoming supplier batch executes inside an isolated transaction boundary, validating records, enforcing monotonic lifecycle rules, recording `Before -> After` item deltas, and persisting checkpoints atomically.

```mermaid
flowchart TD
    Start(["Incoming Supplier Batch B_i"]) --> TxBegin["Begin Atomic SQLite Transaction (IMMEDIATE)"]
    TxBegin --> NextRec{"Next Record R in B_i?"}

    NextRec -- "Yes" --> Validate{"Invariant D:\nValid SKU, Price > 0,\nValid Status & Timestamp?"}
    Validate -- "No (Malformed)" --> Quarantine["Insert into dead_letter_issues\n(DeadLetter++)"]
    Quarantine --> NextRec

    Validate -- "Yes" --> Lookup["Lookup Existing Product P_local by SKU"]
    Lookup --> Exists{"P_local Exists?"}

    Exists -- "No" --> InsertNew["Insert Product (ACTIVE / WITHDRAWN)\nRecord ADDED Delta (Added++ / Withdrawn++)"]
    InsertNew --> NextRec

    Exists -- "Yes" --> CheckIdemp{"Invariant A:\nSHA-256 Content Hash\nP_local == R?"}
    CheckIdemp -- "Yes (Duplicate)" --> SkipDup["Ignore Record (Skipped++)"]
    SkipDup --> NextRec

    CheckIdemp -- "No" --> CheckTombstone{"Invariant C:\nP_local.status == WITHDRAWN?"}
    CheckTombstone -- "Yes (Tombstone Locked)" --> SkipTomb["Reject Resurrection Attempt (Skipped++)"]
    SkipTomb --> NextRec

    CheckTombstone -- "No" --> CheckTime{"Invariant B:\nR.timestamp > P_local.timestamp?"}
    CheckTime -- "No (Stale Out-of-Order)" --> SkipStale["Reject Older Timestamp (Skipped++)"]
    SkipStale --> NextRec

    CheckTime -- "Yes (Newer)" --> ApplyUpsert["Update Product P_local\nRecord UPDATED / WITHDRAWN Delta"]
    ApplyUpsert --> NextRec

    NextRec -- "No (End of Batch)" --> SaveCheckpoint["Invariant E:\nWrite batch_checkpoints(B_i) &\nUpdate sync_jobs.committed_batch_index"]
    SaveCheckpoint --> TxCommit["Commit SQLite WAL Transaction"]
    TxCommit --> PublishSSE["Broadcast Live Progress via SSE (/api/events)"]
```

---

## 2. Product Lifecycle & Tombstone State Machine

Once a product transitions to `WITHDRAWN`, it becomes a permanent tombstone and can **never** be resurrected by older or newer supplier batches.

```mermaid
stateDiagram-v2
    [*] --> ACTIVE : New Product (status = ACTIVE)
    [*] --> WITHDRAWN : New Product (status = WITHDRAWN)

    ACTIVE --> ACTIVE : Newer Timestamp (T_new > T_old)\nPrice or Name Updated
    ACTIVE --> ACTIVE : Duplicate or Stale (T_new <= T_old)\n[Skipped — No Mutation]
    ACTIVE --> WITHDRAWN : Product Withdrawn from Sale\n(Permanent Tombstone)

    WITHDRAWN --> WITHDRAWN : Any Subsequent Update (ACTIVE or WITHDRAWN)\n[Skipped — Tombstone Locked Forever]
```

---

## 3. Sync Job Lifecycle & Crash Recovery State Machine

```mermaid
stateDiagram-v2
    [*] --> RUNNING : Start Sync Job (POST /api/jobs)
    RUNNING --> RUNNING : Commit Batch B_i\n(Save Checkpoint i)
    RUNNING --> COMPLETED : Final Batch Committed\nCreate Catalog Version Milestone
    RUNNING --> INTERRUPTED : Process Crash / Outage / Cancel\nUncommitted Batch Rolled Back
    INTERRUPTED --> RUNNING : Resume Job (POST /api/jobs/{id}/resume)\nContinues from Batch (committed_batch_index + 1)
```

---

## 4. Relational Database Schema (ER Diagram)

```mermaid
erDiagram
    PRODUCTS {
        text shop_id PK
        text sku PK
        text name
        real price
        text status "ACTIVE | WITHDRAWN"
        text supplier_timestamp
        text content_hash "SHA-256"
        text updated_at
    }

    SYNC_JOBS {
        text shop_id PK
        text job_id PK
        text status "RUNNING | INTERRUPTED | COMPLETED | FAILED"
        int total_batches
        int committed_batch_index
        int added_count
        int updated_count
        int withdrawn_count
        int skipped_count
        int dead_letter_count
        text created_at
        text completed_at
    }

    BATCH_CHECKPOINTS {
        text shop_id PK
        text job_id PK
        int batch_index PK
        int items_count
        text summary_json
        text committed_at
    }

    DEAD_LETTER_ISSUES {
        text shop_id PK
        text issue_id PK
        text job_id FK
        int batch_index
        text sku
        text reason "NEGATIVE_OR_ZERO_PRICE | EMPTY_SKU | INVALID_STATUS"
        text raw_payload
        text created_at
    }

    CATALOG_VERSIONS {
        text shop_id PK
        text version_id PK
        int version_number
        text job_id FK
        text type "SYNC | PATCH | ROLLBACK"
        text author
        text description
        text revert_of_version_id
        text created_at
    }

    CATALOG_VERSION_CHANGES {
        int delta_id PK
        text shop_id
        text version_id FK
        text sku
        text action "ADDED | UPDATED | WITHDRAWN"
        text old_name
        real old_price
        text old_status
        text new_name
        real new_price
        text new_status
    }

    SYNC_JOBS ||--o{ BATCH_CHECKPOINTS : "records atomic checkpoints"
    SYNC_JOBS ||--o{ DEAD_LETTER_ISSUES : "quarantines anomalies"
    SYNC_JOBS ||--o| CATALOG_VERSIONS : "creates milestone"
    CATALOG_VERSIONS ||--o{ CATALOG_VERSION_CHANGES : "logs SKU pre/post images"
```

---

## 5. Mathematical Specification of the 5 Core Invariants

### Invariant A: Deterministic Idempotency
- **Rule**: Re-ingesting an identical record payload produces zero catalog mutations and zero drift:
  $$\forall R \in \text{Batch}, \quad P_{local} \equiv R \implies \Delta(\text{Catalog}) = \emptyset, \quad \text{Skipped} \gets \text{Skipped} + 1$$

### Invariant B: Fast-Forward Timestamps (Temporal Monotonicity)
- **Rule**: Incoming records with timestamps older than or equal to the local product's `supplier_timestamp` are rejected:
  $$R.\text{Timestamp} \le P_{local}.\text{SupplierTimestamp} \implies \text{Reject}, \quad \text{Skipped} \gets \text{Skipped} + 1$$

### Invariant C: Tombstone Preservation (Monotonic Status)
- **Rule**: Once a product is marked `WITHDRAWN`, it can never be restored to `ACTIVE` by any future update or rollback:
  $$P_{local}.\text{Status} = \text{WITHDRAWN} \implies \forall R, \quad P_{local}.\text{Status} \gets \text{WITHDRAWN}$$

### Invariant D: Dead-Letter Isolation (Non-Halting Quarantine)
- **Rule**: Records with empty/whitespace SKUs, negative/zero prices, invalid status enums, or missing timestamps are isolated in `dead_letter_issues` without aborting valid sibling records in the same batch.

### Invariant E: Atomic Batch Transactions & Checkpoint Resumability
- **Rule**: Every batch $B_i$ commits product upserts, item deltas, dead-letter records, and `batch_checkpoints(i)` in a single ACID transaction. Resuming an interrupted job starts strictly at `committed_batch_index + 1`.
