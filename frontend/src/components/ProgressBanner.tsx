import React from 'react';
import {
  CheckCircle2,
  AlertCircle,
  Loader2,
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
  const percent =
    totalBatches > 0
      ? Math.min(100, Math.round((currentBatch / totalBatches) * 100))
      : 0;

  const renderStatusIcon = () => {
    switch (smartStatusType) {
      case 'running':
        return <Loader2 className="h-3.5 w-3.5 text-slate-700 animate-spin shrink-0" />;
      case 'warning':
        return <AlertCircle className="h-3.5 w-3.5 text-amber-600 shrink-0" />;
      case 'success':
        return <CheckCircle2 className="h-3.5 w-3.5 text-emerald-600 shrink-0" />;
      default:
        return <span className="h-1.5 w-1.5 rounded-full bg-slate-400 shrink-0" />;
    }
  };

  return (
    <div className="bg-white border border-slate-200/80 rounded-lg px-3.5 py-2.5 shadow-xs transition-colors">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 text-xs mb-2">
        {/* Left: Status icon, text & progress */}
        <div className="flex items-center gap-2 min-w-0">
          {renderStatusIcon()}
          <span className="font-medium text-slate-800 truncate">
            {smartStatusText}
          </span>
          <span className="text-slate-400 font-mono text-[11px] shrink-0">
            {currentBatch > 0 ? `(${currentBatch}/${totalBatches} • ${percent}%)` : `(${percent}%)`}
          </span>
        </div>

        {/* Right: Minimal telemetry */}
        <div className="flex items-center gap-2.5 text-[11px] font-mono text-slate-500 shrink-0">
          {isSyncing && (
            <>
              <span>
                <strong className="text-slate-700 font-semibold">{throughput}</strong> r/s
              </span>
              <span className="text-slate-300">•</span>
              <span>
                ETA <strong className="text-slate-700 font-semibold">{eta}</strong>
              </span>
              <span className="text-slate-300">•</span>
            </>
          )}
          <span>{elapsedTime}</span>

          {/* Quick halt / resume actions */}
          {isSyncing && onCancelSync && (
            <button
              type="button"
              onClick={onCancelSync}
              className="ml-1 inline-flex items-center gap-1 text-[11px] font-sans font-medium text-rose-600 hover:text-rose-700"
              title="Halt active sync"
            >
              <Square className="h-2 w-2 fill-current" />
              Halt
            </button>
          )}
          {isInterrupted && onStartSync && (
            <button
              type="button"
              onClick={onStartSync}
              className="ml-1 inline-flex items-center gap-1 text-[11px] font-sans font-medium text-slate-900 hover:text-slate-700"
              title="Resume interrupted sync"
            >
              <RotateCw className="h-2.5 w-2.5" />
              Resume
            </button>
          )}
        </div>
      </div>

      {/* Sleek thin progress bar */}
      <div className="w-full bg-slate-100 rounded-full h-1 overflow-hidden">
        <div
          className={`h-full rounded-full transition-all duration-300 ${
            smartStatusType === 'warning'
              ? 'bg-amber-500'
              : smartStatusType === 'success'
              ? 'bg-emerald-600'
              : 'bg-slate-900'
          }`}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
};
