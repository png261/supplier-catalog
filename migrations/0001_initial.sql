-- Cloudflare D1 Migration: Initial schema for Catalog Synchronization Engine
-- Multi-tenant SQLite database supporting Invariants A, B, C, D, and E

CREATE TABLE IF NOT EXISTS products (
  shop_id TEXT NOT NULL DEFAULT 'default',
  sku TEXT NOT NULL,
  name TEXT NOT NULL,
  price REAL NOT NULL,
  status TEXT NOT NULL CHECK(status IN ('ACTIVE', 'WITHDRAWN')),
  supplier_timestamp TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (shop_id, sku)
);
CREATE INDEX IF NOT EXISTS idx_products_status ON products(shop_id, status);
CREATE INDEX IF NOT EXISTS idx_products_sku ON products(shop_id, sku);

CREATE TABLE IF NOT EXISTS sync_jobs (
  shop_id TEXT NOT NULL DEFAULT 'default',
  job_id TEXT NOT NULL,
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
  completed_at TEXT,
  PRIMARY KEY (shop_id, job_id)
);
CREATE INDEX IF NOT EXISTS idx_jobs_created_at ON sync_jobs(shop_id, created_at DESC);

CREATE TABLE IF NOT EXISTS batch_checkpoints (
  shop_id TEXT NOT NULL DEFAULT 'default',
  job_id TEXT NOT NULL,
  batch_index INTEGER NOT NULL,
  items_count INTEGER NOT NULL,
  added_count INTEGER NOT NULL,
  updated_count INTEGER NOT NULL,
  withdrawn_count INTEGER NOT NULL,
  skipped_count INTEGER NOT NULL,
  dead_letter_count INTEGER NOT NULL,
  committed_at TEXT NOT NULL,
  PRIMARY KEY (shop_id, job_id, batch_index),
  FOREIGN KEY (shop_id, job_id) REFERENCES sync_jobs(shop_id, job_id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS dead_letter_issues (
  shop_id TEXT NOT NULL DEFAULT 'default',
  issue_id TEXT NOT NULL,
  job_id TEXT NOT NULL,
  batch_index INTEGER NOT NULL,
  sku TEXT,
  raw_payload TEXT NOT NULL,
  reason TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (shop_id, issue_id)
);
CREATE INDEX IF NOT EXISTS idx_issues_job_id ON dead_letter_issues(shop_id, job_id, batch_index);
CREATE INDEX IF NOT EXISTS idx_issues_created_at ON dead_letter_issues(shop_id, created_at DESC);
