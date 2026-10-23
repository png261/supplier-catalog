import React, { useMemo, useState } from 'react';
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
import { CatalogVersion, SyncJob, VersionDiffItem } from '../../types';
import { formatDateTime, formatNumber } from '../../lib/utils';
import { VersionDiffModal } from '../VersionDiffModal';
import {
  History,
  GitBranch,
  ChevronLeft,
  ChevronRight,
  AlertTriangle,
  RotateCw,
  RotateCcw,
  Eye,
  CheckCircle2,
  RefreshCw,
  ShieldCheck,
  Clock,
  User,
  ArrowRight,
} from 'lucide-react';

interface HistoryViewProps {
  jobs: SyncJob[];
  total: number;
  page: number;
  limit: number;
  onPageChange: (page: number) => void;
  status?: string;
  onStatusChange?: (status: string) => void;
  onResumeJob?: (jobId: string) => void;
  isSyncing?: boolean;
  versions?: CatalogVersion[];
  totalVersions?: number;
  isLoadingVersions?: boolean;
  onRefresh?: () => void;
  onRollback?: (versionId: string, operator?: string) => Promise<void>;
  onFetchDiff?: (versionId: string) => Promise<VersionDiffItem[]>;
  isRollingBack?: boolean;
}

interface UnifiedHistoryEntry {
  id: string;
  job?: SyncJob;
  version?: CatalogVersion;
  entryType: string;
  status: string;
  description: string;
  author: string;
  createdAt: string;
  added: number;
  updated: number;
  withdrawn: number;
  skipped: number;
  deadLetter: number;
}

export const HistoryView: React.FC<HistoryViewProps> = ({
  jobs,
  total,
  page,
  limit,
  onPageChange,
  status = '',
  onStatusChange,
  onResumeJob,
  isSyncing = false,
  versions = [],
  totalVersions = 0,
  isLoadingVersions = false,
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

  const interruptedJobs = jobs.filter((j) => j.status === 'INTERRUPTED' || j.status === 'FAILED');

  // Identify the highest version number across all known versions as the current LIVE version
  const latestVersion = useMemo(() => {
    if (!versions || versions.length === 0) return null;
    return versions.reduce((best, cur) =>
      (cur.version_number || 0) > (best.version_number || 0) ? cur : best
    , versions[0]);
  }, [versions]);

  // Merge SyncJobs and CatalogVersions into a unified timeline
  const unifiedEntries = useMemo<UnifiedHistoryEntry[]>(() => {
    const versionById = new Map<string, CatalogVersion>();
    for (const v of versions) {
      versionById.set(v.version_id, v);
    }

    const entries: UnifiedHistoryEntry[] = [];
    const matchedVersionIds = new Set<string>();

    for (const j of jobs) {
      const ver = versionById.get(j.job_id);
      if (ver) {
        matchedVersionIds.add(ver.version_id);
      }
      entries.push({
        id: j.job_id,
        job: j,
        version: ver,
        entryType: ver?.type || ver?.trigger_type || 'SYNC',
        status: j.status,
        description: ver?.description || `Supplier Catalog Sync (${j.job_id})`,
        author: ver?.author || ver?.created_by || 'supplier-sync',
        createdAt: j.created_at || ver?.created_at || '',
        added: j.summary?.added ?? ver?.items_added ?? 0,
        updated: j.summary?.updated ?? ver?.items_updated ?? 0,
        withdrawn: j.summary?.withdrawn ?? ver?.items_withdrawn ?? 0,
        skipped: j.summary?.skipped ?? ver?.items_skipped ?? 0,
        deadLetter: j.summary?.dead_letter ?? ver?.items_dead_letter ?? 0,
      });
    }

    // Add standalone versions (such as ROLLBACK, PATCH, INITIAL, or versions from earlier pages)
    if (status === '' || status === 'ALL' || status === 'ROLLBACK' || status === 'COMPLETED') {
      for (const v of versions) {
        if (matchedVersionIds.has(v.version_id)) continue;
        const vType = v.type || v.trigger_type || 'SYNC';
        if (status === 'ROLLBACK' && vType !== 'ROLLBACK') continue;
        entries.push({
          id: v.version_id,
          version: v,
          entryType: vType,
          status: 'COMPLETED',
          description: v.description || `Milestone Version v${v.version_number}`,
          author: v.author || v.created_by || 'staff',
          createdAt: v.created_at,
          added: v.items_added ?? 0,
          updated: v.items_updated ?? 0,
          withdrawn: v.items_withdrawn ?? 0,
          skipped: v.items_skipped ?? 0,
          deadLetter: v.items_dead_letter ?? 0,
        });
      }
    }

    const filtered = status === 'ROLLBACK'
      ? entries.filter((e) => e.entryType === 'ROLLBACK')
      : entries;

    return filtered.sort((a, b) => {
      const timeA = new Date(a.createdAt).getTime() || 0;
      const timeB = new Date(b.createdAt).getTime() || 0;
      if (timeA !== timeB) return timeB - timeA;
      return (b.version?.version_number || 0) - (a.version?.version_number || 0);
    });
  }, [jobs, versions, status]);

  const combinedTotal = Math.max(total, totalVersions, unifiedEntries.length);
  const totalPages = Math.max(1, Math.ceil(combinedTotal / limit));

  const handleOpenDiff = async (ver: CatalogVersion) => {
    if (!onFetchDiff) return;
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
    if (!rollbackCandidate || !onRollback) return;
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
          <Badge variant="outline" className="border-amber-500 text-amber-700 bg-amber-50 font-mono text-[10px] font-semibold">
            ROLLBACK
          </Badge>
        );
      case 'PATCH':
        return (
          <Badge variant="outline" className="border-indigo-500 text-indigo-700 bg-indigo-50 font-mono text-[10px] font-semibold">
            PATCH
          </Badge>
        );
      case 'SYNC':
        return (
          <Badge variant="outline" className="border-emerald-500 text-emerald-700 bg-emerald-50 font-mono text-[10px] font-semibold">
            SYNC
          </Badge>
        );
      default:
        return (
          <Badge variant="secondary" className="font-mono text-[10px]">
            {type || 'SYNC'}
          </Badge>
        );
    }
  };

  return (
    <div className="space-y-4">
      {/* View Header & Filter Bar */}
      <Card className="p-4 sm:p-5 border-slate-200/80 bg-white shadow-xs">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="h-9 w-9 rounded-lg bg-indigo-50 border border-indigo-100 flex items-center justify-center text-indigo-600 shrink-0">
              <GitBranch className="h-5 w-5" />
            </div>
            <div>
              <div className="flex items-center gap-2">
                <h2 className="text-base font-semibold text-slate-900 tracking-tight">
                  Sync History & Catalog Versions
                </h2>
                <Badge variant="secondary" className="font-mono text-xs font-medium px-2 py-0 h-5">
                  {formatNumber(combinedTotal)}
                </Badge>
              </div>
              <p className="text-xs text-slate-500 mt-0.5">
                Unified audit timeline: inspect SKU diffs, resume interrupted batch checkpoints, or revert the catalog to any milestone.
              </p>
            </div>
          </div>

          <div className="flex flex-wrap items-center gap-2">
            {/* Status / Type Filter Badges */}
            {onStatusChange && (
              <div className="flex flex-wrap items-center gap-1 bg-slate-100 p-0.5 rounded-md">
                <Button
                  variant={status === '' || status === 'ALL' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onStatusChange('')}
                  className="h-7 px-2.5 text-xs font-medium"
                >
                  All ({formatNumber(combinedTotal)})
                </Button>
                <Button
                  variant={status === 'INTERRUPTED' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onStatusChange('INTERRUPTED')}
                  className="h-7 px-2.5 text-xs font-medium"
                >
                  Interrupted
                </Button>
                <Button
                  variant={status === 'COMPLETED' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onStatusChange('COMPLETED')}
                  className="h-7 px-2.5 text-xs font-medium"
                >
                  Completed
                </Button>
                <Button
                  variant={status === 'ROLLBACK' ? 'default' : 'ghost'}
                  size="sm"
                  onClick={() => onStatusChange('ROLLBACK')}
                  className="h-7 px-2.5 text-xs font-medium"
                >
                  Rollbacks
                </Button>
              </div>
            )}

            {onRefresh && (
              <Button
                variant="outline"
                size="sm"
                onClick={onRefresh}
                disabled={isLoadingVersions}
                className="h-8 text-xs font-medium border-slate-200 hover:bg-slate-50"
              >
                <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${isLoadingVersions ? 'animate-spin' : ''}`} />
                Refresh
              </Button>
            )}
          </div>
        </div>

        {/* Invariant Guarantee Banner */}
        <div className="mt-4 p-3 rounded-lg bg-slate-50 border border-slate-200/70 flex items-start gap-2.5 text-xs text-slate-600">
          <ShieldCheck className="h-4 w-4 text-emerald-600 mt-0.5 flex-shrink-0" />
          <div>
            <span className="font-semibold text-slate-800">Atomic Checkpoints & Tombstone Guarantee:</span> Mid-stream crashes resume from the exact uncommitted batch without duplicating prior work. Reverting to an earlier version restores historical prices and names via a forward-moving <span className="font-mono font-semibold">ROLLBACK</span> milestone while <span className="font-medium text-slate-900 underline underline-offset-2">never resurrecting</span> withdrawn items.
          </div>
        </div>
      </Card>

      {/* Interrupted Sessions Alert Banner */}
      {interruptedJobs.length > 0 && (
        <div className="p-4 rounded-xl border border-amber-300 bg-amber-50/80 text-amber-900 flex flex-col sm:flex-row items-start sm:items-center justify-between gap-3 shadow-xs">
          <div className="flex items-start sm:items-center gap-3">
            <AlertTriangle className="h-5 w-5 text-amber-600 shrink-0 mt-0.5 sm:mt-0" />
            <div>
              <h3 className="font-semibold text-xs sm:text-sm text-amber-900">
                {interruptedJobs.length} Incomplete / Interrupted Sync Session(s) Found
              </h3>
              <p className="text-xs text-amber-700 mt-0.5">
                Workloads halted mid-feed preserve exact batch checkpoints. Resume them anytime without re-processing earlier batches.
              </p>
            </div>
          </div>
          {onResumeJob && (
            <Button
              size="sm"
              onClick={() => onResumeJob(interruptedJobs[0].job_id)}
              disabled={isSyncing}
              className="h-8 px-3 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs shrink-0"
            >
              <RotateCw className="h-3.5 w-3.5 mr-1.5" />
              Resume Latest ({interruptedJobs[0].job_id})
            </Button>
          )}
        </div>
      )}

      {/* Unified History & Versions Table Card */}
      <Card className="border-slate-200/80 bg-white shadow-xs overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-slate-50/75">
              <TableRow className="border-slate-200">
                <TableHead className="w-[140px] text-xs font-semibold text-slate-700">Version / Session</TableHead>
                <TableHead className="w-[125px] text-xs font-semibold text-slate-700">Type & Status</TableHead>
                <TableHead className="min-w-[200px] text-xs font-semibold text-slate-700">Description & Checkpoints</TableHead>
                <TableHead className="w-20 text-right text-xs font-semibold text-slate-700">Added</TableHead>
                <TableHead className="w-20 text-right text-xs font-semibold text-slate-700">Updated</TableHead>
                <TableHead className="w-20 text-right text-xs font-semibold text-slate-700">Withdrawn</TableHead>
                <TableHead className="w-20 text-right text-xs font-semibold text-slate-700">Skipped</TableHead>
                <TableHead className="w-16 text-right text-xs font-semibold text-slate-700">DLQ</TableHead>
                <TableHead className="w-40 text-xs font-semibold text-slate-700">Timestamp</TableHead>
                <TableHead className="w-[210px] text-right text-xs font-semibold text-slate-700">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {unifiedEntries.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={10} className="h-36 text-center text-slate-400 text-xs">
                    <History className="h-8 w-8 text-slate-300 mx-auto mb-2" />
                    No sync sessions or catalog versions recorded yet. Run a workload scenario to populate the unified timeline.
                  </TableCell>
                </TableRow>
              ) : (
                unifiedEntries.map((entry) => {
                  const ver = entry.version;
                  const job = entry.job;
                  const isCurrentVersion = !!(ver && latestVersion && ver.version_id === latestVersion.version_id);
                  const isRevertedVer = entry.entryType === 'ROLLBACK' || !!(ver?.revert_of_version_id || ver?.reverted_version_id);
                  const revertTarget = ver?.revert_of_version_id || ver?.reverted_version_id;
                  const isInterrupted = entry.status === 'INTERRUPTED' || entry.status === 'FAILED';

                  return (
                    <TableRow
                      key={entry.id}
                      className={`hover:bg-slate-50/70 border-slate-200 text-xs ${
                        isCurrentVersion ? 'bg-indigo-50/25' : ''
                      }`}
                    >
                      {/* Version Number & Job ID */}
                      <TableCell className="py-3">
                        <div className="flex items-center gap-1.5 flex-wrap">
                          {ver ? (
                            <span className="font-mono text-xs font-bold text-slate-900 bg-slate-100 px-2 py-0.5 rounded">
                              v{ver.version_number}
                            </span>
                          ) : (
                            <span className="font-mono text-[11px] font-semibold text-slate-500 bg-slate-100 px-1.5 py-0.5 rounded">
                              SESSION
                            </span>
                          )}
                          {isCurrentVersion && (
                            <span
                              className="inline-flex items-center text-[10px] font-semibold text-indigo-700 bg-indigo-100/80 px-1.5 py-0.2 rounded"
                              title="Current live catalog version"
                            >
                              LIVE
                            </span>
                          )}
                        </div>
                        <div
                          className="font-mono text-[11px] text-slate-600 mt-1 truncate max-w-[130px]"
                          title={entry.id}
                        >
                          {entry.id}
                        </div>
                      </TableCell>

                      {/* Type & Status Badges */}
                      <TableCell className="py-3">
                        <div className="flex flex-col items-start gap-1">
                          <div className="flex items-center gap-1 flex-wrap">
                            {getVersionTypeBadge(entry.entryType)}
                            <Badge
                              variant={
                                entry.status === 'COMPLETED'
                                  ? 'success'
                                  : entry.status === 'RUNNING'
                                  ? 'info'
                                  : isInterrupted
                                  ? 'warning'
                                  : 'secondary'
                              }
                              className="text-[10px] uppercase font-bold"
                            >
                              {entry.status}
                            </Badge>
                          </div>
                          {isRevertedVer && revertTarget && (
                            <div
                              className="text-[10px] text-amber-700 font-mono flex items-center gap-0.5"
                              title={`Revert of ${revertTarget}`}
                            >
                              <ArrowRight className="h-2.5 w-2.5 inline" />
                              <span className="truncate max-w-[95px]">{revertTarget}</span>
                            </div>
                          )}
                        </div>
                      </TableCell>

                      {/* Description & Batch Checkpoints */}
                      <TableCell className="py-3">
                        <div className="font-medium text-slate-800 line-clamp-1" title={entry.description}>
                          {entry.description}
                        </div>
                        <div className="flex items-center gap-3 text-[11px] text-slate-500 mt-1">
                          {job ? (
                            <span className="font-mono text-slate-600">
                              Batches:{' '}
                              <strong>
                                {job.committed_batch_index !== undefined && job.committed_batch_index >= 0
                                  ? `${job.committed_batch_index + 1}/${job.total_batches}`
                                  : `0/${job.total_batches}`}
                              </strong>
                            </span>
                          ) : (
                            <span className="font-mono text-slate-500">Atomic Milestone</span>
                          )}
                          <span className="flex items-center gap-1 text-slate-400">
                            <User className="h-3 w-3" />
                            {entry.author}
                          </span>
                        </div>
                      </TableCell>

                      {/* Delta Metrics */}
                      <TableCell className="text-right font-mono text-xs text-emerald-600 font-bold">
                        +{entry.added}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-blue-600 font-bold">
                        ~{entry.updated}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-amber-600 font-bold">
                        -{entry.withdrawn}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-slate-500">
                        {entry.skipped}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-rose-600 font-bold">
                        {entry.deadLetter}
                      </TableCell>

                      {/* Timestamp */}
                      <TableCell className="text-slate-500 font-mono text-[11px]">
                        <div className="flex items-center gap-1">
                          <Clock className="h-3 w-3 text-slate-400 shrink-0" />
                          <span>{formatDateTime(entry.createdAt)}</span>
                        </div>
                      </TableCell>

                      {/* Unified Actions: Resume, Diff, Revert */}
                      <TableCell className="py-3 text-right">
                        <div className="flex items-center justify-end gap-1.5 flex-wrap">
                          {isInterrupted && job && onResumeJob && (
                            <Button
                              size="sm"
                              onClick={() => onResumeJob(job.job_id)}
                              disabled={isSyncing}
                              className="h-7 px-2.5 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs"
                              title="Resume this session from last committed batch checkpoint"
                            >
                              <RotateCw className="h-3 w-3 mr-1" />
                              Resume
                            </Button>
                          )}

                          {entry.status === 'RUNNING' && (
                            <Badge
                              variant="outline"
                              className="text-[10px] uppercase font-bold bg-blue-50 text-blue-700 border-blue-200 animate-pulse"
                            >
                              <RefreshCw className="h-3 w-3 mr-1 animate-spin text-blue-600" />
                              Active
                            </Badge>
                          )}

                          {ver && onFetchDiff && (
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => handleOpenDiff(ver)}
                              className="h-7 px-2 text-xs text-slate-700 hover:text-slate-900 border-slate-200 hover:bg-slate-100"
                              title="Inspect SKU pre-image & post-image changes"
                            >
                              <Eye className="h-3 w-3 mr-1" />
                              Diff
                            </Button>
                          )}

                          {ver && onRollback && entry.status !== 'RUNNING' && (
                            !isCurrentVersion ? (
                              <Button
                                variant="outline"
                                size="sm"
                                onClick={() => handlePromptRollback(ver)}
                                disabled={isRollingBack || isSyncing}
                                className="h-7 px-2 text-xs text-amber-700 border-amber-300 hover:bg-amber-50 hover:text-amber-800 font-medium"
                                title={`Rollback catalog to v${ver.version_number}`}
                              >
                                <RotateCcw className="h-3 w-3 mr-1" />
                                Revert to v{ver.version_number}
                              </Button>
                            ) : (
                              <span className="inline-flex items-center text-[11px] font-medium text-emerald-700 px-1.5 py-0.5">
                                <CheckCircle2 className="h-3.5 w-3.5 mr-1" /> Live
                              </span>
                            )
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </div>

        {/* Pagination Bar */}
        <div className="flex items-center justify-between p-4 border-t border-slate-200/80 text-xs text-slate-500 bg-slate-50/40">
          <div>
            Showing {unifiedEntries.length > 0 ? (page - 1) * limit + 1 : 0} to{' '}
            {Math.min(page * limit, combinedTotal)} of {formatNumber(combinedTotal)} entries
          </div>
          <div className="flex items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              onClick={() => onPageChange(page - 1)}
              disabled={page <= 1}
              className="h-8 px-2.5 border-slate-200"
            >
              <ChevronLeft className="h-3.5 w-3.5 mr-1" />
              <span>Prev</span>
            </Button>
            <span className="font-mono font-semibold text-slate-700 px-2">
              {page} / {totalPages}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onPageChange(page + 1)}
              disabled={page >= totalPages}
              className="h-8 px-2.5 border-slate-200"
            >
              <span>Next</span>
              <ChevronRight className="h-3.5 w-3.5 ml-1" />
            </Button>
          </div>
        </div>
      </Card>

      {/* Diff Inspection Modal */}
      <VersionDiffModal
        isOpen={isDiffModalOpen}
        onOpenChange={setIsDiffModalOpen}
        version={selectedVersion}
        changes={diffItems}
        isLoading={isLoadingDiff}
        onRollbackClick={onRollback ? handlePromptRollback : undefined}
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
              You are preparing to revert the live catalog back to baseline{' '}
              <strong className="text-slate-800">Version v{rollbackCandidate?.version_number}</strong> (
              {rollbackCandidate?.version_id}).
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 py-2 text-xs text-slate-600">
            <div className="p-3 rounded-lg bg-amber-50/70 border border-amber-200 text-amber-800 space-y-1.5">
              <div className="font-semibold flex items-center gap-1.5">
                <RotateCcw className="h-3.5 w-3.5 text-amber-700" />
                How Git-Revert Works:
              </div>
              <ul className="list-disc pl-4 space-y-1 text-[11px] text-amber-900/90">
                <li>
                  Creates a new forward-moving milestone version (<strong className="font-mono">ROLLBACK</strong>).
                </li>
                <li>
                  Inverse deltas restore product prices and names to match v{rollbackCandidate?.version_number}.
                </li>
                <li>
                  <strong className="text-rose-900">Withdrawn items remain withdrawn:</strong> Products withdrawn in
                  later versions are never resurrected.
                </li>
                <li>Live catalog lookups remain O(1) fast via database indexes.</li>
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
