import React, { useState } from 'react';
import { Card } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { Input } from './ui/input';
import { Calendar, RefreshCw, Filter } from 'lucide-react';

interface AnalyticsFilterBarProps {
  range: string;
  from: string;
  to: string;
  selectedJobId: string;
  syncJobs: { job_id: string; total_batches: number; created_at: string }[];
  onRangeChange: (newRange: string) => void;
  onCustomDatesApply: (from: string, to: string) => void;
  onJobSelect: (jobId: string) => void;
  onRefresh: () => void;
  isRefreshing: boolean;
}

export const AnalyticsFilterBar: React.FC<AnalyticsFilterBarProps> = ({
  range,
  from,
  to,
  selectedJobId,
  syncJobs,
  onRangeChange,
  onCustomDatesApply,
  onJobSelect,
  onRefresh,
  isRefreshing,
}) => {
  const [customFrom, setCustomFrom] = useState(from);
  const [customTo, setCustomTo] = useState(to);

  const RANGES = [
    { key: 'today', label: 'Today' },
    { key: 'yesterday', label: 'Yesterday' },
    { key: '7d', label: 'Last 7 Days' },
    { key: 'all', label: 'All Time' },
    { key: 'custom', label: 'Custom' },
  ];

  return (
    <Card className="p-4 sm:p-5 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs space-y-3.5 transition-colors">
      <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
        {/* Title and Active Period */}
        <div className="flex flex-wrap items-center gap-2.5">
          <div className="flex items-center gap-2">
            <h2 className="text-base sm:text-lg font-bold text-slate-900 dark:text-slate-100 tracking-tight">
              Operations & Catalog Analytics
            </h2>
            <Badge variant="secondary" className="font-semibold text-xs text-blue-700 dark:text-blue-300 bg-blue-50 dark:bg-blue-950/60 border-blue-200 dark:border-blue-900/50">
              {range.toUpperCase()}
            </Badge>
          </div>
          <span className="text-xs text-slate-500 dark:text-slate-400 hidden sm:inline">
            • Metrics and distribution across synchronized supplier feeds
          </span>
        </div>

        {/* Filter Controls Row */}
        <div className="flex flex-wrap items-center gap-2.5">
          {/* Quick Preset Range Pills */}
          <div className="flex items-center bg-slate-100 dark:bg-slate-800 p-0.5 rounded-lg border border-slate-200 dark:border-slate-700">
            {RANGES.map((r) => (
              <button
                key={r.key}
                type="button"
                onClick={() => onRangeChange(r.key)}
                className={`px-3 py-1 text-xs font-semibold rounded-md transition-all ${
                  range === r.key
                    ? 'bg-white dark:bg-slate-900 text-slate-900 dark:text-slate-100 shadow-xs'
                    : 'text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100'
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>

          {/* Sync Job Dropdown Filter */}
          <div className="flex items-center gap-1.5">
            <Filter className="h-3.5 w-3.5 text-slate-400 dark:text-slate-500 hidden sm:inline" />
            <select
              value={selectedJobId}
              onChange={(e) => onJobSelect(e.target.value)}
              className="h-8 rounded-lg border border-slate-300 dark:border-slate-700 bg-white dark:bg-slate-800 px-2.5 text-xs text-slate-800 dark:text-slate-200 font-medium focus:outline-none focus:ring-1 focus:ring-blue-500"
            >
              <option value="all">All Sync Runs ({syncJobs.length})</option>
              {syncJobs.slice(0, 15).map((j) => (
                <option key={j.job_id} value={j.job_id}>
                  {j.job_id} ({j.total_batches} batches)
                </option>
              ))}
            </select>
          </div>

          {/* Refresh Analytics Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={isRefreshing}
            className="h-8 px-2.5 text-xs font-semibold border-slate-300 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-50 dark:hover:bg-slate-800"
            title="Refresh analytics data"
          >
            <RefreshCw className={`h-3.5 w-3.5 mr-1 text-slate-500 dark:text-slate-400 ${isRefreshing ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
        </div>
      </div>

      {/* Collapsible Custom Date Range Picker */}
      {range === 'custom' && (
        <div className="pt-3 border-t border-slate-100 dark:border-slate-800 flex flex-wrap items-center gap-3 bg-slate-50/70 dark:bg-slate-800/40 p-3 rounded-lg">
          <Calendar className="h-4 w-4 text-slate-500 dark:text-slate-400" />
          <span className="text-xs font-semibold text-slate-700 dark:text-slate-300">Custom Date Range:</span>
          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-500 dark:text-slate-400">From:</span>
            <Input
              type="date"
              value={customFrom}
              onChange={(e) => setCustomFrom(e.target.value)}
              className="h-8 w-36 text-xs bg-white dark:bg-slate-900 border-slate-300 dark:border-slate-700 text-slate-900 dark:text-slate-100"
            />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-500 dark:text-slate-400">To:</span>
            <Input
              type="date"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
              className="h-8 w-36 text-xs bg-white dark:bg-slate-900 border-slate-300 dark:border-slate-700 text-slate-900 dark:text-slate-100"
            />
          </div>
          <Button
            size="sm"
            onClick={() => onCustomDatesApply(customFrom, customTo)}
            className="h-8 text-xs font-semibold bg-blue-600 hover:bg-blue-700 text-white"
          >
            Apply Range
          </Button>
        </div>
      )}
    </Card>
  );
};
