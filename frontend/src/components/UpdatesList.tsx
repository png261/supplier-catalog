import React from 'react';
import { Card } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { CatalogUpdate } from '../types';
import {
  Check,
  Loader2,
  RotateCw,
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
    <Card className="p-4 sm:p-5 border-slate-200/80 bg-white shadow-xs">
      {/* Header */}
      <div className="flex items-center justify-between pb-3 mb-3 border-b border-slate-100">
        <div className="flex items-center gap-2">
          <h2 className="text-sm font-semibold text-slate-900 tracking-tight">
            Catalog Updates
          </h2>
          <Badge variant="secondary" className="font-mono text-[11px] font-medium px-1.5 py-0 h-4">
            {syncedCount}/{updates.length}
          </Badge>
        </div>
      </div>

      {/* Updates List */}
      <div className="divide-y divide-slate-100">
        {updates.map((update) => {
          const isSynced = syncedIds.includes(update.id);
          const isThisSyncing = activeSyncingId === update.id;
          const isThisInterrupted = isInterrupted && activeSyncingId === update.id;

          return (
            <div key={update.id} className="py-3 first:pt-0 last:pb-0">
              <div className="flex items-center justify-between gap-4">
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2 flex-wrap">
                    <span className="font-mono text-xs text-slate-400 font-medium">
                      {update.version}
                    </span>
                    <span className="text-xs font-semibold text-slate-800">
                      {update.title}
                    </span>
                    <span className="text-xs text-slate-400 font-mono">
                      · {update.recordsCount.toLocaleString()} items
                    </span>
                  </div>
                  <p className="text-xs text-slate-500 truncate mt-0.5">
                    {update.description}
                  </p>
                </div>

                {/* Right Action */}
                <div className="shrink-0 flex items-center">
                  {isSynced ? (
                    <span className="inline-flex items-center gap-1 text-xs text-emerald-600 font-medium">
                      <Check className="h-3.5 w-3.5" />
                      Synced
                    </span>
                  ) : isThisSyncing ? (
                    onCancelSync && (
                      <button
                        type="button"
                        onClick={onCancelSync}
                        className="text-xs text-rose-600 hover:text-rose-700 font-medium px-2 py-1 rounded hover:bg-rose-50 transition-colors"
                        title="Halt active sync"
                      >
                        Halt
                      </button>
                    )
                  ) : isThisInterrupted && onResumeSync ? (
                    <Button
                      size="sm"
                      onClick={onResumeSync}
                      className="h-7 px-2.5 text-xs font-medium bg-amber-600 hover:bg-amber-700 text-white shadow-none"
                    >
                      <RotateCw className="h-3 w-3 mr-1" />
                      Resume
                    </Button>
                  ) : (
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={() => onSync(update)}
                      disabled={isAnySyncing}
                      className="h-7 px-3 text-xs font-medium border-slate-200 text-slate-700 hover:bg-slate-50 hover:text-slate-900 disabled:opacity-30 disabled:hover:bg-transparent shadow-none"
                      title={
                        isAnySyncing
                          ? 'Wait for current sync to finish'
                          : `Sync ${update.title}`
                      }
                    >
                      Sync
                    </Button>
                  )}
                </div>
              </div>

              {/* Inlined Minimal Progress when active */}
              {isThisSyncing && (
                <div className="mt-2.5 space-y-1.5">
                  <div className="flex items-center justify-between text-[11px] font-mono text-slate-500">
                    <div className="flex items-center gap-1.5">
                      <Loader2 className="h-3 w-3 animate-spin text-slate-700" />
                      <span className="text-slate-800 font-medium">
                        Batch {currentBatch}/{totalBatches}
                      </span>
                      <span className="text-slate-400">({percent}%)</span>
                    </div>
                    <div className="flex items-center gap-2 text-slate-400">
                      {throughput > 0 && <span>{throughput} r/s</span>}
                      {eta && eta !== '—' && <span>ETA {eta}</span>}
                      <span>{elapsedTime}</span>
                    </div>
                  </div>
                  <div className="w-full bg-slate-100 rounded-full h-1 overflow-hidden">
                    <div
                      className="bg-slate-900 h-full rounded-full transition-all duration-300"
                      style={{ width: `${percent}%` }}
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
