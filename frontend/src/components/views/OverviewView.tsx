import React from 'react';
import { UpdatesList } from '../UpdatesList';
import { AnalyticsFilterBar } from '../AnalyticsFilterBar';
import { MetricsGrid } from '../MetricsGrid';
import { ChartsSection } from '../ChartsSection';
import { Button } from '../ui/button';
import { AnalyticsResponse, CatalogUpdate, SyncJob } from '../../types';
import { ShieldAlert, ArrowRight, GitBranch } from 'lucide-react';
import { formatNumber } from '../../lib/utils';

interface OverviewViewProps {
  currentBatch: number;
  totalBatches: number;
  throughput: number;
  eta: string;
  elapsedTime: string;
  smartStatusText: string;
  smartStatusType: 'normal' | 'running' | 'warning' | 'success';
  isSyncing?: boolean;
  isInterrupted?: boolean;
  interruptedJob?: SyncJob | null;
  interruptedUpdateId?: string | null;
  onCancelSync?: () => void;
  onResumeSync?: () => void;
  updates: CatalogUpdate[];
  syncedUpdateIds: string[];
  activeSyncingUpdateId: string | null;
  activeRevertingUpdateId?: string | null;
  isRollingBack?: boolean;
  onSyncUpdate: (update: CatalogUpdate) => void;
  onRevertUpdate?: (update: CatalogUpdate) => void;
  onInspectUpdate?: (update: CatalogUpdate) => void;
  analyticsRange: string;
  analyticsFrom: string;
  analyticsTo: string;
  selectedJobId: string;
  analyticsData: AnalyticsResponse;
  metrics: {
    added: number;
    updated: number;
    withdrawn: number;
    skipped: number;
    dead_letter: number;
  };
  deltas: {
    added?: number;
    updated?: number;
    withdrawn?: number;
    skipped?: number;
    dead_letter?: number;
  };
  quarantinedTotal?: number;
  versionsTotal?: number;
  isRefreshingAnalytics: boolean;
  onRangeChange: (range: string) => void;
  onCustomDatesApply: (from: string, to: string) => void;
  onJobSelect: (jobId: string) => void;
  onRefreshAnalytics: () => void;
  onMetricCardClick: (category: 'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter') => void;
  onNavigateToTab: (tab: 'catalog' | 'attention' | 'history') => void;
}

export const OverviewView: React.FC<OverviewViewProps> = ({
  currentBatch,
  totalBatches,
  throughput,
  eta,
  elapsedTime,
  smartStatusText,
  smartStatusType,
  isSyncing,
  isInterrupted,
  interruptedJob,
  interruptedUpdateId,
  onCancelSync,
  onResumeSync,
  updates,
  syncedUpdateIds,
  activeSyncingUpdateId,
  activeRevertingUpdateId = null,
  onSyncUpdate,
  onRevertUpdate,
  onInspectUpdate,
  isRollingBack = false,
  analyticsRange,
  analyticsFrom,
  analyticsTo,
  selectedJobId,
  analyticsData,
  metrics,
  deltas,
  quarantinedTotal = 0,
  versionsTotal = 0,
  isRefreshingAnalytics,
  onRangeChange,
  onCustomDatesApply,
  onJobSelect,
  onRefreshAnalytics,
  onMetricCardClick,
  onNavigateToTab,
}) => {
  return (
    <div className="space-y-5">
      {/* Available Catalog Updates with Inlined Live Progress & Checkpoint Recovery */}
      <UpdatesList
        updates={updates}
        syncedIds={syncedUpdateIds}
        activeSyncingId={activeSyncingUpdateId}
        activeRevertingId={activeRevertingUpdateId}
        interruptedUpdateId={interruptedUpdateId}
        interruptedJob={interruptedJob}
        isAnySyncing={Boolean(isSyncing)}
        onSync={onSyncUpdate}
        onRevert={onRevertUpdate}
        onInspectUpdate={onInspectUpdate}
        isRollingBack={isRollingBack}
        currentBatch={currentBatch}
        totalBatches={totalBatches}
        throughput={throughput}
        eta={eta}
        elapsedTime={elapsedTime}
        smartStatusText={smartStatusText}
        smartStatusType={smartStatusType}
        onCancelSync={onCancelSync}
        isInterrupted={isInterrupted}
        onResumeSync={onResumeSync}
      />

      {/* Contextual Action Bar: Quarantine Remediation & Version Audit Shortcuts */}
      {!isSyncing && (quarantinedTotal > 0 || versionsTotal > 0) && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {quarantinedTotal > 0 && (
            <div className="p-3.5 rounded-xl border border-rose-200/90 bg-rose-50/70 flex items-center justify-between gap-3 shadow-xs">
              <div className="flex items-center gap-2.5 min-w-0">
                <div className="h-8 w-8 rounded-lg bg-rose-100 text-rose-700 flex items-center justify-center shrink-0">
                  <ShieldAlert className="h-4 w-4" />
                </div>
                <div className="min-w-0">
                  <p className="text-xs font-semibold text-rose-950 truncate">
                    {formatNumber(quarantinedTotal)} Quarantined Supplier Record{quarantinedTotal === 1 ? '' : 's'}
                  </p>
                  <p className="text-[11px] text-rose-700 truncate">
                    Isolated in Dead-Letter Queue due to price or schema defects.
                  </p>
                </div>
              </div>
              <Button
                size="sm"
                onClick={() => onNavigateToTab('attention')}
                className="h-7 px-2.5 text-xs font-semibold bg-rose-600 hover:bg-rose-700 text-white shrink-0 shadow-none"
              >
                Review & Fix
                <ArrowRight className="h-3 w-3 ml-1" />
              </Button>
            </div>
          )}

          {versionsTotal > 0 && (
            <div
              className={`p-3.5 rounded-xl border border-indigo-200/80 bg-indigo-50/50 flex items-center justify-between gap-3 shadow-xs ${
                quarantinedTotal === 0 ? 'md:col-span-2' : ''
              }`}
            >
              <div className="flex items-center gap-2.5 min-w-0">
                <div className="h-8 w-8 rounded-lg bg-indigo-100 text-indigo-700 flex items-center justify-center shrink-0">
                  <GitBranch className="h-4 w-4" />
                </div>
                <div className="min-w-0">
                  <p className="text-xs font-semibold text-indigo-950 truncate">
                    {formatNumber(versionsTotal)} Catalog Milestone Version{versionsTotal === 1 ? '' : 's'} Recorded
                  </p>
                  <p className="text-[11px] text-indigo-700 truncate">
                    Inspect SKU-level pre-image diffs or perform an atomic Git-revert rollback.
                  </p>
                </div>
              </div>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onNavigateToTab('history')}
                className="h-7 px-2.5 text-xs font-semibold border-indigo-200 bg-white text-indigo-700 hover:bg-indigo-50 shrink-0 shadow-none"
              >
                History & Versions
                <ArrowRight className="h-3 w-3 ml-1" />
              </Button>
            </div>
          )}
        </div>
      )}

      {/* Operations Analytics Filter Bar */}
      <AnalyticsFilterBar
        range={analyticsRange}
        from={analyticsFrom}
        to={analyticsTo}
        selectedJobId={selectedJobId}
        syncJobs={analyticsData.sync_jobs || []}
        onRangeChange={onRangeChange}
        onCustomDatesApply={onCustomDatesApply}
        onJobSelect={onJobSelect}
        onRefresh={onRefreshAnalytics}
        isRefreshing={isRefreshingAnalytics}
      />

      {/* 5 Summary Metric Cards */}
      <MetricsGrid
        metrics={metrics}
        deltas={deltas}
        onCardClick={onMetricCardClick}
      />

      {/* Visual Analytics Charts Section */}
      <ChartsSection
        totals={analyticsData.totals}
        timeSeries={analyticsData.time_series}
      />
    </div>
  );
};
