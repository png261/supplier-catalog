import React, { useEffect, useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table';
import { Badge } from './ui/badge';
import { Button } from './ui/button';
import { CatalogProduct, DeadLetterIssue } from '../types';
import { fetchCatalog, fetchDLQ } from '../services/api';
import { formatCurrency, formatDateTime, formatNumber } from '../lib/utils';
import {
  Layers,
  Wrench,
  ArrowRight,
  CopyCheck,
  Loader2,
  ShieldCheck,
} from 'lucide-react';

interface MetricDrilldownModalProps {
  category: 'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter' | null;
  metricCount?: number;
  onOpenChange: (open: boolean) => void;
  catalogProducts: CatalogProduct[];
  dlqIssues: DeadLetterIssue[];
  onRemediateIssue?: (issue: DeadLetterIssue) => void;
  onNavigateToCatalog?: (statusFilter: string) => void;
  onNavigateToTab?: (tab: 'catalog' | 'attention' | 'history') => void;
}

export const MetricDrilldownModal: React.FC<MetricDrilldownModalProps> = ({
  category,
  metricCount = 0,
  onOpenChange,
  catalogProducts,
  dlqIssues,
  onRemediateIssue,
  onNavigateToCatalog,
  onNavigateToTab,
}) => {
  const [fetchedProducts, setFetchedProducts] = useState<CatalogProduct[]>([]);
  const [fetchedIssues, setFetchedIssues] = useState<DeadLetterIssue[]>([]);
  const [totalMatched, setTotalMatched] = useState<number>(0);
  const [isLoading, setIsLoading] = useState<boolean>(false);

  useEffect(() => {
    if (!category) return;

    let cancelled = false;
    const loadAccurateSlice = async () => {
      if (category === 'skipped') {
        setTotalMatched(metricCount);
        return;
      }

      setIsLoading(true);
      try {
        if (category === 'dead_letter') {
          const res = await fetchDLQ(25, 0, '');
          if (!cancelled) {
            setFetchedIssues(res.issues || dlqIssues);
            setTotalMatched(res.total ?? metricCount);
          }
        } else {
          const statusFilter = category === 'withdrawn' ? 'WITHDRAWN' : 'ACTIVE';
          const res = await fetchCatalog(25, 0, '', statusFilter);
          if (!cancelled) {
            setFetchedProducts(res.products || []);
            setTotalMatched(res.total ?? metricCount);
          }
        }
      } catch {
        if (!cancelled) {
          if (category === 'dead_letter') {
            setFetchedIssues(dlqIssues);
          } else if (category === 'withdrawn') {
            setFetchedProducts(catalogProducts.filter((p) => p.status === 'WITHDRAWN'));
          } else {
            setFetchedProducts(catalogProducts.filter((p) => p.status === 'ACTIVE'));
          }
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    };

    loadAccurateSlice();
    return () => {
      cancelled = true;
    };
  }, [category, metricCount]);

  if (!category) return null;

  const getTitle = () => {
    switch (category) {
      case 'added':
        return 'Products Added to Catalog';
      case 'updated':
        return 'Price & Attribute Updated Products';
      case 'withdrawn':
        return 'Withdrawn (Tombstoned) Products';
      case 'skipped':
        return 'Idempotent & Out-of-Order Skipped Records';
      case 'dead_letter':
        return 'Quarantined Dead-Letter Records (DLQ)';
    }
  };

  const getDescription = () => {
    switch (category) {
      case 'added':
        return 'Active products newly ingested into the catalog from supplier feed batches.';
      case 'updated':
        return 'Active catalog products that accepted newer supplier timestamps for price or name updates.';
      case 'withdrawn':
        return 'Items marked as WITHDRAWN (tombstoned). Protected by the Tombstone Invariant so they can never be accidentally resurrected by stale feeds or rollbacks.';
      case 'skipped':
        return 'Feed events that were safely ignored without mutating the database because their supplier timestamp was older than or identical to the current catalog state.';
      case 'dead_letter':
        return 'Records isolated during ingestion due to validation failures (non-positive price, empty SKU, or malformed JSON).';
    }
  };

  return (
    <Dialog open={!!category} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl sm:max-w-3xl p-0 overflow-hidden bg-white">
        <DialogHeader className="p-6 pb-4 border-b border-slate-100 bg-slate-50/50">
          <div className="flex items-center justify-between gap-3">
            <DialogTitle className="text-base font-bold text-slate-900 flex items-center gap-2">
              <Layers className="h-4 w-4 text-slate-700" />
              <span>{getTitle()}</span>
              <Badge variant="secondary" className="font-mono text-xs px-2 py-0 h-5">
                {formatNumber(metricCount || totalMatched)}
              </Badge>
            </DialogTitle>
          </div>
          <DialogDescription className="text-xs text-slate-500 pt-1">
            {getDescription()}
          </DialogDescription>
        </DialogHeader>

        <div className="p-6 pt-4">
          {isLoading ? (
            <div className="h-48 flex flex-col items-center justify-center text-xs text-slate-400 gap-2">
              <Loader2 className="h-5 w-5 animate-spin text-slate-600" />
              <span>Loading matching records...</span>
            </div>
          ) : category === 'skipped' ? (
            <div className="rounded-xl border border-slate-200/80 bg-slate-50/60 p-5 space-y-3 text-xs text-slate-600">
              <div className="flex items-start gap-3">
                <div className="h-9 w-9 rounded-lg bg-white border border-slate-200 flex items-center justify-center text-slate-700 shrink-0">
                  <CopyCheck className="h-4 w-4" />
                </div>
                <div className="space-y-1">
                  <h4 className="font-semibold text-slate-900 text-sm">
                    {formatNumber(metricCount)} Duplicate / Out-of-Order Events Ignored
                  </h4>
                  <p className="text-slate-500 leading-relaxed">
                    Skipped events are not stored as separate rows in the live catalog because they represent redundant or stale supplier transmissions.
                  </p>
                </div>
              </div>
              <div className="p-3.5 rounded-lg bg-white border border-slate-200/80 space-y-1.5">
                <div className="font-semibold text-slate-800 flex items-center gap-1.5">
                  <ShieldCheck className="h-3.5 w-3.5 text-emerald-600" />
                  How Idempotency & Timestamp Ordering Works:
                </div>
                <ul className="list-disc pl-4 space-y-1 text-[11px] text-slate-600">
                  <li>
                    <strong>Exact Duplicates:</strong> Retried batches or duplicate SKU records with identical <code className="font-mono bg-slate-100 px-1 rounded">supplier_timestamp</code> are skipped in $O(1)$ without creating noisy version deltas.
                  </li>
                  <li>
                    <strong>Out-of-Order Protection:</strong> If an older message arrives late (<code className="font-mono bg-slate-100 px-1 rounded">incoming.timestamp &lt; current.timestamp</code>), the engine preserves the newer catalog state and increments the skipped counter.
                  </li>
                </ul>
              </div>
            </div>
          ) : (
            <div className="rounded-lg border border-slate-200 overflow-hidden max-h-[52vh] overflow-y-auto">
              {category === 'dead_letter' ? (
                <Table>
                  <TableHeader className="bg-slate-50/90 sticky top-0 z-10">
                    <TableRow>
                      <TableHead className="w-24 text-xs">Issue ID</TableHead>
                      <TableHead className="w-32 text-xs">SKU</TableHead>
                      <TableHead className="text-xs">Defect Reason & Payload</TableHead>
                      <TableHead className="w-36 text-xs">Detected At</TableHead>
                      {onRemediateIssue && <TableHead className="w-28 text-center text-xs">Action</TableHead>}
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {fetchedIssues.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={onRemediateIssue ? 5 : 4} className="h-28 text-center text-slate-400 text-xs">
                          No quarantined items currently in the Dead-Letter Queue.
                        </TableCell>
                      </TableRow>
                    ) : (
                      fetchedIssues.map((iss) => {
                        let parsed: any = {};
                        try {
                          parsed = JSON.parse(iss.raw_payload || '{}');
                        } catch {}
                        return (
                          <TableRow key={iss.issue_id} className="text-xs hover:bg-slate-50/70">
                            <TableCell className="font-mono text-xs text-slate-500">
                              #{(iss.issue_id || '').substring(0, 8)}
                            </TableCell>
                            <TableCell className="font-mono font-bold text-slate-900">
                              {iss.sku || <span className="text-rose-500 italic">EMPTY</span>}
                            </TableCell>
                            <TableCell>
                              <div className="flex items-center gap-2">
                                <Badge variant="destructive" className="text-[10px] uppercase font-bold shrink-0">
                                  {iss.reason}
                                </Badge>
                                <span className="text-xs text-slate-600 truncate max-w-[240px]">
                                  {parsed.name ? `${parsed.name} ($${parsed.price})` : iss.raw_payload}
                                </span>
                              </div>
                            </TableCell>
                            <TableCell className="text-slate-500 font-mono text-[11px]">
                              {formatDateTime(iss.created_at)}
                            </TableCell>
                            {onRemediateIssue && (
                              <TableCell className="text-center">
                                <Button
                                  variant="outline"
                                  size="xs"
                                  onClick={() => {
                                    onOpenChange(false);
                                    onRemediateIssue(iss);
                                  }}
                                  className="text-indigo-600 hover:text-indigo-700 hover:bg-indigo-50 border-indigo-200 font-semibold"
                                >
                                  <Wrench className="h-3 w-3 mr-1" />
                                  Fix
                                </Button>
                              </TableCell>
                            )}
                          </TableRow>
                        );
                      })
                    )}
                  </TableBody>
                </Table>
              ) : (
                <Table>
                  <TableHeader className="bg-slate-50/90 sticky top-0 z-10">
                    <TableRow>
                      <TableHead className="w-36 text-xs">SKU</TableHead>
                      <TableHead className="text-xs">Product Name</TableHead>
                      <TableHead className="w-28 text-right text-xs">Price</TableHead>
                      <TableHead className="w-24 text-center text-xs">Status</TableHead>
                      <TableHead className="w-40 text-xs">Last Updated</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {fetchedProducts.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} className="h-28 text-center text-slate-400 text-xs">
                          No matching products found in catalog.
                        </TableCell>
                      </TableRow>
                    ) : (
                      fetchedProducts.map((p) => (
                        <TableRow key={p.sku} className="text-xs hover:bg-slate-50/70">
                          <TableCell className="font-mono font-bold text-slate-900">{p.sku}</TableCell>
                          <TableCell className="text-slate-800 font-medium text-xs">{p.name}</TableCell>
                          <TableCell className="text-right font-mono font-bold text-slate-900">
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
                          <TableCell className="text-slate-500 font-mono text-[11px]">
                            {formatDateTime(p.updated_at || p.last_updated_at || p.supplier_timestamp)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              )}
            </div>
          )}
        </div>

        <DialogFooter className="p-4 border-t border-slate-100 bg-slate-50/50 flex sm:justify-between items-center gap-2">
          <span className="text-[11px] text-slate-500">
            {category === 'skipped'
              ? 'Audit logs record per-batch skipped counts in Sync History'
              : `Showing preview of up to 25 matching records`}
          </span>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)} className="h-8 text-xs">
              Close
            </Button>
            {category === 'dead_letter' && onNavigateToTab && (
              <Button
                size="sm"
                onClick={() => {
                  onOpenChange(false);
                  onNavigateToTab('attention');
                }}
                className="h-8 text-xs bg-slate-900 hover:bg-slate-800 text-white"
              >
                Open Quarantine Workspace
                <ArrowRight className="h-3.5 w-3.5 ml-1" />
              </Button>
            )}
            {category === 'skipped' && onNavigateToTab && (
              <Button
                size="sm"
                onClick={() => {
                  onOpenChange(false);
                  onNavigateToTab('history');
                }}
                className="h-8 text-xs bg-slate-900 hover:bg-slate-800 text-white"
              >
                View Sync Run History
                <ArrowRight className="h-3.5 w-3.5 ml-1" />
              </Button>
            )}
            {(category === 'added' || category === 'updated' || category === 'withdrawn') &&
              onNavigateToCatalog && (
                <Button
                  size="sm"
                  onClick={() => {
                    onOpenChange(false);
                    onNavigateToCatalog(category === 'withdrawn' ? 'WITHDRAWN' : 'ACTIVE');
                  }}
                  className="h-8 text-xs bg-slate-900 hover:bg-slate-800 text-white"
                >
                  Explore in Catalog ({category === 'withdrawn' ? 'WITHDRAWN' : 'ACTIVE'})
                  <ArrowRight className="h-3.5 w-3.5 ml-1" />
                </Button>
              )}
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
