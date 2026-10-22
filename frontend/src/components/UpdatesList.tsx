import React from 'react';
import { Card } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { CatalogUpdate } from '../types';
import {
  Check,
  Play,
  Loader2,
  Layers,
  Square,
  RotateCw,
  AlertCircle,
} from 'lucide-react';

interface UpdatesListProps {
  updates: CatalogUpdate[];
  syncedIds: string[];
  activeSyncingId: string | null;
  isAnySyncing: boolean;
  onSync: (update: CatalogUpdate) => void;
  // Live progress telemetry for active syncing item
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
  isAnySyncing,
  onSync,
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

  return (
    <Card className="p-4 sm:p-5 border-slate-200/80 bg-white shadow-xs space-y-4">
      {/* Header */}
      <div className="flex items-center justify-between pb-3 border-b border-slate-100">
        <div className="flex items-center gap-2">
          <Layers className="h-4 w-4 text-slate-700" />
          <h2 className="text-base font-semibold text-slate-900 tracking-tight">
            Catalog Updates
          </h2>
          <Badge variant="secondary" className="font-mono text-xs font-medium px-2 py-0 h-5">
            {syncedCount}/{updates.length} Synced
          </Badge>
        </div>
        <span className="text-xs text-slate-400 hidden sm:inline">
          Sync available updates sequentially into catalog
        </span>
      </div>

      {/* Updates List */}
      <div className="divide-y divide-slate-100">
        {updates.map((update) => {
          const isSynced = syncedIds.includes(update.id);
          const isThisSyncing = activeSyncingId === update.id;
          const isThisInterrupted = isInterrupted && activeSyncingId === update.id;

          return (
            <div
              key={update.id}
              className={`py-3.5 first:pt-0 last:pb-0 transition-colors ${
                isThisSyncing
                  ? 'bg-slate-50/80 border border-slate-200/80 rounded-lg p-3.5 my-1.5'
                  : ''
              }`}
            >
              <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
                <div className="space-y-1 min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <Badge
                      variant="outline"
                      className="font-mono text-[10px] px-1.5 py-0 h-4 bg-slate-50 border-slate-200 text-slate-600"
                    >
                      {update.version}
                    </Badge>
                    <h3 className="text-xs font-semibold text-slate-800 truncate">
                      {update.title}
                    </h3>
                  </div>
                  <p className="text-xs text-slate-500 leading-relaxed">
                    {update.description}
                  </p>
                  <div className="flex items-center gap-2 text-[11px] text-slate-400 font-mono">
                    <span>{update.recordsCount.toLocaleString()} items</span>
                    <span>•</span>
                    <span>{update.batches} batches</span>
                  </div>
                </div>

                {/* Right Action */}
                <div className="shrink-0 flex items-center">
                  {isSynced ? (
                    <div className="flex items-center gap-1.5 px-3 py-1 rounded-md bg-emerald-50 text-emerald-700 border border-emerald-200 text-xs font-medium">
                      <Check className="h-3.5 w-3.5" />
                      <span>Synced</span>
                    </div>
                  ) : isThisSyncing ? (
                    <div className="flex items-center gap-2">
                      {onCancelSync && (
                        <Button
                          variant="outline"
                          size="sm"
                          onClick={onCancelSync}
                          className="h-7 px-2.5 text-xs font-medium border-rose-200 text-rose-600 hover:bg-rose-50"
                          title="Halt active sync"
                        >
                          <Square className="h-2.5 w-2.5 mr-1 fill-current" />
                          <span>Halt</span>
                        </Button>
                      )}
                    </div>
                  ) : isThisInterrupted && onResumeSync ? (
                    <Button
                      size="sm"
                      onClick={onResumeSync}
                      className="h-8 px-3 text-xs font-medium bg-amber-600 hover:bg-amber-700 text-white shadow-none"
                    >
                      <RotateCw className="h-3 w-3 mr-1.5" />
                      <span>Resume</span>
                    </Button>
                  ) : (
                    <Button
                      size="sm"
                      onClick={() => onSync(update)}
                      disabled={isAnySyncing}
                      className="h-8 px-3 text-xs font-medium bg-slate-900 hover:bg-slate-800 text-white shadow-none transition-colors disabled:opacity-40 disabled:cursor-not-allowed"
                      title={
                        isAnySyncing
                          ? 'Wait for current sync to finish'
                          : `Sync ${update.title}`
                      }
                    >
                      <Play className="h-3 w-3 mr-1.5 fill-current" />
                      <span>Sync</span>
                    </Button>
                  )}
                </div>
              </div>

              {/* Progress Embedded Directly Inside The Active Item */}
              {isThisSyncing && (
                <div className="mt-3 pt-3 border-t border-slate-200/60 space-y-2">
                  <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-1.5 text-xs">
                    {/* Status Message */}
                    <div className="flex items-center gap-1.5 text-slate-800 font-medium truncate">
                      {smartStatusType === 'warning' ? (
                        <AlertCircle className="h-3.5 w-3.5 text-amber-600 shrink-0" />
                      ) : (
                        <Loader2 className="h-3.5 w-3.5 text-slate-700 animate-spin shrink-0" />
                      )}
                      <span className="truncate">{smartStatusText}</span>
                    </div>

                    {/* Live Telemetry */}
                    <div className="flex items-center gap-2.5 text-[11px] font-mono text-slate-500 shrink-0">
                      <span>
                        <strong className="text-slate-700 font-semibold">{throughput}</strong> r/s
                      </span>
                      <span className="text-slate-300">•</span>
                      <span>
                        ETA <strong className="text-slate-700 font-semibold">{eta}</strong>
                      </span>
                      <span className="text-slate-300">•</span>
                      <span>{elapsedTime}</span>
                    </div>
                  </div>

                  {/* Progress Line */}
                  <div className="flex items-center gap-2.5 pt-0.5">
                    <div className="flex-1 bg-slate-200/70 rounded-full h-1.5 overflow-hidden">
                      <div
                        className={`h-full rounded-full transition-all duration-300 ${
                          smartStatusType === 'warning'
                            ? 'bg-amber-500'
                            : 'bg-slate-900'
                        }`}
                        style={{ width: `${percent}%` }}
                      />
                    </div>
                    <span className="text-[11px] font-mono text-slate-600 font-medium shrink-0">
                      Batch {currentBatch}/{totalBatches} ({percent}%)
                    </span>
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
