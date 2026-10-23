import React, { useState } from 'react';
import { Card } from '../ui/card';
import { Button } from '../ui/button';
import { Badge } from '../ui/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '../ui/table';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '../ui/dialog';
import { CatalogVersion, VersionDiffItem } from '../../types';
import { formatDateTime, formatNumber } from '../../lib/utils';
import { VersionDiffModal } from '../VersionDiffModal';
import {
  GitBranch,
  History,
  RotateCcw,
  Eye,
  CheckCircle2,
  AlertTriangle,
  RefreshCw,
  ChevronLeft,
  ChevronRight,
  ShieldCheck,
  Clock,
  User,
  ArrowRight,
  GitCommit,
} from 'lucide-react';

interface VersionsViewProps {
  versions: CatalogVersion[];
  total: number;
  page: number;
  limit: number;
  onPageChange: (page: number) => void;
  isLoading: boolean;
  onRefresh: () => void;
  onRollback: (versionId: string, operator?: string) => Promise<void>;
  onFetchDiff: (versionId: string) => Promise<VersionDiffItem[]>;
  isRollingBack?: boolean;
}

export const VersionsView: React.FC<VersionsViewProps> = ({
  versions,
  total,
  page,
  limit,
  onPageChange,
  isLoading,
  onRefresh,
  onRollback,
  onFetchDiff,
  isRollingBack = false,
}) => {
  const [selectedVersion, setSelectedVersion] = useState<CatalogVersion | null>(null);
  const [diffItems, setDiffItems] = useState<VersionDiffItem[]>([]);
  const [isLoadingDiff, setIsLoadingDiff] = useState(false);
  const [isDiffModalOpen, setIsDiffModalOpen] = useState(false);

  // Rollback confirmation dialog state
  const [rollbackCandidate, setRollbackCandidate] = useState<CatalogVersion | null>(null);
  const [isConfirmOpen, setIsConfirmOpen] = useState(false);
  const [operatorName, setOperatorName] = useState('staff-admin');
  const [actionError, setActionError] = useState<string | null>(null);

  const totalPages = Math.max(1, Math.ceil(total / limit));
  const latestVersion = versions.length > 0 ? versions[0] : null;

  const handleOpenDiff = async (ver: CatalogVersion) => {
    setSelectedVersion(ver);
    setIsDiffModalOpen(true);
    setIsLoadingDiff(true);
    try {
      const items = await onFetchDiff(ver.version_id);
      setDiffItems(items);
    } catch (err) {
      console.error('Failed to load version diff', err);
      setDiffItems([]);
    } finally {
      setIsLoadingDiff(false);
    }
  };

  const handlePromptRollback = (ver: CatalogVersion) => {
    setRollbackCandidate(ver);
    setActionError(null);
    setIsConfirmOpen(true);
  };

  const handleConfirmRollback = async () => {
    if (!rollbackCandidate) return;
    setActionError(null);
    try {
      await onRollback(rollbackCandidate.version_id, operatorName);
      setIsConfirmOpen(false);
      setRollbackCandidate(null);
      if (isDiffModalOpen) {
        setIsDiffModalOpen(false);
      }
    } catch (err: any) {
      setActionError(err.message || 'Rollback execution failed');
    }
  };

  const getVersionTypeBadge = (type?: string) => {
    switch (type) {
      case 'ROLLBACK':
        return (
          <Badge variant="outline" className="border-amber-500 text-amber-700 bg-amber-50 font-mono text-[11px] font-semibold">
            ROLLBACK
          </Badge>
        );
      case 'PATCH':
        return (
          <Badge variant="outline" className="border-indigo-500 text-indigo-700 bg-indigo-50 font-mono text-[11px] font-semibold">
            PATCH
          </Badge>
        );
      case 'SYNC':
        return (
          <Badge variant="outline" className="border-emerald-500 text-emerald-700 bg-emerald-50 font-mono text-[11px] font-semibold">
            SYNC
          </Badge>
        );
      case 'INITIAL':
        return (
          <Badge variant="outline" className="border-slate-400 text-slate-700 bg-slate-50 font-mono text-[11px] font-semibold">
            INITIAL
          </Badge>
        );
      default:
        return (
          <Badge variant="secondary" className="font-mono text-[11px]">
            {type || 'VERSION'}
          </Badge>
        );
    }
  };

  return (
    <div className="space-y-4">
      {/* View Header & Informational Banner */}
      <Card className="p-4 sm:p-5 border-slate-200/80 bg-white shadow-xs">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="h-9 w-9 rounded-lg bg-indigo-50 border border-indigo-100 flex items-center justify-center text-indigo-600">
              <GitBranch className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-base font-semibold text-slate-900 tracking-tight">
                  Catalog Versions & Audit Milestones
                </h2>
                <Badge variant="secondary" className="font-mono text-xs font-medium px-2 py-0 h-5">
                  {formatNumber(total)}
                </Badge>
              </div>
              <p className="text-xs text-slate-500 mt-0.5">
                Atomic Git-revert milestone journaling: Reverting restores historical catalog states without destroying prior audit logs.
              </p>
            </div>
          </div>

          <div className="flex items-center gap-2 self-start sm:self-auto">
            <Button
              variant="outline"
              size="sm"
              onClick={onRefresh}
              disabled={isLoading}
              className="h-8 text-xs font-medium border-slate-200 hover:bg-slate-50"
            >
              <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${isLoading ? 'animate-spin' : ''}`} />
              Refresh
            </Button>
          </div>
        </div>

        {/* Invariant Guarantee Banner */}
        <div className="mt-4 p-3 rounded-lg bg-slate-50 border border-slate-200/70 flex items-start gap-2.5 text-xs text-slate-600">
          <ShieldCheck className="h-4 w-4 text-emerald-600 mt-0.5 flex-shrink-0" />
          <div>
            <span className="font-semibold text-slate-800">Tombstone Invariant Guarantee:</span> Reverting to an earlier version where a product was active will <span className="font-medium text-slate-900 underline underline-offset-2">never resurrect</span> withdrawn items. The system restores historical prices and attributes while maintaining the withdrawn tombstone status.
          </div>
        </div>
      </Card>

      {/* Versions Table Card */}
      <Card className="border-slate-200/80 bg-white shadow-xs overflow-hidden">
        {versions.length === 0 && !isLoading ? (
          <div className="py-16 text-center text-slate-500">
            <GitCommit className="h-10 w-10 text-slate-300 mx-auto mb-3" />
            <h3 className="text-sm font-semibold text-slate-800">No Catalog Versions Recorded Yet</h3>
            <p className="text-xs text-slate-500 mt-1 max-w-sm mx-auto">
              Run a catalog synchronization or publish a patch update to generate version milestones and pre-image delta journals.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader className="bg-slate-50/80">
                <TableRow>
                  <TableHead className="w-[110px] text-xs font-semibold text-slate-700">Version</TableHead>
                  <TableHead className="w-[100px] text-xs font-semibold text-slate-700">Type</TableHead>
                  <TableHead className="text-xs font-semibold text-slate-700">Description & Changes</TableHead>
                  <TableHead className="w-[160px] text-xs font-semibold text-slate-700">Author</TableHead>
                  <TableHead className="w-[180px] text-xs font-semibold text-slate-700">Timestamp</TableHead>
                  <TableHead className="w-[170px] text-right text-xs font-semibold text-slate-700">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {versions.map((ver, idx) => {
                  const isCurrent = idx === 0 && page === 1;
                  const isRevertedVer = ver.type === 'ROLLBACK' || !!ver.revert_of_version_id || !!ver.reverted_version_id;
                  const revertTarget = ver.revert_of_version_id || ver.reverted_version_id;

                  return (
                    <TableRow key={ver.version_id} className={`text-xs hover:bg-slate-50/60 ${isCurrent ? 'bg-indigo-50/30' : ''}`}>
                      {/* Version Number & Current Indicator */}
                      <TableCell className="py-3 font-medium">
                        <div className="flex items-center gap-1.5">
                          <span className="font-mono text-xs font-bold text-slate-900 bg-slate-100 px-2 py-0.5 rounded">
                            v{ver.version_number}
                          </span>
                          {isCurrent && (
                            <span className="inline-flex items-center text-[10px] font-semibold text-indigo-700 bg-indigo-100/80 px-1.5 py-0.2 rounded" title="Current live catalog version">
                              LIVE
                            </span>
                          )}
                        </div>
                        <div className="font-mono text-[10px] text-slate-400 mt-1 truncate max-w-[90px]" title={ver.version_id}>
                          {ver.version_id}
                        </div>
                      </TableCell>

                      {/* Type Badge */}
                      <TableCell className="py-3">
                        <div className="space-y-1">
                          {getVersionTypeBadge(ver.type || ver.trigger_type)}
                          {isRevertedVer && revertTarget && (
                            <div className="text-[10px] text-amber-700 font-mono flex items-center gap-0.5" title={`Revert of ${revertTarget}`}>
                              <ArrowRight className="h-2.5 w-2.5 inline" />
                              <span className="truncate max-w-[80px]">{revertTarget}</span>
                            </div>
                          )}
                        </div>
                      </TableCell>

                      {/* Description & Impact Metrics */}
                      <TableCell className="py-3">
                        <div className="font-medium text-slate-800 line-clamp-1" title={ver.description}>
                          {ver.description || `Milestone Version v${ver.version_number}`}
                        </div>
                        <div className="flex items-center gap-3 text-[11px] text-slate-500 mt-1 font-mono">
                          <span className="text-emerald-700 font-medium">+{ver.items_added || 0} added</span>
                          <span className="text-blue-700 font-medium">~{ver.items_updated || 0} updated</span>
                          <span className="text-rose-700 font-medium">-{ver.items_withdrawn || 0} withdrawn</span>
                          <span className="text-slate-400">({ver.total_items_affected || 0} total)</span>
                        </div>
                      </TableCell>

                      {/* Author */}
                      <TableCell className="py-3">
                        <div className="flex items-center gap-1.5 text-slate-600">
                          <User className="h-3 w-3 text-slate-400" />
                          <span className="font-medium">{ver.author || ver.created_by || 'system'}</span>
                        </div>
                      </TableCell>

                      {/* Timestamp */}
                      <TableCell className="py-3">
                        <div className="flex items-center gap-1.5 text-slate-600">
                          <Clock className="h-3 w-3 text-slate-400" />
                          <span>{formatDateTime(ver.created_at)}</span>
                        </div>
                      </TableCell>

                      {/* Actions */}
                      <TableCell className="py-3 text-right">
                        <div className="flex items-center justify-end gap-1.5">
                          {/* View Diff Button */}
                          <Button
                            variant="outline"
                            size="sm"
                            onClick={() => handleOpenDiff(ver)}
                            className="h-7 px-2 text-xs text-slate-700 hover:text-slate-900 border-slate-200 hover:bg-slate-100"
                            title="Inspect SKU changes in this version"
                          >
                            <Eye className="h-3 w-3 mr-1" />
                            Diff
                          </Button>

                          {/* Rollback / Revert Button */}
                          {!isCurrent ? (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handlePromptRollback(ver)}
                              disabled={isRollingBack}
                              className="h-7 px-2 text-xs text-amber-700 border-amber-300 hover:bg-amber-50 hover:text-amber-800 font-medium"
                              title={`Rollback catalog to v${ver.version_number}`}
                            >
                              <RotateCcw className="h-3 w-3 mr-1" />
                              Revert to v{ver.version_number}
                            </Button>
                          ) : (
                            <span className="inline-flex items-center text-[11px] font-medium text-emerald-700 px-2 py-1">
                              <CheckCircle2 className="h-3.5 w-3.5 mr-1" /> Active
                            </span>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </div>
        )}

        {/* Pagination Bar */}
        {totalPages > 1 && (
          <div className="p-3.5 border-t border-slate-200/80 flex items-center justify-between text-xs text-slate-500 bg-slate-50/50">
            <span>
              Page <span className="font-semibold text-slate-700">{page}</span> of{' '}
              <span className="font-semibold text-slate-700">{totalPages}</span> ({formatNumber(total)} versions)
            </span>
            <div className="flex items-center gap-1">
              <Button
                variant="outline"
                size="sm"
                onClick={() => onPageChange(Math.max(1, page - 1))}
                disabled={page <= 1 || isLoading}
                className="h-7 px-2 text-xs"
              >
                <ChevronLeft className="h-3.5 w-3.5 mr-0.5" />
                Prev
              </Button>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onPageChange(Math.min(totalPages, page + 1))}
                disabled={page >= totalPages || isLoading}
                className="h-7 px-2 text-xs"
              >
                Next
                <ChevronRight className="h-3.5 w-3.5 ml-0.5" />
              </Button>
            </div>
          </div>
        )}
      </Card>

      {/* Diff Inspection Modal */}
      <VersionDiffModal
        isOpen={isDiffModalOpen}
        onOpenChange={setIsDiffModalOpen}
        version={selectedVersion}
        changes={diffItems}
        isLoading={isLoadingDiff}
        onRollbackClick={handlePromptRollback}
        isCurrentVersion={selectedVersion?.version_id === latestVersion?.version_id}
      />

      {/* Rollback Confirmation Modal */}
      <Dialog open={isConfirmOpen} onOpenChange={setIsConfirmOpen}>
        <DialogContent className="sm:max-w-md bg-white">
          <DialogHeader>
            <div className="flex items-center gap-2.5 text-amber-600 mb-1">
              <div className="h-8 w-8 rounded-full bg-amber-50 border border-amber-200 flex items-center justify-center">
                <AlertTriangle className="h-4 w-4 text-amber-600" />
              </div>
              <DialogTitle className="text-base font-semibold text-slate-900">
                Confirm Catalog Reversion
              </DialogTitle>
            </div>
            <DialogDescription className="text-xs text-slate-500 pt-1">
              You are preparing to revert the live catalog back to baseline <strong className="text-slate-800">Version v{rollbackCandidate?.version_number}</strong> ({rollbackCandidate?.version_id}).
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 py-2 text-xs text-slate-600">
            <div className="p-3 rounded-lg bg-amber-50/70 border border-amber-200 text-amber-800 space-y-1.5">
              <div className="font-semibold flex items-center gap-1.5">
                <RotateCcw className="h-3.5 w-3.5 text-amber-700" />
                How Git-Revert Works:
              </div>
              <ul className="list-disc pl-4 space-y-1 text-[11px] text-amber-900/90">
                <li>Creates a new forward-moving milestone version (<strong className="font-mono">ROLLBACK</strong>).</li>
                <li>Inverse deltas restore product prices and names to match v{rollbackCandidate?.version_number}.</li>
                <li><strong className="text-rose-900">Withdrawn items remain withdrawn:</strong> Products withdrawn in later versions are never resurrected.</li>
                <li>Live catalog lookups remain $O(1)$ fast via database indexes.</li>
              </ul>
            </div>

            <div className="space-y-1">
              <label className="text-[11px] font-medium text-slate-700">Operator Identity</label>
              <input
                type="text"
                value={operatorName}
                onChange={(e) => setOperatorName(e.target.value)}
                placeholder="staff-username"
                className="w-full text-xs px-2.5 py-1.5 rounded-md border border-slate-200 focus:outline-none focus:ring-1 focus:ring-indigo-500 bg-white"
              />
            </div>

            {actionError && (
              <div className="p-2.5 rounded-md bg-rose-50 border border-rose-200 text-rose-700 text-xs">
                {actionError}
              </div>
            )}
          </div>

          <DialogFooter className="flex sm:justify-end gap-2 pt-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setIsConfirmOpen(false)}
              disabled={isRollingBack}
              className="text-xs"
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={handleConfirmRollback}
              disabled={isRollingBack}
              className="bg-amber-600 hover:bg-amber-700 text-white text-xs font-semibold"
            >
              {isRollingBack ? (
                <>
                  <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                  Executing Rollback...
                </>
              ) : (
                `Confirm Revert to v${rollbackCandidate?.version_number}`
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};
