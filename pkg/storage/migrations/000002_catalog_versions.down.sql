-- Drop catalog_version_deltas and catalog_versions in reverse dependency order

DROP INDEX IF EXISTS idx_deltas_version_sku;
DROP INDEX IF EXISTS idx_deltas_sku;
DROP INDEX IF EXISTS idx_deltas_version_id;
DROP TABLE IF EXISTS catalog_version_deltas;

DROP INDEX IF EXISTS idx_products_status_sku;

DROP INDEX IF EXISTS idx_catalog_versions_revert_of;
DROP INDEX IF EXISTS idx_catalog_versions_created_at;
DROP INDEX IF EXISTS idx_catalog_versions_number;
DROP TABLE IF EXISTS catalog_versions;
