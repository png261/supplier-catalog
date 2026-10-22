import React from 'react';
import { Card } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { CatalogUpdate } from '../types';
import { Check, Play, RefreshCw, Layers } from 'lucide-react';

interface UpdatesListProps {
  updates: CatalogUpdate[];
  syncedIds: string[];
  activeSyncingId: string | null;
  isAnySyncing: boolean;
  onSync: (update: CatalogUpdate) => void;
}

export const UpdatesList: React.FC<UpdatesListProps> = ({
  updates,
  syncedIds,
  activeSyncingId,
  isAnySyncing,
  onSync,
}) => {
  const syncedCount = updates.filter((u) => syncedIds.includes(u.id)).length;

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

          return (
            <div
              key={update.id}
              className="py-3 first:pt-0 last:pb-0 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3"
            >
              <div className="space-y-1 min-w-0">
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

              {/* Action */}
              <div className="shrink-0 flex items-center">
                {isSynced ? (
                  <div className="flex items-center gap-1.5 px-3 py-1 rounded-md bg-emerald-50 text-emerald-700 border border-emerald-200 text-xs font-medium">
                    <Check className="h-3.5 w-3.5" />
                    <span>Synced</span>
                  </div>
                ) : isThisSyncing ? (
                  <Button
                    disabled
                    size="sm"
                    className="h-8 px-3 text-xs font-medium bg-slate-100 text-slate-600 border border-slate-200"
                  >
                    <RefreshCw className="h-3 w-3 mr-1.5 animate-spin text-slate-500" />
                    <span>Syncing...</span>
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={() => onSync(update)}
                    disabled={isAnySyncing}
                    className="h-8 px-3 text-xs font-medium bg-slate-900 hover:bg-slate-800 text-white shadow-none transition-colors"
                    title={`Sync ${update.title}`}
                  >
                    <Play className="h-3 w-3 mr-1.5 fill-current" />
                    <span>Sync</span>
                  </Button>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </Card>
  );
};
