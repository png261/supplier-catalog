import React from 'react';
import { Card } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { CatalogUpdate, SyncJob } from '../types';
import {
  Check,
  Loader2,
  RotateCw,
  RotateCcw,
  AlertTriangle,
  CheckCircle2,
  Activity,
  Sparkles,
  Square,
  Eye,
} from 'lucide-react';

interface UpdatesListProps {
  updates: CatalogUpdate[];
  syncedIds: string[];
  activeSyncingId: string | null;
  activeRevertingId?: string | null;
  interruptedUpdateId?: string | null;
  interruptedJob?: SyncJob | null;
  isAnySyncing: boolean;
  onSync: (update: CatalogUpdate) => void;
  onRevert?: (update: CatalogUpdate) => void;
  onInspectUpdate?: (update: CatalogUpdate) => void;
  isRollingBack?: boolean;
  // Live progress telemetry for active syncing / reverting item
  currentBatch: number;
  totalBatches: number;
  throughput: number;
  eta: string;
  elapsedTime: string;
  smartStatusText: string;
  smartStatusType: 'normal' | 'running' | 'warning' | 'success';
  onCancelSync?: () => void;
  isInterrupted?: boolean;
  onResumeSync?: () => void;
}

export const UpdatesList: React.FC<UpdatesListProps> = ({
  updates,
  syncedIds,
  activeSyncingId,
  activeRevertingId = null,
  interruptedUpdateId,
  interruptedJob,
  isAnySyncing,
  onSync,
  onRevert,
  onInspectUpdate,
  isRollingBack = false,
  currentBatch,
  totalBatches,
  throughput,
  eta,
  elapsedTime,
  smartStatusText,
  smartStatusType,
  onCancelSync,
  isInterrupted = false,
  onResumeSync,
}) => {
  const syncedCount = updates.filter((u) => syncedIds.includes(u.id)).length;
  const percent =
    totalBatches > 0
      ? Math.min(100, Math.round((currentBatch / totalBatches) * 100))
      : 0;

  const getStatusBannerStyle = () => {
    switch (smartStatusType) {
      case 'running':
        return 'bg-blue-50/80 border-blue-200/80 text-blue-800';
      case 'warning':
        return 'bg-amber-50/90 border-amber-200/90 text-amber-900';
      case 'success':
        return 'bg-emerald-50/80 border-emerald-200/80 text-emerald-800';
      default:
        return 'bg-slate-50/80 border-slate-200/70 text-slate-600';
    }
  };

  const renderStatusIcon = () => {
    switch (smartStatusType) {
      case 'running':
        return <Loader2 className="h-3.5 w-3.5 animate-spin text-blue-600 shrink-0" />;
      case 'warning':
        return <AlertTriangle className="h-3.5 w-3.5 text-amber-600 shrink-0" />;
      case 'success':
        return <CheckCircle2 className="h-3.5 w-3.5 text-emerald-600 shrink-0" />;
      default:
        return <Activity className="h-3.5 w-3.5 text-slate-400 shrink-0" />;
    }
  };

  // Determine which update row should show the interrupted checkpoint state
  const targetInterruptedId =
    interruptedUpdateId ||
    (isInterrupted && activeSyncingId ? activeSyncingId : null) ||
    (isInterrupted ? 'update-5-recovery' : null);

  const interruptedBatch =
    interruptedJob && typeof interruptedJob.committed_batch_index === 'number'
      ? interruptedJob.committed_batch_index + 1
      : currentBatch || 5;
  const interruptedTotal =
    interruptedJob?.total_batches || totalBatches || 10;
  const interruptedPercent =
    interruptedTotal > 0
      ? Math.min(100, Math.round((interruptedBatch / interruptedTotal) * 100))
      : 50;

  return (
    <Card className="p-4 sm:p-5 border-slate-200/80 bg-white shadow-xs">
      {/* Header & Live Engine Telemetry Status Bar */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5 pb-3 mb-3 border-b border-slate-100">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-slate-900 tracking-tight">
            Catalog Updates & Workload Scenarios
          </h2>
          <Badge variant="secondary" className="font-mono text-[11px] font-medium px-1.5 py-0 h-4">
            {syncedCount}/{updates.length} synced
          </Badge>
        </div>

        {/* Smart Status Pill */}
        {smartStatusText && (
          <div
            className={`inline-flex items-center gap-2 px-2.5 py-1 rounded-md border text-xs font-medium transition-colors ${getStatusBannerStyle()}`}
          >
            {renderStatusIcon()}
            <span className="truncate max-w-md">{smartStatusText}</span>
          </div>
        )}
      </div>

      {/* Updates List */}
      <div className="divide-y divide-slate-100">
        {updates.map((update) => {
          const isSynced = syncedIds.includes(update.id);
          const isThisSyncing = isAnySyncing && activeSyncingId === update.id;
          const isThisReverting = isRollingBack && activeRevertingId === update.id;
          const isThisInterrupted =
            !isAnySyncing && !isRollingBack && isInterrupted && targetInterruptedId === update.id;

          return (
            <div
              key={update.id}
              onClick={() => onInspectUpdate && onInspectUpdate(update)}
              className={`py-3 first:pt-0 last:pb-0 transition-colors ${
                onInspectUpdate ? 'cursor-pointer hover:bg-slate-50/75 -mx-2 px-2 rounded-lg' : ''
              } ${
                isThisInterrupted || isThisReverting
                  ? 'bg-amber-50/40 -mx-2 px-2 rounded-lg my-1'
                  : isThisSyncing
                  ? 'bg-slate-50/60 -mx-2 px-2 rounded-lg my-1'
                  : ''
              }`}
              title="Click to inspect row-by-row changes (Before → After diff)"
            >
              <div className="flex items-center justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="text-xs font-semibold text-slate-900 hover:text-indigo-700 transition-colors">
                      {update.title}
                    </span>
                    <span className="text-xs text-slate-400 font-mono">
                      · {update.recordsCount.toLocaleString()} items ({update.batches} batches)
                    </span>
                    {update.invalidRatio >= 10 && (
                      <Badge
                        variant="outline"
                        className="text-[10px] py-0 h-4 border-rose-200 bg-rose-50/60 text-rose-700 font-medium"
                      >
                        DLQ Anomaly Test
                      </Badge>
                    )}
                    {update.isInterrupted && !isThisInterrupted && !isSynced && (
                      <Badge
                        variant="outline"
                        className="text-[10px] py-0 h-4 border-amber-200 bg-amber-50/60 text-amber-700 font-medium"
                      >
                        Auto-Halts at 50%
                      </Badge>
                    )}
                    {isThisInterrupted && (
                      <Badge
                        variant="warning"
                        className="text-[10px] py-0 h-4 font-mono uppercase font-bold"
                      >
                        Checkpoint Paused ({interruptedBatch}/{interruptedTotal})
                      </Badge>
                    )}
                  </div>
                  <p className="text-xs text-slate-500 truncate mt-1">
                    {update.description}
                  </p>
                </div>

                {/* Right Action */}
                <div
                  className="shrink-0 flex items-center gap-1.5"
                  onClick={(e) => e.stopPropagation()}
                >
                  {onInspectUpdate && (
                    <Button
                      type="button"
                      variant="outline"
                      size="sm"
                      onClick={() => onInspectUpdate(update)}
                      className="h-7 px-2 text-xs text-indigo-700 border-indigo-200 bg-indigo-50/40 hover:bg-indigo-100/70 hover:text-indigo-900 font-medium shadow-none"
                      title={`View row-by-row diff for ${update.title}`}
                    >
                      <Eye className="h-3 w-3 mr-1" />
                      Diff
                    </Button>
                  )}
                  {isThisReverting ? (
                    <Badge
                      variant="outline"
                      className="text-[11px] bg-amber-50 text-amber-700 border-amber-300 font-mono font-semibold px-2.5 py-1 animate-pulse"
                    >
                      <Loader2 className="h-3 w-3 mr-1.5 animate-spin text-amber-700" />
                      Reverting...
                    </Badge>
                  ) : isSynced ? (
                    <div className="flex items-center gap-1.5">
                      <span className="inline-flex items-center gap-1 text-xs text-emerald-600 font-semibold bg-emerald-50 border border-emerald-200/60 px-2.5 py-1 rounded-md">
                        <Check className="h-3.5 w-3.5" />
                        Synced
                      </span>
                      {onRevert && (
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          onClick={() => onRevert(update)}
                          disabled={isAnySyncing || isRollingBack}
                          className="h-7 px-2 text-xs text-amber-700 border-amber-300 hover:bg-amber-50 hover:text-amber-800 font-medium shadow-none"
                          title={`Revert ${update.title}`}
                        >
                          <RotateCcw className="h-3 w-3 mr-1" />
                          Revert
                        </Button>
                      )}
                    </div>
                  ) : isThisSyncing ? (
                    onCancelSync && (
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={onCancelSync}
                        className="h-7 px-2.5 text-xs text-rose-600 border-rose-200 hover:bg-rose-50 hover:text-rose-700 font-medium shadow-none"
                        title="Halt active sync and save checkpoint"
                      >
                        <Square className="h-3 w-3 mr-1 fill-rose-600" />
                        Halt
                      </Button>
                    )
                  ) : isThisInterrupted && onResumeSync ? (
                    <Button
                      size="sm"
                      onClick={onResumeSync}
                      disabled={isRollingBack}
                      className="h-7 px-3 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs"
                      title="Resume from committed SQLite WAL batch checkpoint"
                    >
                      <RotateCw className="h-3 w-3 mr-1.5" />
                      Resume Checkpoint
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => onSync(update)}
                      disabled={isAnySyncing || isRollingBack}
                      className="h-7 px-3 text-xs font-medium border-slate-200 text-slate-700 hover:bg-slate-900 hover:text-white hover:border-slate-900 disabled:opacity-40 transition-colors shadow-none"
                      title={
                        isAnySyncing || isRollingBack
                          ? 'Wait for current operation to finish'
                          : `Sync ${update.title}`
                      }
                    >
                      Sync
                    </Button>
                  )}
                </div>
              </div>

              {/* Inlined Live Progress when actively syncing */}
              {isThisSyncing && (
                <div className="mt-2.5 space-y-1.5 pt-2 border-t border-slate-200/60">
                  <div className="flex items-center justify-between text-[11px] font-mono text-slate-600">
                    <div className="flex items-center gap-1.5">
                      <Loader2 className="h-3 w-3 animate-spin text-slate-900" />
                      <span className="text-slate-900 font-semibold">
                        Committing Batch {currentBatch}/{totalBatches}
                      </span>
                      <span className="text-slate-500">({percent}%)</span>
                    </div>
                    <div className="flex items-center gap-2.5 text-slate-500">
                      {throughput > 0 && (
                        <span className="text-emerald-700 font-semibold">{throughput} items/s</span>
                      )}
                      {eta && eta !== '—' && <span>ETA {eta}</span>}
                      <span>Elapsed {elapsedTime}</span>
                    </div>
                  </div>
                  <div className="w-full bg-slate-200/80 rounded-full h-1.5 overflow-hidden">
                    <div
                      className="bg-slate-900 h-full rounded-full transition-all duration-300"
                      style={{ width: `${percent}%` }}
                    />
                  </div>
                </div>
              )}

              {/* Inlined Live Progress when actively reverting */}
              {isThisReverting && (
                <div className="mt-2.5 space-y-1.5 pt-2 border-t border-amber-200/70">
                  <div className="flex items-center justify-between text-[11px] font-mono text-amber-900">
                    <div className="flex items-center gap-1.5">
                      <Loader2 className="h-3 w-3 animate-spin text-amber-700" />
                      <span className="text-amber-950 font-semibold">
                        Reverting Batch {currentBatch}/{totalBatches} — Restoring Pre-Image State
                      </span>
                      <span className="text-amber-700">({percent}%)</span>
                    </div>
                    <div className="flex items-center gap-2.5 text-amber-700">
                      {throughput > 0 && (
                        <span className="text-amber-800 font-semibold">{throughput} items/s</span>
                      )}
                      {eta && eta !== '—' && <span>ETA {eta}</span>}
                      <span>Elapsed {elapsedTime}</span>
                    </div>
                  </div>
                  <div className="w-full bg-amber-200/60 rounded-full h-1.5 overflow-hidden">
                    <div
                      className="bg-amber-600 h-full rounded-full transition-all duration-200"
                      style={{ width: `${percent}%` }}
                    />
                  </div>
                </div>
              )}

              {/* Inlined Checkpoint Progress when interrupted */}
              {isThisInterrupted && (
                <div className="mt-2.5 space-y-1.5 pt-2 border-t border-amber-200/70">
                  <div className="flex items-center justify-between text-[11px] font-mono text-amber-900">
                    <div className="flex items-center gap-1.5">
                      <AlertTriangle className="h-3 w-3 text-amber-600" />
                      <span className="font-semibold">
                        Halted at Batch {interruptedBatch}/{interruptedTotal} ({interruptedPercent}%)
                      </span>
                      <span className="text-amber-700 font-sans text-xs hidden sm:inline">
                        — Atomic checkpoint preserved in SQLite WAL. Click Resume to continue from Batch {interruptedBatch + 1}.
                      </span>
                    </div>
                    {interruptedJob?.job_id && (
                      <span className="text-amber-700">{interruptedJob.job_id}</span>
                    )}
                  </div>
                  <div className="w-full bg-amber-200/60 rounded-full h-1.5 overflow-hidden">
                    <div
                      className="bg-amber-600 h-full rounded-full transition-all duration-300"
                      style={{ width: `${interruptedPercent}%` }}
                    />
                  </div>
                </div>
              )}
            </div>
          );
        })}
      </div>
    </Card>
  );
};
