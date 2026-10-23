-- Initial schema for products, sync jobs, batch checkpoints, and dead letter issues

CREATE TABLE IF NOT EXISTS products (
    sku TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    price REAL NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('ACTIVE', 'WITHDRAWN')),
    supplier_timestamp TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_products_status ON products(status);

CREATE TABLE IF NOT EXISTS sync_jobs (
    job_id TEXT PRIMARY KEY,
    status TEXT NOT NULL CHECK(status IN ('PENDING', 'RUNNING', 'INTERRUPTED', 'COMPLETED', 'FAILED')),
    total_batches INTEGER NOT NULL DEFAULT 0,
    committed_batch_index INTEGER NOT NULL DEFAULT -1,
    items_added INTEGER NOT NULL DEFAULT 0,
    items_updated INTEGER NOT NULL DEFAULT 0,
    items_withdrawn INTEGER NOT NULL DEFAULT 0,
    items_skipped INTEGER NOT NULL DEFAULT 0,
    items_dead_letter INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    completed_at TEXT
);

CREATE TABLE IF NOT EXISTS batch_checkpoints (
    job_id TEXT NOT NULL,
    batch_index INTEGER NOT NULL,
    items_count INTEGER NOT NULL,
    added_count INTEGER NOT NULL,
    updated_count INTEGER NOT NULL,
    withdrawn_count INTEGER NOT NULL,
    skipped_count INTEGER NOT NULL,
    dead_letter_count INTEGER NOT NULL,
    committed_at TEXT NOT NULL,
    PRIMARY KEY (job_id, batch_index),
    FOREIGN KEY (job_id) REFERENCES sync_jobs(job_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS dead_letter_issues (
    issue_id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL,
    batch_index INTEGER NOT NULL,
    sku TEXT,
    raw_payload TEXT NOT NULL,
    reason TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_issues_job_id ON dead_letter_issues(job_id, batch_index);
