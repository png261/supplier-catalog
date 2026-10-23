-- Drop initial schema tables and indices in reverse dependency order

DROP INDEX IF EXISTS idx_issues_job_id;
DROP TABLE IF EXISTS dead_letter_issues;
DROP TABLE IF EXISTS batch_checkpoints;
DROP TABLE IF EXISTS sync_jobs;
DROP INDEX IF EXISTS idx_products_status;
DROP TABLE IF EXISTS products;
