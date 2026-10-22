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
    <Card className="p-4 sm:p-5 border-slate-200 bg-white shadow-sm space-y-3.5">
      <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
        {/* Title and Active Period */}
        <div className="flex flex-wrap items-center gap-2.5">
          <div className="flex items-center gap-2">
            <h2 className="text-base sm:text-lg font-bold text-slate-900 tracking-tight">
              Operations & Catalog Analytics
            </h2>
            <Badge variant="secondary" className="font-semibold text-xs text-blue-700 bg-blue-50 border-blue-200">
              {range.toUpperCase()}
            </Badge>
          </div>
          <span className="text-xs text-slate-500 hidden sm:inline">
            • Metrics and distribution across synchronized supplier feeds
          </span>
        </div>

        {/* Filters and Tools */}
        <div className="flex flex-wrap items-center gap-2.5">
          {/* Time Range Pills */}
          <div className="inline-flex rounded-lg border border-slate-200 bg-slate-100 p-0.5 text-xs">
            {RANGES.map((r) => (
              <button
                key={r.key}
                type="button"
                onClick={() => onRangeChange(r.key)}
                className={`px-2.5 py-1 rounded-md font-medium transition-all ${
                  range === r.key
                    ? 'bg-white text-slate-900 shadow-sm font-semibold'
                    : 'text-slate-600 hover:text-slate-900'
                }`}
              >
                {r.label}
              </button>
            ))}
          </div>

          {/* Sync Run Selector */}
          <div className="flex items-center gap-1.5 text-xs">
            <label htmlFor="select-job" className="text-slate-500 font-medium shrink-0 flex items-center gap-1">
              <Filter className="h-3 w-3 text-slate-400" />
              <span>Sync Run:</span>
            </label>
            <select
              id="select-job"
              value={selectedJobId}
              onChange={(e) => onJobSelect(e.target.value)}
              className="h-8 rounded-md border border-slate-200 bg-white px-2.5 py-1 text-xs text-slate-800 shadow-sm focus:outline-none focus:ring-1 focus:ring-blue-500 max-w-[190px]"
            >
              <option value="all">All Sync Runs in period</option>
              {syncJobs.filter(j => j && j.job_id).map((j) => (
                <option key={j.job_id} value={j.job_id}>
                  {(j.job_id || '').substring(0, 14)}... ({j.total_batches || 0} b)
                </option>
              ))}
            </select>
          </div>

          {/* Refresh Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={isRefreshing}
            className="h-8 px-2.5 text-xs flex items-center gap-1.5 text-slate-700"
            title="Refresh metrics and time-series charts"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isRefreshing ? 'animate-spin' : ''}`} />
            <span>Refresh</span>
          </Button>
        </div>
      </div>

      {/* Collapsible Custom Date Picker */}
      {range === 'custom' && (
        <div className="flex flex-wrap items-center gap-3 pt-3 border-t border-slate-100 text-xs animate-in fade-in-50">
          <span className="font-medium text-slate-500 flex items-center gap-1">
            <Calendar className="h-3.5 w-3.5 text-slate-400" />
            <span>Select Date Window:</span>
          </span>
          <div className="flex items-center gap-2">
            <span className="text-slate-500">From:</span>
            <Input
              type="date"
              value={customFrom}
              onChange={(e) => setCustomFrom(e.target.value)}
              className="h-8 text-xs w-36"
            />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-slate-500">To:</span>
            <Input
              type="date"
              value={customTo}
              onChange={(e) => setCustomTo(e.target.value)}
              className="h-8 text-xs w-36"
            />
          </div>
          <Button
            variant="default"
            size="sm"
            className="h-8 px-3 text-xs"
            onClick={() => onCustomDatesApply(customFrom, customTo)}
          >
            Apply Range
          </Button>
        </div>
      )}
    </Card>
  );
};
