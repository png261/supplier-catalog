import React from 'react';
import { ProgressBanner } from '../ProgressBanner';
import { AnalyticsFilterBar } from '../AnalyticsFilterBar';
import { MetricsGrid } from '../MetricsGrid';
import { ChartsSection } from '../ChartsSection';
import { Card } from '../ui/card';
import { Button } from '../ui/button';
import { Badge } from '../ui/badge';
import { AnalyticsResponse } from '../../types';

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
  onStartSync?: () => void;
  onCancelSync?: () => void;
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
  onStartSync,
  onCancelSync,
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
  onNavigateToTab,
}) => {
  return (
    <div className="space-y-6">
      {/* Feed Ingestion Progress Strip */}
      <ProgressBanner
        currentBatch={currentBatch}
        totalBatches={totalBatches}
        throughput={throughput}
        eta={eta}
        elapsedTime={elapsedTime}
        smartStatusText={smartStatusText}
        smartStatusType={smartStatusType}
        isSyncing={isSyncing}
        isInterrupted={isInterrupted}
        onStartSync={onStartSync}
        onCancelSync={onCancelSync}
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

      {/* 5 Interactive Summary Metric Cards */}
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
