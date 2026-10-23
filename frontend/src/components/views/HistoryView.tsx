import React from 'react';
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
import { SyncJob } from '../../types';
import { formatDateTime, formatNumber } from '../../lib/utils';
import {
  History,
  ChevronLeft,
  ChevronRight,
  AlertTriangle,
  RotateCw,
  CheckCircle2,
  RefreshCw,
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
}) => {
  const totalPages = Math.max(1, Math.ceil(total / limit));
  const interruptedJobs = jobs.filter((j) => j.status === 'INTERRUPTED' || j.status === 'FAILED');

  return (
    <div className="space-y-4">
      {/* View Header & Filter Bar */}
      <Card className="p-3.5 sm:p-4 border-slate-200/80 bg-white shadow-xs">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center gap-2">
            <History className="h-4 w-4 text-slate-700" />
            <h2 className="text-base font-semibold text-slate-900 tracking-tight">
              History
            </h2>
            <Badge variant="secondary" className="font-mono text-xs font-medium px-2 py-0 h-5">
              {formatNumber(total)}
            </Badge>
          </div>

          {/* Status Filter Badges */}
          {onStatusChange && (
            <div className="flex flex-wrap items-center gap-1 bg-slate-100 p-0.5 rounded-md">
              <Button
                variant={status === '' || status === 'ALL' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => onStatusChange('')}
                className="h-6 px-2 text-xs font-medium"
              >
                All
              </Button>
              <Button
                variant={status === 'INTERRUPTED' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => onStatusChange('INTERRUPTED')}
                className="h-6 px-2 text-xs font-medium"
              >
                Interrupted
              </Button>
              <Button
                variant={status === 'COMPLETED' ? 'default' : 'ghost'}
                size="sm"
                onClick={() => onStatusChange('COMPLETED')}
                className="h-6 px-2 text-xs font-medium"
              >
                Completed
              </Button>
            </div>
          )}
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
                Workloads halted mid-feed preserve exact SQLite batch checkpoints. You can safely resume them without re-processing earlier batches.
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

      {/* Audit Table Card */}
      <Card className="border-slate-200/80 bg-white shadow-xs overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-slate-50/75">
              <TableRow className="border-slate-200">
                <TableHead className="w-44 text-slate-700">Job ID</TableHead>
                <TableHead className="w-28 text-center text-slate-700">Status</TableHead>
                <TableHead className="w-28 text-center text-slate-700">Batches</TableHead>
                <TableHead className="w-24 text-right text-slate-700">Added</TableHead>
                <TableHead className="w-24 text-right text-slate-700">Updated</TableHead>
                <TableHead className="w-24 text-right text-slate-700">Withdrawn</TableHead>
                <TableHead className="w-24 text-right text-slate-700">Skipped</TableHead>
                <TableHead className="w-24 text-right text-slate-700">DLQ</TableHead>
                <TableHead className="w-44 text-slate-700">Execution Timestamp</TableHead>
                <TableHead className="w-28 text-center text-slate-700">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {jobs.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={10} className="h-36 text-center text-slate-400 text-xs">
                    No sync run sessions recorded yet. Run a workload scenario to populate audit logs.
                  </TableCell>
                </TableRow>
              ) : (
                jobs.map((j) => (
                  <TableRow
                    key={j.job_id}
                    className="hover:bg-slate-50/70 border-slate-200"
                  >
                    <TableCell className="font-mono font-semibold text-slate-900 text-xs">
                      {j.job_id}
                    </TableCell>
                    <TableCell className="text-center">
                      <Badge
                        variant={
                          j.status === 'COMPLETED'
                            ? 'success'
                            : j.status === 'RUNNING'
                            ? 'info'
                            : j.status === 'INTERRUPTED'
                            ? 'warning'
                            : 'secondary'
                        }
                        className="text-[10px] uppercase font-bold"
                      >
                        {j.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-center font-mono text-xs text-slate-700">
                      {j.committed_batch_index !== undefined && j.committed_batch_index >= 0
                        ? `${j.committed_batch_index + 1}/${j.total_batches}`
                        : j.total_batches}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-emerald-600 font-bold">
                      +{j.summary?.added || 0}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-blue-600 font-bold">
                      ~{j.summary?.updated || 0}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-amber-600 font-bold">
                      -{j.summary?.withdrawn || 0}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-slate-500">
                      {j.summary?.skipped || 0}
                    </TableCell>
                    <TableCell className="text-right font-mono text-xs text-rose-600 font-bold">
                      {j.summary?.dead_letter || 0}
                    </TableCell>
                    <TableCell className="text-slate-500 font-mono text-[11px]">
                      {formatDateTime(j.created_at)}
                    </TableCell>
                    <TableCell className="text-center">
                      {j.status === 'INTERRUPTED' || j.status === 'FAILED' ? (
                        <Button
                          size="sm"
                          onClick={() => onResumeJob?.(j.job_id)}
                          disabled={isSyncing}
                          className="h-7 px-2.5 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs"
                          title="Resume this session from checkpoint"
                        >
                          <RotateCw className="h-3 w-3 mr-1" />
                          Resume
                        </Button>
                      ) : j.status === 'RUNNING' ? (
                        <Badge
                          variant="outline"
                          className="text-[10px] uppercase font-bold bg-blue-50 text-blue-700 border-blue-200 animate-pulse"
                        >
                          <RefreshCw className="h-3 w-3 mr-1 animate-spin text-blue-600" />
                          Active
                        </Badge>
                      ) : (
                        <span className="inline-flex items-center text-xs font-medium text-emerald-600">
                          <CheckCircle2 className="h-3.5 w-3.5 mr-1" />
                          Done
                        </span>
                      )}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>

        {/* Pagination Bar */}
        <div className="flex items-center justify-between p-4 border-t border-slate-200/80 text-xs text-slate-500 bg-slate-50/40">
          <div>
            Showing {jobs.length > 0 ? (page - 1) * limit + 1 : 0} to{' '}
            {Math.min(page * limit, total)} of {formatNumber(total)} sessions
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
    </div>
  );
};
