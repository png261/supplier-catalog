import React from 'react';
import { Card } from './ui/card';
import { Progress } from './ui/progress';
import { Gauge, Clock, Hourglass, CheckCircle, AlertCircle, RefreshCw } from 'lucide-react';

interface ProgressBannerProps {
  currentBatch: number;
  totalBatches: number;
  throughput: number;
  eta: string;
  elapsedTime: string;
  smartStatusText: string;
  smartStatusType: 'normal' | 'running' | 'warning' | 'success';
}

export const ProgressBanner: React.FC<ProgressBannerProps> = ({
  currentBatch,
  totalBatches,
  throughput,
  eta,
  elapsedTime,
  smartStatusText,
  smartStatusType,
}) => {
  const percent = totalBatches > 0 ? Math.min(100, Math.round((currentBatch / totalBatches) * 100)) : 0;

  const getSmartStatusIcon = () => {
    switch (smartStatusType) {
      case 'running':
        return <RefreshCw className="h-4 w-4 text-blue-600 animate-spin shrink-0" />;
      case 'warning':
        return <AlertCircle className="h-4 w-4 text-amber-600 shrink-0" />;
      case 'success':
        return <CheckCircle className="h-4 w-4 text-emerald-600 shrink-0" />;
      default:
        return <span className="h-2 w-2 rounded-full bg-slate-400 shrink-0 ml-1 mr-1" />;
    }
  };

  const getSmartStatusBg = () => {
    switch (smartStatusType) {
      case 'running':
        return 'bg-blue-50/80 border-blue-200 text-blue-900';
      case 'warning':
        return 'bg-amber-50/80 border-amber-200 text-amber-900';
      case 'success':
        return 'bg-emerald-50/80 border-emerald-200 text-emerald-900';
      default:
        return 'bg-slate-50 border-slate-200 text-slate-700';
    }
  };

  return (
    <Card className="p-4 sm:p-5 border-slate-200 bg-white shadow-sm space-y-3.5">
      {/* Header & Telemetry */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5">
        <div className="flex items-center gap-2">
          <span className="text-xs font-semibold uppercase tracking-wider text-slate-500">
            Feed Ingestion:
          </span>
          <strong className="text-sm font-bold text-slate-900 font-mono">
            {currentBatch} / {totalBatches} Batches ({percent}%)
          </strong>
        </div>

        {/* Telemetry Items */}
        <div className="flex items-center gap-4 text-xs text-slate-500">
          <div className="flex items-center gap-1.5" title="Ingestion Throughput">
            <Gauge className="h-3.5 w-3.5 text-blue-500" />
            <span>Speed: <strong className="text-slate-700 font-mono">{throughput} rec/s</strong></span>
          </div>
          <div className="flex items-center gap-1.5" title="Estimated Time Remaining">
            <Hourglass className="h-3.5 w-3.5 text-amber-500" />
            <span>ETA: <strong className="text-slate-700 font-mono">{eta}</strong></span>
          </div>
          <div className="flex items-center gap-1.5" title="Elapsed Run Time">
            <Clock className="h-3.5 w-3.5 text-slate-500" />
            <span>Elapsed: <strong className="text-slate-700 font-mono">{elapsedTime}</strong></span>
          </div>
        </div>
      </div>

      {/* Progress Track */}
      <Progress value={percent} className="h-2.5 bg-slate-100" />

      {/* Smart Status Banner */}
      <div
        className={`flex items-center gap-2.5 px-3 py-2 rounded-md border text-xs transition-colors duration-200 ${getSmartStatusBg()}`}
      >
        {getSmartStatusIcon()}
        <span className="font-medium flex-1 leading-snug">{smartStatusText}</span>
      </div>
    </Card>
  );
};
