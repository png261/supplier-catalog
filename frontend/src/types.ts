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
  last_updated_at: string;
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
