import React from 'react';
import { Card } from './ui/card';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './ui/tabs';
import { Input } from './ui/input';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table';
import { CatalogProduct, DeadLetterIssue, SyncJob } from '../types';
import { formatCurrency, formatDateTime, formatNumber } from '../lib/utils';
import {
  Package,
  AlertTriangle,
  History,
  Search,
  ChevronLeft,
  ChevronRight,
  Wrench,
} from 'lucide-react';

interface ExplorerSectionProps {
  catalog: { products: CatalogProduct[]; total: number };
  dlq: { issues: DeadLetterIssue[]; total: number };
  history: { jobs: SyncJob[]; total: number };
  catalogPage: number;
  catalogLimit: number;
  catalogSearch: string;
  catalogStatus: string;
  dlqPage: number;
  dlqLimit: number;
  dlqReason: string;
  historyPage: number;
  historyLimit: number;
  activeTab?: string;
  onTabChange?: (tab: string) => void;
  onCatalogSearchChange: (search: string) => void;
  onCatalogStatusChange: (status: string) => void;
  onCatalogPageChange: (page: number) => void;
  onDlqReasonChange: (reason: string) => void;
  onDlqPageChange: (page: number) => void;
  onHistoryPageChange: (page: number) => void;
  onRemediateClick: (issue: DeadLetterIssue) => void;
}

export const ExplorerSection: React.FC<ExplorerSectionProps> = ({
  catalog,
  dlq,
  history,
  catalogPage,
  catalogLimit,
  catalogSearch,
  catalogStatus,
  dlqPage,
  dlqLimit,
  dlqReason,
  historyPage,
  historyLimit,
  activeTab = "catalog",
  onTabChange,
  onCatalogSearchChange,
  onCatalogStatusChange,
  onCatalogPageChange,
  onDlqReasonChange,
  onDlqPageChange,
  onHistoryPageChange,
  onRemediateClick,
}) => {
  const totalCatalogPages = Math.max(1, Math.ceil(catalog.total / catalogLimit));
  const totalDlqPages = Math.max(1, Math.ceil(dlq.total / dlqLimit));
  const totalHistoryPages = Math.max(1, Math.ceil(history.total / historyLimit));

  return (
    <Card className="border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs overflow-hidden transition-colors">
      <Tabs
        value={activeTab}
        onValueChange={(val) => onTabChange && onTabChange(val)}
        className="w-full"
      >
        <div className="border-b border-slate-200 dark:border-slate-800 px-4 pt-3 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <TabsList className="bg-slate-100 dark:bg-slate-800 p-1">
            <TabsTrigger value="catalog" className="flex items-center gap-2">
              <Package className="h-3.5 w-3.5" />
              <span>Live Catalog</span>
              <span className="ml-1 text-[11px] font-mono font-bold bg-slate-200/80 dark:bg-slate-700 px-1.5 py-0.2 rounded-full">
                {catalog.total}
              </span>
            </TabsTrigger>
            <TabsTrigger value="dlq" className="flex items-center gap-2">
              <AlertTriangle className="h-3.5 w-3.5 text-red-500" />
              <span>Attention Needed (DLQ)</span>
              {dlq.total > 0 && (
                <span className="ml-1 text-[11px] font-mono font-bold bg-red-100 dark:bg-red-950/80 text-red-700 dark:text-red-300 px-1.5 py-0.2 rounded-full">
                  {dlq.total}
                </span>
              )}
            </TabsTrigger>
            <TabsTrigger value="history" className="flex items-center gap-2">
              <History className="h-3.5 w-3.5" />
              <span>Sync Run History</span>
              <span className="ml-1 text-[11px] font-mono font-bold bg-slate-200/80 dark:bg-slate-700 px-1.5 py-0.2 rounded-full">
                {history.total}
              </span>
            </TabsTrigger>
          </TabsList>
        </div>

        {/* Tab 1: Live Catalog */}
        <TabsContent value="catalog" className="p-4 sm:p-5 m-0 space-y-4">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div className="relative flex-1 max-w-sm">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-slate-400 dark:text-slate-500" />
              <Input
                placeholder="Search by SKU or Product Name..."
                value={catalogSearch}
                onChange={(e) => onCatalogSearchChange(e.target.value)}
                className="pl-8 h-9 text-xs bg-white dark:bg-slate-800 border-slate-200 dark:border-slate-700 text-slate-900 dark:text-slate-100"
              />
            </div>

            <div className="flex items-center gap-2">
              <select
                value={catalogStatus}
                onChange={(e) => onCatalogStatusChange(e.target.value)}
                className="h-9 rounded-md border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-2.5 py-1 text-xs text-slate-700 dark:text-slate-300 shadow-xs focus:outline-none focus:ring-1 focus:ring-indigo-500"
              >
                <option value="">All Statuses</option>
                <option value="ACTIVE">Active only</option>
                <option value="WITHDRAWN">Withdrawn only</option>
              </select>
            </div>
          </div>

          <div className="rounded-md border border-slate-200 dark:border-slate-800 overflow-hidden">
            <Table>
              <TableHeader className="bg-slate-50/75 dark:bg-slate-800/60">
                <TableRow className="border-slate-200 dark:border-slate-800">
                  <TableHead className="w-36 text-slate-700 dark:text-slate-300">SKU</TableHead>
                  <TableHead className="text-slate-700 dark:text-slate-300">Product Name</TableHead>
                  <TableHead className="w-28 text-right text-slate-700 dark:text-slate-300">Unit Price</TableHead>
                  <TableHead className="w-28 text-center text-slate-700 dark:text-slate-300">Status</TableHead>
                  <TableHead className="w-44 text-slate-700 dark:text-slate-300">Last Updated</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {catalog.products.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="h-28 text-center text-slate-400 dark:text-slate-500 text-xs">
                      No products found matching filter criteria.
                    </TableCell>
                  </TableRow>
                ) : (
                  catalog.products.map((p) => (
                    <TableRow key={p.sku} className="hover:bg-slate-50/70 dark:hover:bg-slate-800/50 border-slate-200 dark:border-slate-800">
                      <TableCell className="font-mono font-semibold text-slate-900 dark:text-slate-100">{p.sku}</TableCell>
                      <TableCell className="font-medium text-slate-800 dark:text-slate-200">{p.name}</TableCell>
                      <TableCell className="text-right font-mono font-bold text-slate-900 dark:text-slate-100">
                        {formatCurrency(p.price)}
                      </TableCell>
                      <TableCell className="text-center">
                        <Badge
                          variant={p.status === 'ACTIVE' ? 'success' : 'warning'}
                          className="text-[10px] uppercase font-bold"
                        >
                          {p.status}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-slate-500 dark:text-slate-400 font-mono text-[11px]">
                        {formatDateTime(p.updated_at || p.last_updated_at || p.supplier_timestamp)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          {/* Catalog Pagination */}
          <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400 pt-2">
            <div>
              Showing {catalog.products.length > 0 ? (catalogPage - 1) * catalogLimit + 1 : 0} to{' '}
              {Math.min(catalogPage * catalogLimit, catalog.total)} of {formatNumber(catalog.total)} records
            </div>
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="sm"
                onClick={() => onCatalogPageChange(catalogPage - 1)}
                disabled={catalogPage <= 1}
                className="h-8 px-2 border-slate-300 dark:border-slate-700"
              >
                <ChevronLeft className="h-3.5 w-3.5 mr-1" />
                <span>Prev</span>
              </Button>
              <span className="font-mono font-semibold text-slate-700 dark:text-slate-300 px-2">
                {catalogPage} / {totalCatalogPages}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onCatalogPageChange(catalogPage + 1)}
                disabled={catalogPage >= totalCatalogPages}
                className="h-8 px-2 border-slate-300 dark:border-slate-700"
              >
                <span>Next</span>
                <ChevronRight className="h-3.5 w-3.5 ml-1" />
              </Button>
            </div>
          </div>
        </TabsContent>

        {/* Tab 2: Quarantined DLQ */}
        <TabsContent value="dlq" className="p-4 sm:p-5 m-0 space-y-4">
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
            <div className="flex items-center gap-2">
              <span className="text-xs text-slate-500 dark:text-slate-400 font-medium">Filter by Defect:</span>
              <select
                value={dlqReason}
                onChange={(e) => onDlqReasonChange(e.target.value)}
                className="h-9 rounded-md border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-800 px-2.5 py-1 text-xs text-slate-700 dark:text-slate-300 shadow-xs focus:outline-none focus:ring-1 focus:ring-indigo-500"
              >
                <option value="">All Defect Reasons</option>
                <option value="NEGATIVE_OR_ZERO_PRICE">Invalid Price (≤ 0)</option>
                <option value="EMPTY_SKU">Empty / Missing SKU</option>
                <option value="ZERO_OR_MISSING_TIMESTAMP">Missing Timestamp</option>
                <option value="CORRUPTED_JSON">Malformed Schema</option>
              </select>
            </div>
          </div>

          <div className="rounded-md border border-slate-200 dark:border-slate-800 overflow-hidden">
            <Table>
              <TableHeader className="bg-slate-50/75 dark:bg-slate-800/60">
                <TableRow className="border-slate-200 dark:border-slate-800">
                  <TableHead className="w-24 text-slate-700 dark:text-slate-300">Issue ID</TableHead>
                  <TableHead className="w-32 text-slate-700 dark:text-slate-300">SKU</TableHead>
                  <TableHead className="text-slate-700 dark:text-slate-300">Quarantine Reason & Defect Detail</TableHead>
                  <TableHead className="w-40 text-slate-700 dark:text-slate-300">Detected At</TableHead>
                  <TableHead className="w-32 text-center text-slate-700 dark:text-slate-300">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {dlq.issues.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="h-28 text-center text-slate-400 dark:text-slate-500 text-xs">
                      Zero quarantined defects detected. Pipeline is completely clean!
                    </TableCell>
                  </TableRow>
                ) : (
                  dlq.issues.map((iss) => {
                    let parsed: any = {};
                    try {
                      parsed = JSON.parse(iss.raw_payload || '{}');
                    } catch {}
                    return (
                      <TableRow key={iss.issue_id} className="hover:bg-slate-50/70 dark:hover:bg-slate-800/50 border-slate-200 dark:border-slate-800">
                        <TableCell className="font-mono text-xs text-slate-500 dark:text-slate-400">
                          #{(iss.issue_id || '').substring(0, 8)}
                        </TableCell>
                        <TableCell className="font-mono font-bold text-slate-900 dark:text-slate-100">
                          {iss.sku || <span className="text-red-500 italic">EMPTY</span>}
                        </TableCell>
                        <TableCell>
                          <Badge variant="destructive" className="text-[10px] uppercase font-bold mr-2">
                            {iss.reason}
                          </Badge>
                          <span className="text-xs text-slate-600 dark:text-slate-300">
                            {parsed.name ? `"${parsed.name}" (Price: $${parsed.price})` : 'Malformed Record'}
                          </span>
                        </TableCell>
                        <TableCell className="text-slate-500 dark:text-slate-400 font-mono text-[11px]">
                          {formatDateTime(iss.created_at)}
                        </TableCell>
                        <TableCell className="text-center">
                          <Button
                            variant="outline"
                            size="xs"
                            onClick={() => onRemediateClick(iss)}
                            className="text-indigo-600 dark:text-indigo-400 hover:text-indigo-700 hover:bg-indigo-50 dark:hover:bg-indigo-950/60 border-indigo-200 dark:border-indigo-800 font-medium"
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

          {/* DLQ Pagination */}
          <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400 pt-2">
            <div>
              Showing {dlq.issues.length > 0 ? (dlqPage - 1) * dlqLimit + 1 : 0} to{' '}
              {Math.min(dlqPage * dlqLimit, dlq.total)} of {formatNumber(dlq.total)} quarantined defects
            </div>
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="sm"
                onClick={() => onDlqPageChange(dlqPage - 1)}
                disabled={dlqPage <= 1}
                className="h-8 px-2 border-slate-300 dark:border-slate-700"
              >
                <ChevronLeft className="h-3.5 w-3.5 mr-1" />
                <span>Prev</span>
              </Button>
              <span className="font-mono font-semibold text-slate-700 dark:text-slate-300 px-2">
                {dlqPage} / {totalDlqPages}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onDlqPageChange(dlqPage + 1)}
                disabled={dlqPage >= totalDlqPages}
                className="h-8 px-2 border-slate-300 dark:border-slate-700"
              >
                <span>Next</span>
                <ChevronRight className="h-3.5 w-3.5 ml-1" />
              </Button>
            </div>
          </div>
        </TabsContent>

        {/* Tab 3: History */}
        <TabsContent value="history" className="p-4 sm:p-5 m-0 space-y-4">
          <div className="rounded-md border border-slate-200 dark:border-slate-800 overflow-hidden">
            <Table>
              <TableHeader className="bg-slate-50/75 dark:bg-slate-800/60">
                <TableRow className="border-slate-200 dark:border-slate-800">
                  <TableHead className="w-36 text-slate-700 dark:text-slate-300">Job ID</TableHead>
                  <TableHead className="w-28 text-center text-slate-700 dark:text-slate-300">Status</TableHead>
                  <TableHead className="w-28 text-center text-slate-700 dark:text-slate-300">Batches</TableHead>
                  <TableHead className="w-24 text-right text-slate-700 dark:text-slate-300">Added</TableHead>
                  <TableHead className="w-24 text-right text-slate-700 dark:text-slate-300">Updated</TableHead>
                  <TableHead className="w-24 text-right text-slate-700 dark:text-slate-300">Withdrawn</TableHead>
                  <TableHead className="w-24 text-right text-slate-700 dark:text-slate-300">Skipped</TableHead>
                  <TableHead className="w-24 text-right text-slate-700 dark:text-slate-300">DLQ</TableHead>
                  <TableHead className="w-40 text-slate-700 dark:text-slate-300">Started At</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {history.jobs.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="h-28 text-center text-slate-400 dark:text-slate-500 text-xs">
                      No synchronization jobs recorded yet.
                    </TableCell>
                  </TableRow>
                ) : (
                  history.jobs.map((j) => (
                    <TableRow key={j.job_id} className="hover:bg-slate-50/70 dark:hover:bg-slate-800/50 border-slate-200 dark:border-slate-800">
                      <TableCell className="font-mono font-semibold text-slate-900 dark:text-slate-100 text-xs">
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
                      <TableCell className="text-center font-mono text-xs text-slate-700 dark:text-slate-300">
                        {j.committed_batch_index !== undefined ? `${j.committed_batch_index + 1}/${j.total_batches}` : j.total_batches}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-emerald-600 dark:text-emerald-400 font-bold">
                        +{j.summary?.added || 0}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-blue-600 dark:text-blue-400 font-bold">
                        ~{j.summary?.updated || 0}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-amber-600 dark:text-amber-400 font-bold">
                        -{j.summary?.withdrawn || 0}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-slate-500 dark:text-slate-400">
                        {j.summary?.skipped || 0}
                      </TableCell>
                      <TableCell className="text-right font-mono text-xs text-red-600 dark:text-red-400 font-bold">
                        {j.summary?.dead_letter || 0}
                      </TableCell>
                      <TableCell className="text-slate-500 dark:text-slate-400 font-mono text-[11px]">
                        {formatDateTime(j.created_at)}
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>

          {/* History Pagination */}
          <div className="flex items-center justify-between text-xs text-slate-500 dark:text-slate-400 pt-2">
            <div>
              Showing {history.jobs.length > 0 ? (historyPage - 1) * historyLimit + 1 : 0} to{' '}
              {Math.min(historyPage * historyLimit, history.total)} of {formatNumber(history.total)} jobs
            </div>
            <div className="flex items-center gap-1.5">
              <Button
                variant="outline"
                size="sm"
                onClick={() => onHistoryPageChange(historyPage - 1)}
                disabled={historyPage <= 1}
                className="h-8 px-2 border-slate-300 dark:border-slate-700"
              >
                <ChevronLeft className="h-3.5 w-3.5 mr-1" />
                <span>Prev</span>
              </Button>
              <span className="font-mono font-semibold text-slate-700 dark:text-slate-300 px-2">
                {historyPage} / {totalHistoryPages}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => onHistoryPageChange(historyPage + 1)}
                disabled={historyPage >= totalHistoryPages}
                className="h-8 px-2 border-slate-300 dark:border-slate-700"
              >
                <span>Next</span>
                <ChevronRight className="h-3.5 w-3.5 ml-1" />
              </Button>
            </div>
          </div>
        </TabsContent>
      </Tabs>
    </Card>
  );
};
