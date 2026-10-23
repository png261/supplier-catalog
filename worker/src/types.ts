export interface Env {
  DB: D1Database;
  ENVIRONMENT?: string;
}

export type ProductStatus = 'ACTIVE' | 'WITHDRAWN';

export type JobStatus = 'PENDING' | 'RUNNING' | 'INTERRUPTED' | 'COMPLETED' | 'FAILED';

export interface Product {
  sku: string;
  name: string;
  price: number;
  status: ProductStatus;
  supplier_timestamp: string;
  updated_at: string;
}

export interface SupplierUpdate {
  sku: string;
  name: string;
  price: number;
  status: ProductStatus;
  timestamp: string;
}

export interface Batch {
  job_id: string;
  batch_index: number;
  records: SupplierUpdate[];
}

export interface ChangeSummary {
  added: number;
  updated: number;
  withdrawn: number;
  skipped: number;
  dead_letter: number;
}

export interface SyncJob {
  job_id: string;
  status: JobStatus;
  total_batches: number;
  committed_batch_index: number;
  summary: ChangeSummary;
  created_at: string;
  updated_at: string;
  completed_at?: string | null;
}

export interface BatchCheckpoint {
  job_id: string;
  batch_index: number;
  items_count: number;
  summary: ChangeSummary;
  committed_at: string;
}

export interface DeadLetterRecord {
  issue_id: string;
  job_id: string;
  batch_index: number;
  sku: string;
  raw_payload: string;
  reason: string;
  created_at: string;
}

export interface GeneratorConfig {
  seed: number;
  total_batches: number;
  batch_size: number;
  update_ratio: number;
  duplicate_ratio: number;
  out_of_order_ratio: number;
  withdrawn_ratio: number;
  invalid_ratio: number;
  reservoir_capacity: number;
  base_time?: string;
}

export interface MetricsFilter {
  time_range: string;
  job_id?: string;
  from?: string;
  to?: string;
}

export interface AggregatedMetrics {
  summary: ChangeSummary;
  total_processed: number;
  quality_score: number;
  total_syncs: number;
  completed_syncs: number;
}

export type VersionType = 'INITIAL' | 'PATCH' | 'ROLLBACK' | 'SYNC';
export type DeltaAction = 'ADDED' | 'UPDATED' | 'WITHDRAWN';

export interface CatalogVersion {
  shop_id: string;
  version_id: string;
  version_number: number;
  description: string;
  author: string;
  type: VersionType;
  revert_of_version_id?: string | null;
  items_added: number;
  items_updated: number;
  items_withdrawn: number;
  items_skipped: number;
  items_dead_letter: number;
  total_items_affected: number;
  metadata: string;
  created_at: string;
}

export interface ItemDelta {
  delta_id?: number;
  shop_id?: string;
  version_id: string;
  sku: string;
  action: DeltaAction;
  old_name?: string | null;
  new_name: string;
  old_price?: number | null;
  new_price: number;
  old_status?: ProductStatus | null;
  new_status: ProductStatus;
  old_timestamp?: string | null;
  new_timestamp: string;
}

export interface VersionDiffItem {
  sku: string;
  action: DeltaAction;
  old_name?: string | null;
  new_name: string;
  old_price?: number | null;
  new_price: number;
  old_status?: ProductStatus | null;
  new_status: ProductStatus;
}

export interface VersionDiffResponse {
  version: CatalogVersion;
  changes: VersionDiffItem[];
}

