import React from 'react';
import { Card } from './ui/card';
import { Progress } from './ui/progress';
import { Button } from './ui/button';
import {
  Gauge,
  Clock,
  Hourglass,
  CheckCircle,
  AlertCircle,
  RefreshCw,
  Play,
  RotateCw,
  Square,
} from 'lucide-react';

interface ProgressBannerProps {
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
}

export const ProgressBanner: React.FC<ProgressBannerProps> = ({
  currentBatch,
  totalBatches,
  throughput,
  eta,
  elapsedTime,
  smartStatusText,
  smartStatusType,
  isSyncing = false,
  isInterrupted = false,
  onStartSync,
  onCancelSync,
}) => {
  const percent = totalBatches > 0 ? Math.min(100, Math.round((currentBatch / totalBatches) * 100)) : 0;

  const getSmartStatusIcon = () => {
    switch (smartStatusType) {
      case 'running':
        return <RefreshCw className="h-4 w-4 text-blue-600 dark:text-blue-400 animate-spin shrink-0" />;
      case 'warning':
        return <AlertCircle className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0" />;
      case 'success':
        return <CheckCircle className="h-4 w-4 text-emerald-600 dark:text-emerald-400 shrink-0" />;
      default:
        return <span className="h-2 w-2 rounded-full bg-slate-400 shrink-0 ml-1 mr-1" />;
    }
  };

  const getSmartStatusBg = () => {
    switch (smartStatusType) {
      case 'running':
        return 'bg-blue-50/80 dark:bg-blue-950/40 border-blue-200 dark:border-blue-900/60 text-blue-900 dark:text-blue-200';
      case 'warning':
        return 'bg-amber-50/80 dark:bg-amber-950/40 border-amber-200 dark:border-amber-900/60 text-amber-900 dark:text-amber-200';
      case 'success':
        return 'bg-emerald-50/80 dark:bg-emerald-950/40 border-emerald-200 dark:border-emerald-900/60 text-emerald-900 dark:text-emerald-200';
      default:
        return 'bg-slate-50 dark:bg-slate-800/60 border-slate-200 dark:border-slate-700 text-slate-700 dark:text-slate-300';
    }
  };

  return (
    <Card className="p-4 sm:p-5 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs space-y-3.5 transition-colors">
      {/* Header & Telemetry */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold uppercase tracking-wider text-slate-500 dark:text-slate-400">
            Feed Ingestion:
          </span>
          <span className="text-sm font-bold text-slate-900 dark:text-slate-100 font-mono">
            Batch {currentBatch} of {totalBatches}
          </span>
          <span className="text-xs text-slate-400 dark:text-slate-500 font-mono">({percent}%)</span>
        </div>

        {/* Telemetry Strip & Quick Action */}
        <div className="flex flex-wrap items-center gap-3 sm:gap-4 text-xs text-slate-600 dark:text-slate-400">
          <div className="flex items-center gap-1.5 font-mono">
            <Gauge className="h-3.5 w-3.5 text-blue-600 dark:text-blue-400" />
            <span className="font-semibold text-slate-800 dark:text-slate-200">{throughput}</span> rec/s
          </div>
          <div className="flex items-center gap-1.5 font-mono">
            <Clock className="h-3.5 w-3.5 text-amber-600 dark:text-amber-400" />
            ETA: <span className="font-semibold text-slate-800 dark:text-slate-200">{eta}</span>
          </div>
          <div className="flex items-center gap-1.5 font-mono">
            <Hourglass className="h-3.5 w-3.5 text-emerald-600 dark:text-emerald-400" />
            Elapsed: <span className="font-semibold text-slate-800 dark:text-slate-200">{elapsedTime}</span>
          </div>

          {onStartSync && (
            <div className="flex items-center gap-1.5 pl-2 sm:border-l sm:border-slate-200 dark:sm:border-slate-700">
              {isSyncing ? (
                <>
                  <Button
                    size="sm"
                    disabled
                    className="h-7 px-2.5 text-xs font-semibold bg-blue-50 text-blue-700 border border-blue-200 dark:bg-blue-950/40 dark:border-blue-800 dark:text-blue-300"
                  >
                    <RefreshCw className="h-3 w-3 mr-1 animate-spin text-blue-600 dark:text-blue-400" />
                    Syncing
                  </Button>
                  {onCancelSync && (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={onCancelSync}
                      className="h-7 px-2 text-xs font-medium border-rose-200 text-rose-600 hover:bg-rose-50 dark:border-rose-900/60 dark:text-rose-400"
                    >
                      <Square className="h-2.5 w-2.5 mr-1 fill-current" />
                      Halt
                    </Button>
                  )}
                </>
              ) : isInterrupted ? (
                <Button
                  size="sm"
                  onClick={onStartSync}
                  className="h-7 px-3 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs"
                >
                  <RotateCw className="h-3 w-3 mr-1" />
                  Resume Sync
                </Button>
              ) : (
                <Button
                  size="sm"
                  onClick={onStartSync}
                  className="h-7 px-3 text-xs font-semibold bg-blue-600 hover:bg-blue-700 text-white shadow-xs"
                >
                  <Play className="h-3 w-3 mr-1 fill-current" />
                  Start Sync
                </Button>
              )}
            </div>
          )}
        </div>
      </div>

      {/* Progress Bar */}
      <Progress value={percent} className="h-2 bg-slate-100 dark:bg-slate-800" />

      {/* Smart Status Banner */}
      <div className={`p-2.5 rounded-lg border text-xs flex items-center gap-2.5 transition-colors ${getSmartStatusBg()}`}>
        {getSmartStatusIcon()}
        <span className="font-medium flex-1 leading-relaxed">{smartStatusText}</span>
      </div>
    </Card>
  );
};
