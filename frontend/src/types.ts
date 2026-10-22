export type JobStatus = 'RUNNING' | 'COMPLETED' | 'INTERRUPTED' | 'FAILED' | 'IDLE';

export interface JobSummary {
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
  summary: JobSummary;
  created_at: string;
  updated_at: string;
}

export interface CatalogProduct {
  sku: string;
  name: string;
  price: number;
  status: 'ACTIVE' | 'WITHDRAWN';
  supplier_timestamp?: string;
  updated_at?: string;
  last_updated_at?: string;
}

export interface DeadLetterIssue {
  issue_id: string;
  job_id: string;
  batch_index: number;
  sku: string;
  raw_payload: string;
  reason: string;
  created_at: string;
}

export interface TimeSeriesPoint {
  timestamp: string;
  batch_index: number;
  added: number;
  updated: number;
  withdrawn: number;
  skipped: number;
  dead_letter: number;
  processing_speed: number;
}

export interface AnalyticsResponse {
  period: string;
  job_id: string;
  time_range: {
    start: string;
    end: string;
  };
  totals: {
    total_jobs: number;
    active_products: number;
    withdrawn_products: number;
    quarantined_issues: number;
    total_events_processed: number;
    total_events_succeeded: number;
    defect_rate: number;
  };
  summary: JobSummary;
  time_series: TimeSeriesPoint[];
  sync_jobs: {
    job_id: string;
    status: JobStatus;
    total_batches: number;
    created_at: string;
  }[];
}

export interface TestScenario {
  key: string;
  name: string;
  desc: string;
  batches: number;
  size: number;
  volume: number;
  quality: string;
  seed: number;
  updateRatio: number;
  dupRatio: number;
  oooRatio: number;
  withdrawnRatio: number;
  invalidRatio: number;
}

export interface CatalogUpdate {
  id: string;
  version: string;
  title: string;
  description: string;
  recordsCount: number;
  batches: number;
  batchSize: number;
  seed: number;
  updateRatio: number;
  dupRatio: number;
  oooRatio: number;
  withdrawnRatio: number;
  invalidRatio: number;
  isInterrupted?: boolean;
}

export const DEFAULT_CATALOG_UPDATES: CatalogUpdate[] = [
  {
    id: 'update-1-baseline',
    version: 'v1.0.0',
    title: 'Baseline Catalog Feed',
    description: '1,000 new products imported directly from supplier baseline.',
    recordsCount: 1000,
    batches: 10,
    batchSize: 100,
    seed: 42,
    updateRatio: 20,
    dupRatio: 5,
    oooRatio: 10,
    withdrawnRatio: 5,
    invalidRatio: 5,
  },
  {
    id: 'update-2-price-refresh',
    version: 'v1.1.0',
    title: 'Price & Inventory Adjustment',
    description: '1,000 products refreshed with new pricing and compliant schema.',
    recordsCount: 1000,
    batches: 10,
    batchSize: 100,
    seed: 101,
    updateRatio: 20,
    dupRatio: 0,
    oooRatio: 0,
    withdrawnRatio: 0,
    invalidRatio: 0,
  },
  {
    id: 'update-3-defect-quarantine',
    version: 'v1.2.0',
    title: 'Supplier Anomaly Feed (DLQ)',
    description: '1,500 records containing price defects and empty SKUs for quarantine testing.',
    recordsCount: 1500,
    batches: 15,
    batchSize: 100,
    seed: 999,
    updateRatio: 15,
    dupRatio: 10,
    oooRatio: 15,
    withdrawnRatio: 10,
    invalidRatio: 15,
  },
  {
    id: 'update-4-high-volume',
    version: 'v1.3.0',
    title: 'High-Volume Restock Stream',
    description: '2,500 products stream-ingested across 25 batches.',
    recordsCount: 2500,
    batches: 25,
    batchSize: 100,
    seed: 777,
    updateRatio: 20,
    dupRatio: 5,
    oooRatio: 10,
    withdrawnRatio: 5,
    invalidRatio: 5,
  },
  {
    id: 'update-5-recovery',
    version: 'v1.4.0',
    title: 'Mid-Feed Crash & Checkpoint Recovery',
    description: 'Halts automatically at batch 5/10 to verify WAL atomic recovery.',
    recordsCount: 1000,
    batches: 10,
    batchSize: 100,
    seed: 555,
    updateRatio: 20,
    dupRatio: 5,
    oooRatio: 10,
    withdrawnRatio: 5,
    invalidRatio: 5,
    isInterrupted: true,
  },
];
