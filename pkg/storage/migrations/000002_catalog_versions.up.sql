-- Migration: 000002_catalog_versions
-- Catalog versioning milestones and pre-image delta journaling

CREATE TABLE IF NOT EXISTS catalog_versions (
    version_id TEXT PRIMARY KEY,
    version_number INTEGER NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    type TEXT NOT NULL CHECK(type IN ('INITIAL', 'PATCH', 'ROLLBACK', 'SYNC')),
    revert_of_version_id TEXT,
    items_added INTEGER NOT NULL DEFAULT 0,
    items_updated INTEGER NOT NULL DEFAULT 0,
    items_withdrawn INTEGER NOT NULL DEFAULT 0,
    items_skipped INTEGER NOT NULL DEFAULT 0,
    items_dead_letter INTEGER NOT NULL DEFAULT 0,
    total_items_affected INTEGER NOT NULL DEFAULT 0,
    metadata TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    FOREIGN KEY (revert_of_version_id) REFERENCES catalog_versions(version_id) ON DELETE SET NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_versions_number ON catalog_versions(version_number DESC);
CREATE INDEX IF NOT EXISTS idx_catalog_versions_created_at ON catalog_versions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_catalog_versions_revert_of ON catalog_versions(revert_of_version_id);

CREATE TABLE IF NOT EXISTS catalog_version_deltas (
    delta_id INTEGER PRIMARY KEY AUTOINCREMENT,
    version_id TEXT NOT NULL,
    sku TEXT NOT NULL,
    action TEXT NOT NULL CHECK(action IN ('ADDED', 'UPDATED', 'WITHDRAWN')),
    old_name TEXT,
    new_name TEXT NOT NULL,
    old_price REAL,
    new_price REAL NOT NULL,
    old_status TEXT CHECK(old_status IS NULL OR old_status IN ('ACTIVE', 'WITHDRAWN')),
    new_status TEXT NOT NULL CHECK(new_status IN ('ACTIVE', 'WITHDRAWN')),
    old_timestamp TEXT,
    new_timestamp TEXT NOT NULL,
    FOREIGN KEY (version_id) REFERENCES catalog_versions(version_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_deltas_version_id ON catalog_version_deltas(version_id, delta_id ASC);
CREATE INDEX IF NOT EXISTS idx_deltas_sku ON catalog_version_deltas(sku);
CREATE INDEX IF NOT EXISTS idx_deltas_version_sku ON catalog_version_deltas(version_id, sku);

-- Composite index to accelerate filtered paginated catalog queries in O(1)
CREATE INDEX IF NOT EXISTS idx_products_status_sku ON products(status, sku);

