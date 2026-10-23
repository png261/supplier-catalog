import React, { useMemo, useState, useEffect } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { Input } from './ui/input';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table';
import { CatalogUpdate, CatalogVersion, VersionDiffItem } from '../types';
import { formatCurrency, formatDateTime } from '../lib/utils';
import {
  GitCommit,
  ArrowRight,
  CheckCircle2,
  Clock,
  Search,
  X,
  TrendingUp,
  TrendingDown,
  Play,
  AlertTriangle,
} from 'lucide-react';

interface VersionDiffModalProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  version: CatalogVersion | null;
  previousVersion?: CatalogVersion | null;
  changes: VersionDiffItem[];
  isLoading: boolean;
  onRollbackClick?: (targetVersion: CatalogVersion, undoneVersion?: CatalogVersion) => void;
  isCurrentVersion?: boolean;
  previewUpdate?: CatalogUpdate | null;
  isUpdateSynced?: boolean;
  onSyncUpdateClick?: (update: CatalogUpdate) => void;
}

export const VersionDiffModal: React.FC<VersionDiffModalProps> = ({
  isOpen,
  onOpenChange,
  version,
  previousVersion = null,
  changes,
  isLoading,
  onRollbackClick,
  isCurrentVersion = false,
  previewUpdate = null,
  isUpdateSynced = false,
  onSyncUpdateClick,
}) => {
  const [actionFilter, setActionFilter] = useState<string>('ALL');
  const [searchQuery, setSearchQuery] = useState<string>('');

  useEffect(() => {
    if (isOpen) {
      setActionFilter('ALL');
      setSearchQuery('');
    }
  }, [isOpen, version?.version_id]);

  const counts = useMemo(() => {
    let added = 0;
    let updated = 0;
    let withdrawn = 0;
    let dlq = 0;
    for (const c of changes) {
      const act = (c.action || c.operation || 'UPDATED').toUpperCase();
      if (act === 'ADDED') added++;
      else if (act === 'WITHDRAWN') withdrawn++;
      else if (act === 'DEAD_LETTER') dlq++;
      else updated++;
    }
    const hasChanges = changes.length > 0;
    return {
      all: changes.length,
      added: hasChanges ? added : (version?.items_added ?? 0),
      updated: hasChanges ? updated : (version?.items_updated ?? 0),
      withdrawn: hasChanges ? withdrawn : (version?.items_withdrawn ?? 0),
      dlq: hasChanges ? dlq : (version?.items_dead_letter ?? 0),
    };
  }, [changes, version]);

  const filteredChanges = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    return changes.filter((c) => {
      const act = (c.action || c.operation || 'UPDATED').toUpperCase();
      if (actionFilter !== 'ALL' && act !== actionFilter) {
        return false;
      }
      if (q) {
        const matchSku = (c.sku || '').toLowerCase().includes(q);
        const matchNewName = (c.new_name || '').toLowerCase().includes(q);
        const matchOldName = (c.old_name || '').toLowerCase().includes(q);
        return matchSku || matchNewName || matchOldName;
      }
      return true;
    });
  }, [changes, actionFilter, searchQuery]);

  if (!version) return null;

  const isPreviewMode = version.version_id === 'preview' || Boolean(previewUpdate && !isUpdateSynced);

  const versionTypeBadge = (type?: string) => {
    if (isPreviewMode) {
      return (
        <Badge
          variant="outline"
          className="border-indigo-500 text-indigo-700 bg-indigo-50 font-mono text-xs"
        >
          PREVIEW DIFF
        </Badge>
      );
    }
    switch (type) {
      case 'ROLLBACK':
        return (
          <Badge
            variant="outline"
            className="border-amber-500 text-amber-700 bg-amber-50 font-mono text-xs"
          >
            ROLLBACK
          </Badge>
        );
      case 'PATCH':
        return (
          <Badge
            variant="outline"
            className="border-indigo-500 text-indigo-700 bg-indigo-50 font-mono text-xs"
          >
            PATCH
          </Badge>
        );
      case 'SYNC':
        return (
          <Badge
            variant="outline"
            className="border-emerald-500 text-emerald-700 bg-emerald-50 font-mono text-xs"
          >
            SYNC
          </Badge>
        );
      default:
        return (
          <Badge variant="secondary" className="font-mono text-xs">
            {type || 'MILESTONE'}
          </Badge>
        );
    }
  };

  const getActionBadge = (action: string) => {
    switch (action.toUpperCase()) {
      case 'ADDED':
        return (
          <Badge className="bg-emerald-100 text-emerald-800 border-0 text-[10px] font-bold uppercase">
            + ADDED
          </Badge>
        );
      case 'UPDATED':
        return (
          <Badge className="bg-blue-100 text-blue-800 border-0 text-[10px] font-bold uppercase">
            ~ UPDATED
          </Badge>
        );
      case 'WITHDRAWN':
        return (
          <Badge className="bg-rose-100 text-rose-800 border-0 text-[10px] font-bold uppercase">
            - WITHDRAWN
          </Badge>
        );
      case 'DEAD_LETTER':
        return (
          <Badge className="bg-amber-100 text-amber-900 border-0 text-[10px] font-bold uppercase">
            ! QUARANTINED
          </Badge>
        );
      default:
        return (
          <Badge variant="secondary" className="text-[10px] uppercase">
            {action}
          </Badge>
        );
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-4xl max-h-[88vh] flex flex-col p-0 overflow-hidden bg-white">
        <DialogHeader className="p-6 pb-4 border-b border-slate-100 bg-slate-50/50">
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-2.5">
              <div className="h-9 w-9 rounded-lg bg-indigo-50 border border-indigo-100 flex items-center justify-center text-indigo-600 shrink-0">
                <GitCommit className="h-4 w-4" />
              </div>
              <div>
                <DialogTitle className="text-base sm:text-lg font-semibold text-slate-900 flex items-center gap-2 flex-wrap">
                  <span>
                    {previewUpdate
                      ? previewUpdate.title
                      : version.version_number > 0
                      ? `Version v${version.version_number} Row-Level Diff`
                      : `Session ${version.version_id} Row-Level Diff`}
                  </span>
                  {versionTypeBadge(version.type || version.trigger_type)}
                </DialogTitle>
                <DialogDescription className="text-xs text-slate-500 flex items-center gap-2.5 mt-1 flex-wrap">
                  <span className="font-mono text-slate-600 font-medium">{version.version_id}</span>
                  <span>•</span>
                  <span>Author: {version.author || version.created_by || 'supplier-sync'}</span>
                  <span>•</span>
                  <span className="flex items-center gap-1">
                    <Clock className="h-3 w-3" />
                    {formatDateTime(version.created_at)}
                  </span>
                </DialogDescription>
              </div>
            </div>
          </div>
          {version.description && (
            <p className="text-xs text-slate-600 mt-3 bg-white p-2.5 rounded border border-slate-200/80">
              {version.description}
            </p>
          )}
        </DialogHeader>

        {/* Filter Tabs & Search Bar */}
        <div className="px-6 py-3 bg-slate-50/40 border-b border-slate-100 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2.5">
          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              type="button"
              variant={actionFilter === 'ALL' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setActionFilter('ALL')}
              className="h-7 px-2.5 text-xs font-medium"
            >
              All ({counts.all})
            </Button>
            <Button
              type="button"
              variant={actionFilter === 'ADDED' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setActionFilter('ADDED')}
              className={`h-7 px-2.5 text-xs font-mono ${
                actionFilter === 'ADDED'
                  ? 'bg-emerald-600 hover:bg-emerald-700 text-white'
                  : 'text-emerald-700 border-emerald-200 hover:bg-emerald-50'
              }`}
            >
              +{counts.added} Added
            </Button>
            <Button
              type="button"
              variant={actionFilter === 'UPDATED' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setActionFilter('UPDATED')}
              className={`h-7 px-2.5 text-xs font-mono ${
                actionFilter === 'UPDATED'
                  ? 'bg-blue-600 hover:bg-blue-700 text-white'
                  : 'text-blue-700 border-blue-200 hover:bg-blue-50'
              }`}
            >
              ~{counts.updated} Updated
            </Button>
            <Button
              type="button"
              variant={actionFilter === 'WITHDRAWN' ? 'default' : 'outline'}
              size="sm"
              onClick={() => setActionFilter('WITHDRAWN')}
              className={`h-7 px-2.5 text-xs font-mono ${
                actionFilter === 'WITHDRAWN'
                  ? 'bg-rose-600 hover:bg-rose-700 text-white'
                  : 'text-rose-700 border-rose-200 hover:bg-rose-50'
              }`}
            >
              -{counts.withdrawn} Withdrawn
            </Button>
            {counts.dlq > 0 && (
              <Button
                type="button"
                variant={actionFilter === 'DEAD_LETTER' ? 'default' : 'outline'}
                size="sm"
                onClick={() => setActionFilter('DEAD_LETTER')}
                className={`h-7 px-2.5 text-xs font-mono ${
                  actionFilter === 'DEAD_LETTER'
                    ? 'bg-amber-600 hover:bg-amber-700 text-white'
                    : 'text-amber-700 border-amber-200 hover:bg-amber-50'
                }`}
              >
                !{counts.dlq} DLQ
              </Button>
            )}
          </div>

          <div className="relative w-full sm:w-56">
            <Search className="absolute left-2.5 top-2 h-3.5 w-3.5 text-slate-400 pointer-events-none" />
            <Input
              placeholder="Filter SKU or name..."
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              className="pl-8 pr-7 h-7 text-xs bg-white border-slate-200"
            />
            {searchQuery && (
              <button
                type="button"
                onClick={() => setSearchQuery('')}
                className="absolute right-2 top-1.5 text-slate-400 hover:text-slate-700"
              >
                <X className="h-3.5 w-3.5" />
              </button>
            )}
          </div>
        </div>

        {/* Changes Table / Content */}
        <div className="flex-1 overflow-y-auto p-6 pt-3">
          {isLoading ? (
            <div className="py-16 text-center text-slate-400 text-sm">
              <div className="animate-spin h-6 w-6 border-2 border-indigo-600 border-t-transparent rounded-full mx-auto mb-2" />
              Computing row-by-row before & after state transitions...
            </div>
          ) : filteredChanges.length === 0 ? (
            <div className="py-12 text-center text-slate-500 text-sm">
              No matching row deltas found for the selected filter.
            </div>
          ) : (
            <div className="rounded-md border border-slate-200 overflow-hidden">
              <Table>
                <TableHeader className="bg-slate-50">
                  <TableRow>
                    <TableHead className="w-[115px] text-xs font-semibold">Change Type</TableHead>
                    <TableHead className="text-xs font-semibold">SKU & Product Name (Before → After)</TableHead>
                    <TableHead className="w-[220px] text-xs font-semibold">Unit Price (Before → After)</TableHead>
                    <TableHead className="w-[170px] text-xs font-semibold">Status (Before → After)</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filteredChanges.map((change, idx) => {
                    const act = (change.action || change.operation || 'UPDATED').toUpperCase();
                    const hasOldPrice =
                      change.old_price !== null && change.old_price !== undefined;
                    const priceDelta = hasOldPrice
                      ? change.new_price - (change.old_price || 0)
                      : 0;
                    const priceChanged = hasOldPrice && Math.abs(priceDelta) > 0.0001;
                    const nameChanged =
                      Boolean(change.old_name) && change.old_name !== change.new_name;
                    const statusChanged =
                      Boolean(change.old_status) && change.old_status !== change.new_status;

                    return (
                      <TableRow
                        key={`${change.sku}-${idx}`}
                        className="text-xs hover:bg-slate-50/70"
                      >
                        <TableCell className="py-2.5">
                          {getActionBadge(act)}
                        </TableCell>

                        {/* SKU & Name Diff */}
                        <TableCell className="py-2.5 font-medium">
                          <div className="flex items-center gap-2">
                            <span className="font-mono text-slate-900 font-bold">
                              {change.sku}
                            </span>
                            {change.reason && (
                              <Badge
                                variant="outline"
                                className="text-[10px] border-amber-300 bg-amber-50 text-amber-800 font-mono"
                              >
                                <AlertTriangle className="h-2.5 w-2.5 mr-1 inline" />
                                {change.reason}
                              </Badge>
                            )}
                          </div>
                          <div className="text-[11px] text-slate-600 mt-0.5">
                            {nameChanged ? (
                              <div className="flex items-center gap-1.5 flex-wrap">
                                <span className="line-through text-slate-400">
                                  {change.old_name}
                                </span>
                                <ArrowRight className="h-3 w-3 text-blue-600 shrink-0" />
                                <span className="font-semibold text-slate-900 bg-blue-50/80 px-1.5 py-0.2 rounded">
                                  {change.new_name}
                                </span>
                              </div>
                            ) : (
                              <span>{change.new_name}</span>
                            )}
                          </div>
                        </TableCell>

                        {/* Price Diff */}
                        <TableCell className="py-2.5">
                          {act === 'DEAD_LETTER' ? (
                            <span className="font-mono text-rose-600 font-semibold">
                              Invalid ({formatCurrency(change.new_price || 0)})
                            </span>
                          ) : hasOldPrice ? (
                            priceChanged ? (
                              <div className="space-y-0.5 font-mono">
                                <div className="flex items-center gap-1.5">
                                  <span className="line-through text-slate-400">
                                    {formatCurrency(change.old_price || 0)}
                                  </span>
                                  <ArrowRight className="h-3 w-3 text-slate-400 shrink-0" />
                                  <span
                                    className={
                                      priceDelta > 0
                                        ? 'text-emerald-700 font-bold'
                                        : 'text-blue-700 font-bold'
                                    }
                                  >
                                    {formatCurrency(change.new_price)}
                                  </span>
                                </div>
                                <div className="text-[10px] flex items-center gap-1">
                                  {priceDelta > 0 ? (
                                    <span className="text-emerald-600 flex items-center gap-0.5 font-semibold">
                                      <TrendingUp className="h-2.5 w-2.5" />+
                                      {formatCurrency(priceDelta)}
                                    </span>
                                  ) : (
                                    <span className="text-blue-600 flex items-center gap-0.5 font-semibold">
                                      <TrendingDown className="h-2.5 w-2.5" />
                                      {formatCurrency(priceDelta)}
                                    </span>
                                  )}
                                </div>
                              </div>
                            ) : (
                              <div className="font-mono text-slate-600">
                                {formatCurrency(change.new_price)}{' '}
                                <span className="text-[10px] text-slate-400 font-sans">
                                  (no change)
                                </span>
                              </div>
                            )
                          ) : (
                            <div className="font-mono text-emerald-700 font-bold flex items-center gap-1.5">
                              <span className="text-[10px] bg-emerald-50 text-emerald-700 border border-emerald-200 px-1 rounded">
                                NEW
                              </span>
                              <span>{formatCurrency(change.new_price)}</span>
                            </div>
                          )}
                        </TableCell>

                        {/* Status Diff */}
                        <TableCell className="py-2.5">
                          {act === 'DEAD_LETTER' ? (
                            <Badge
                              variant="outline"
                              className="border-amber-300 text-amber-800 bg-amber-50 text-[10px]"
                            >
                              QUARANTINED
                            </Badge>
                          ) : statusChanged ? (
                            <div className="flex items-center gap-1.5 font-mono">
                              <span className="line-through text-slate-400 text-[11px]">
                                {change.old_status}
                              </span>
                              <ArrowRight className="h-3 w-3 text-slate-400 shrink-0" />
                              <Badge
                                variant="outline"
                                className={
                                  change.new_status === 'ACTIVE'
                                    ? 'border-emerald-300 text-emerald-700 bg-emerald-50 font-bold text-[10px]'
                                    : 'border-rose-300 text-rose-700 bg-rose-50 font-bold text-[10px]'
                                }
                              >
                                {change.new_status}
                              </Badge>
                            </div>
                          ) : (
                            <div className="flex items-center gap-1.5">
                              <Badge
                                variant="outline"
                                className={
                                  change.new_status === 'ACTIVE'
                                    ? 'border-emerald-200 text-emerald-700 bg-emerald-50/50 text-[10px]'
                                    : 'border-rose-200 text-rose-700 bg-rose-50/50 text-[10px]'
                                }
                              >
                                {change.new_status}
                              </Badge>
                              {act === 'ADDED' && (
                                <span className="text-[10px] text-emerald-600 font-medium">
                                  (Added)
                                </span>
                              )}
                            </div>
                          )}
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            </div>
          )}
        </div>

        <DialogFooter className="p-4 border-t border-slate-100 bg-slate-50/50 flex sm:justify-between items-center gap-2">
          <div className="text-[11px] text-slate-500">
            {isPreviewMode ? (
              <span className="text-indigo-700 font-medium">
                Dry-run simulation against current live catalog ({filteredChanges.length} rows shown)
              </span>
            ) : isCurrentVersion ? (
              <span className="text-emerald-600 font-medium flex items-center gap-1">
                <CheckCircle2 className="h-3.5 w-3.5" /> Current Active Version ({filteredChanges.length} rows shown)
              </span>
            ) : (
              <span>Showing {filteredChanges.length} of {changes.length} changed rows</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              Close
            </Button>
            {previewUpdate && !isUpdateSynced && onSyncUpdateClick && (
              <Button
                size="sm"
                onClick={() => {
                  onOpenChange(false);
                  onSyncUpdateClick(previewUpdate);
                }}
                className="bg-slate-900 hover:bg-slate-800 text-white font-medium text-xs"
              >
                <Play className="h-3 w-3 mr-1.5 fill-white" />
                Sync {previewUpdate.title} Now
              </Button>
            )}
            {!isPreviewMode && !isCurrentVersion && onRollbackClick && (
              <Button
                variant="destructive"
                size="sm"
                onClick={() => {
                  onOpenChange(false);
                  onRollbackClick(version);
                }}
                className="bg-amber-600 hover:bg-amber-700 text-white font-medium"
              >
                Revert to Version v{version.version_number}
              </Button>
            )}
            {!isPreviewMode &&
              isCurrentVersion &&
              previousVersion &&
              onRollbackClick &&
              version.revert_of_version_id !== 'v0' && (
                <Button
                  variant="destructive"
                  size="sm"
                  onClick={() => {
                    onOpenChange(false);
                    onRollbackClick(previousVersion, version);
                  }}
                  className="bg-amber-600 hover:bg-amber-700 text-white font-medium"
                >
                  Revert Before v{version.version_number} (to v{previousVersion.version_number})
                </Button>
              )}
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
