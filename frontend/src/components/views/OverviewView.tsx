import React from 'react';
import { UpdatesList } from '../UpdatesList';
import { AnalyticsFilterBar } from '../AnalyticsFilterBar';
import { MetricsGrid } from '../MetricsGrid';
import { ChartsSection } from '../ChartsSection';
import { AnalyticsResponse, CatalogUpdate } from '../../types';

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
  onCancelSync?: () => void;
  updates: CatalogUpdate[];
  syncedUpdateIds: string[];
  activeSyncingUpdateId: string | null;
  onSyncUpdate: (update: CatalogUpdate) => void;
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
  onCancelSync,
  updates,
  syncedUpdateIds,
  activeSyncingUpdateId,
  onSyncUpdate,
  analyticsRange,
  analyticsFrom,
  analyticsTo,
  selectedJobId,
  analyticsData,
  metrics,
  deltas,
  isRefreshingAnalytics,
  onRangeChange,
  onCustomDatesApply,
  onJobSelect,
  onRefreshAnalytics,
  onMetricCardClick,
}) => {
  return (
    <div className="space-y-5">
      {/* Available Catalog Updates with Inlined Live Progress */}
      <UpdatesList
        updates={updates}
        syncedIds={syncedUpdateIds}
        activeSyncingId={activeSyncingUpdateId}
        isAnySyncing={Boolean(isSyncing)}
        onSync={onSyncUpdate}
        currentBatch={currentBatch}
        totalBatches={totalBatches}
        throughput={throughput}
        eta={eta}
        elapsedTime={elapsedTime}
        smartStatusText={smartStatusText}
        smartStatusType={smartStatusType}
        onCancelSync={onCancelSync}
        isInterrupted={isInterrupted}
      />

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
