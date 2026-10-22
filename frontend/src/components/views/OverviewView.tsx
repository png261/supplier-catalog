import React from 'react';
import { ProgressBanner } from '../ProgressBanner';
import { AnalyticsFilterBar } from '../AnalyticsFilterBar';
import { MetricsGrid } from '../MetricsGrid';
import { ChartsSection } from '../ChartsSection';
import { Card } from '../ui/card';
import { Button } from '../ui/button';
import { Badge } from '../ui/badge';
import { AnalyticsResponse } from '../../types';
import { ArrowRight } from 'lucide-react';

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

      {/* Biểu đồ phân bổ Donut & Biểu đồ lưu lượng Area Chart */}
      <ChartsSection
        totals={analyticsData.totals}
        timeSeries={analyticsData.time_series}
      />

      {/* Quick Navigation & Engine Health Banner */}
      <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
        {/* Card 1: Catalog Navigation */}
        <Card className="p-4 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs flex flex-col justify-between hover:border-indigo-300 dark:hover:border-indigo-700 transition-colors">
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                Live Inventory
              </span>
              <Badge variant="secondary" className="font-mono text-[11px]">
                {metrics.added} items
              </Badge>
            </div>
            <h4 className="text-sm font-bold text-slate-900 dark:text-slate-100">
              Browse Active Catalog
            </h4>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              Search by SKU or name, inspect pricing, and view withdrawal statuses.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onNavigateToTab('catalog')}
            className="mt-3 justify-between p-0 h-auto text-xs font-semibold text-indigo-600 dark:text-indigo-400 hover:text-indigo-700 hover:bg-transparent"
          >
            <span>Open Catalog Explorer</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </Button>
        </Card>

        {/* Card 2: Attention & DLQ */}
        <Card className="p-4 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs flex flex-col justify-between hover:border-amber-300 dark:hover:border-amber-700 transition-colors">
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                Attention Queue
              </span>
              <Badge
                variant={metrics.dead_letter > 0 ? 'destructive' : 'success'}
                className="font-mono text-[11px]"
              >
                {metrics.dead_letter} defects
              </Badge>
            </div>
            <h4 className="text-sm font-bold text-slate-900 dark:text-slate-100">
              Quarantined DLQ Remediation
            </h4>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              Review and correct malformed records without restarting feed pipelines.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onNavigateToTab('attention')}
            className="mt-3 justify-between p-0 h-auto text-xs font-semibold text-amber-600 dark:text-amber-400 hover:text-amber-700 hover:bg-transparent"
          >
            <span>Review Quarantined Items</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </Button>
        </Card>

        {/* Card 3: WAL Concurrency Health */}
        <Card className="p-4 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs flex flex-col justify-between hover:border-emerald-300 dark:hover:border-emerald-700 transition-colors">
          <div className="space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="text-xs font-semibold text-slate-500 dark:text-slate-400 uppercase tracking-wider">
                Engine Health
              </span>
              <Badge variant="success" className="font-mono text-[11px]">
                WAL Normal
              </Badge>
            </div>
            <h4 className="text-sm font-bold text-slate-900 dark:text-slate-100">
              Checkpoint Audit Log
            </h4>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              Inspect session history, committed batch indices, and historical throughput.
            </p>
          </div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => onNavigateToTab('history')}
            className="mt-3 justify-between p-0 h-auto text-xs font-semibold text-emerald-600 dark:text-emerald-400 hover:text-emerald-700 hover:bg-transparent"
          >
            <span>View Sync Audit Trail</span>
            <ArrowRight className="h-3.5 w-3.5" />
          </Button>
        </Card>
      </div>
    </div>
  );
};
