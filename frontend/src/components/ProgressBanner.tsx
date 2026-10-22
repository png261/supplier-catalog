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
  Square,
  RotateCw,
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
        return <RefreshCw className="h-3.5 w-3.5 text-slate-700 animate-spin shrink-0" />;
      case 'warning':
        return <AlertCircle className="h-3.5 w-3.5 text-amber-600 shrink-0" />;
      case 'success':
        return <CheckCircle className="h-3.5 w-3.5 text-emerald-600 shrink-0" />;
      default:
        return <span className="h-1.5 w-1.5 rounded-full bg-slate-400 shrink-0" />;
    }
  };

  const getSmartStatusBg = () => {
    switch (smartStatusType) {
      case 'running':
        return 'bg-slate-50 border-slate-200 text-slate-800';
      case 'warning':
        return 'bg-amber-50/60 border-amber-200 text-amber-900';
      case 'success':
        return 'bg-emerald-50/50 border-emerald-200 text-emerald-900';
      default:
        return 'bg-slate-50/60 border-slate-200/80 text-slate-600';
    }
  };

  return (
    <Card className="p-4 border-slate-200/80 bg-white shadow-xs space-y-3">
      {/* Telemetry Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold text-slate-900 font-mono">
            Batch {currentBatch}/{totalBatches}
          </span>
          <span className="text-xs text-slate-400 font-mono">({percent}%)</span>
        </div>

        {/* Telemetry Strip */}
        <div className="flex flex-wrap items-center gap-3 sm:gap-4 text-xs text-slate-500 font-mono">
          <div className="flex items-center gap-1">
            <Gauge className="h-3 w-3 text-slate-400" />
            <span className="font-semibold text-slate-800">{throughput}</span> r/s
          </div>
          <div className="flex items-center gap-1">
            <Clock className="h-3 w-3 text-slate-400" />
            ETA: <span className="font-semibold text-slate-800">{eta}</span>
          </div>
          <div className="flex items-center gap-1">
            <Hourglass className="h-3 w-3 text-slate-400" />
            <span className="font-semibold text-slate-800">{elapsedTime}</span>
          </div>

          {/* Quick halt / resume if active */}
          {isSyncing && onCancelSync && (
            <Button
              variant="outline"
              size="sm"
              onClick={onCancelSync}
              className="h-6 px-2 text-[11px] font-medium border-rose-200 text-rose-600 hover:bg-rose-50"
            >
              <Square className="h-2 w-2 mr-1 fill-current" />
              Halt
            </Button>
          )}
          {isInterrupted && onStartSync && (
            <Button
              size="sm"
              onClick={onStartSync}
              className="h-6 px-2.5 text-[11px] font-medium bg-slate-900 hover:bg-slate-800 text-white shadow-none"
            >
              <RotateCw className="h-2.5 w-2.5 mr-1" />
              Resume
            </Button>
          )}
        </div>
      </div>

      {/* Progress Bar */}
      <Progress value={percent} className="h-1.5 bg-slate-100" />

      {/* Status Line */}
      <div className={`px-2.5 py-1.5 rounded-md border text-xs flex items-center gap-2 ${getSmartStatusBg()}`}>
        {getSmartStatusIcon()}
        <span className="font-normal flex-1 truncate">{smartStatusText}</span>
      </div>
    </Card>
  );
};
