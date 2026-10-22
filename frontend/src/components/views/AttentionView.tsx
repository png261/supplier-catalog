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
import { DeadLetterIssue } from '../../types';
import { formatDateTime, formatNumber } from '../../lib/utils';
import { AlertTriangle, Wrench, ChevronLeft, ChevronRight, ShieldAlert, CheckCircle2 } from 'lucide-react';

interface AttentionViewProps {
  issues: DeadLetterIssue[];
  total: number;
  page: number;
  limit: number;
  reason: string;
  onReasonChange: (reason: string) => void;
  onPageChange: (page: number) => void;
  onRemediateClick: (issue: DeadLetterIssue) => void;
}

export const AttentionView: React.FC<AttentionViewProps> = ({
  issues,
  total,
  page,
  limit,
  reason,
  onReasonChange,
  onPageChange,
  onRemediateClick,
}) => {
  const totalPages = Math.max(1, Math.ceil(total / limit));

  return (
    <div className="space-y-4">
      {/* View Header & Guidance Banner */}
      <Card className="p-4 sm:p-5 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs space-y-3">
        <div className="flex flex-col md:flex-row md:items-center md:justify-between gap-4">
          <div className="space-y-1">
            <div className="flex items-center gap-2">
              <ShieldAlert className="h-5 w-5 text-red-600 dark:text-red-400" />
              <h2 className="text-base sm:text-lg font-bold text-slate-900 dark:text-slate-100 tracking-tight">
                Quarantined Attention (Dead-Letter Queue)
              </h2>
              {total > 0 ? (
                <Badge variant="destructive" className="font-mono text-xs font-bold">
                  {formatNumber(total)} defects require staff remediation
                </Badge>
              ) : (
                <Badge variant="success" className="font-mono text-xs font-bold">
                  Zero defects detected
                </Badge>
              )}
            </div>
            <p className="text-xs text-slate-500 dark:text-slate-400">
              Invariant D enforces dead-letter isolation. Defective records are quarantined to prevent catalog corruption.
            </p>
          </div>

          {/* Reason Filter */}
          <div className="flex items-center gap-2">
            <span className="text-xs text-slate-500 dark:text-slate-400 font-medium">Defect Type:</span>
            <select
              value={reason}
              onChange={(e) => onReasonChange(e.target.value)}
              className="h-9 rounded-md border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-3 py-1 text-xs text-slate-700 dark:text-slate-300 font-medium shadow-xs focus:outline-none focus:ring-1 focus:ring-indigo-500"
            >
              <option value="">All Defect Reasons</option>
              <option value="NEGATIVE_OR_ZERO_PRICE">Invalid Price (≤ 0)</option>
              <option value="EMPTY_SKU">Empty / Missing SKU</option>
              <option value="ZERO_OR_MISSING_TIMESTAMP">Missing Timestamp</option>
              <option value="CORRUPTED_JSON">Malformed Schema</option>
            </select>
          </div>
        </div>

        {/* Informational Strip */}
        <div className="p-3 rounded-lg bg-amber-50/70 dark:bg-amber-950/30 border border-amber-200/80 dark:border-amber-900/50 text-xs text-amber-900 dark:text-amber-200 flex items-start gap-2.5">
          <AlertTriangle className="h-4 w-4 text-amber-600 dark:text-amber-400 shrink-0 mt-0.5" />
          <div className="leading-relaxed">
            Staff can inspect the raw supplier payload, rectify defective pricing or missing identifiers, and click <strong>Save & Import to Catalog</strong> to safely remediate the item without restarting the feed.
          </div>
        </div>
      </Card>

      {/* Issues Table Card */}
      <Card className="border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-slate-50/75 dark:bg-slate-800/60">
              <TableRow className="border-slate-200 dark:border-slate-800">
                <TableHead className="w-28 text-slate-700 dark:text-slate-300">Issue ID</TableHead>
                <TableHead className="w-36 text-slate-700 dark:text-slate-300">SKU</TableHead>
                <TableHead className="text-slate-700 dark:text-slate-300">Quarantine Reason & Defect Preview</TableHead>
                <TableHead className="w-44 text-slate-700 dark:text-slate-300">Detected Timestamp</TableHead>
                <TableHead className="w-36 text-center text-slate-700 dark:text-slate-300">Remediation</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {issues.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="h-36 text-center text-slate-400 dark:text-slate-500 text-xs">
                    <CheckCircle2 className="h-6 w-6 text-emerald-500 mx-auto mb-1.5 opacity-80" />
                    Zero quarantined defects. All supplier records ingested cleanly!
                  </TableCell>
                </TableRow>
              ) : (
                issues.map((iss) => {
                  let parsed: any = {};
                  try {
                    parsed = JSON.parse(iss.raw_payload || '{}');
                  } catch {}

                  return (
                    <TableRow
                      key={iss.issue_id}
                      className="hover:bg-slate-50/70 dark:hover:bg-slate-800/50 border-slate-200 dark:border-slate-800"
                    >
                      <TableCell className="font-mono text-xs text-slate-500 dark:text-slate-400">
                        #{(iss.issue_id || '').substring(0, 8)}
                      </TableCell>
                      <TableCell className="font-mono font-bold text-slate-900 dark:text-slate-100">
                        {iss.sku || <span className="text-red-500 italic">EMPTY SKU</span>}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Badge variant="destructive" className="text-[10px] uppercase font-bold shrink-0">
                            {iss.reason}
                          </Badge>
                          <span className="text-xs text-slate-600 dark:text-slate-300 truncate max-w-md">
                            {parsed.name ? `"${parsed.name}" (Price: $${parsed.price})` : 'Malformed Record'}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-slate-500 dark:text-slate-400 font-mono text-[11px]">
                        {formatDateTime(iss.created_at)}
                      </TableCell>
                      <TableCell className="text-center">
                        <Button
                          variant="outline"
                          size="xs"
                          onClick={() => onRemediateClick(iss)}
                          className="text-indigo-600 dark:text-indigo-400 hover:text-indigo-700 hover:bg-indigo-50 dark:hover:bg-indigo-950/60 border-indigo-200 dark:border-indigo-800 font-semibold"
                        >
                          <Wrench className="h-3 w-3 mr-1" />
                          <span>Review & Fix</span>
                        </Button>
                      </TableCell>
                    </TableRow>
                  );
                })
              )}
            </TableBody>
          </Table>
        </div>

        {/* Pagination Bar */}
        <div className="flex items-center justify-between p-4 border-t border-slate-200 dark:border-slate-800 text-xs text-slate-500 dark:text-slate-400">
          <div>
            Showing {issues.length > 0 ? (page - 1) * limit + 1 : 0} to{' '}
            {Math.min(page * limit, total)} of {formatNumber(total)} quarantined records
          </div>
          <div className="flex items-center gap-1.5">
            <Button
              variant="outline"
              size="sm"
              onClick={() => onPageChange(page - 1)}
              disabled={page <= 1}
              className="h-8 px-2.5 border-slate-300 dark:border-slate-700"
            >
              <ChevronLeft className="h-3.5 w-3.5 mr-1" />
              <span>Prev</span>
            </Button>
            <span className="font-mono font-semibold text-slate-700 dark:text-slate-300 px-2">
              {page} / {totalPages}
            </span>
            <Button
              variant="outline"
              size="sm"
              onClick={() => onPageChange(page + 1)}
              disabled={page >= totalPages}
              className="h-8 px-2.5 border-slate-300 dark:border-slate-700"
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
