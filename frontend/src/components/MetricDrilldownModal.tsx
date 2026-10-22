import React from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
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
import { CatalogProduct, DeadLetterIssue } from '../types';
import { formatCurrency, formatDateTime } from '../lib/utils';
import { Layers } from 'lucide-react';

interface MetricDrilldownModalProps {
  category: 'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter' | null;
  onOpenChange: (open: boolean) => void;
  catalogProducts: CatalogProduct[];
  dlqIssues: DeadLetterIssue[];
}

export const MetricDrilldownModal: React.FC<MetricDrilldownModalProps> = ({
  category,
  onOpenChange,
  catalogProducts,
  dlqIssues,
}) => {
  if (!category) return null;

  const getTitle = () => {
    switch (category) {
      case 'added':
        return 'Products Added to Catalog';
      case 'updated':
        return 'Price / Name Updated Products';
      case 'withdrawn':
        return 'Withdrawn Products';
      case 'skipped':
        return 'Duplicate & Out-of-Order Records';
      case 'dead_letter':
        return 'Quarantined Dead-Letter Records';
    }
  };

  const getDescription = () => {
    switch (category) {
      case 'added':
        return 'Recently inserted catalog products from supplier feed batches.';
      case 'updated':
        return 'Catalog products that received price adjustments or name modifications with newer timestamps.';
      case 'withdrawn':
        return 'Items marked as WITHDRAWN (tombstoned) to safely prevent fulfillment without breaking audit logs.';
      case 'skipped':
        return 'Feeds with identical or older timestamps that were idempotently ignored.';
      case 'dead_letter':
        return 'Feeds isolated due to validation failures (e.g. non-positive price, empty SKU, corrupt schema).';
    }
  };

  const getFilteredItems = () => {
    if (category === 'dead_letter') {
      return dlqIssues;
    }
    if (category === 'withdrawn') {
      return catalogProducts.filter((p) => p.status === 'WITHDRAWN');
    }
    // For added/updated/skipped, show active products or sample subset
    return catalogProducts.filter((p) => p.status === 'ACTIVE');
  };

  const items = getFilteredItems();

  return (
    <Dialog open={!!category} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl sm:max-w-3xl p-6">
        <DialogHeader>
          <DialogTitle className="text-lg font-bold text-slate-900 flex items-center gap-2">
            <Layers className="h-5 w-5 text-blue-600" />
            <span>{getTitle()}</span>
          </DialogTitle>
          <DialogDescription className="text-xs text-slate-500">
            {getDescription()}
          </DialogDescription>
        </DialogHeader>

        <div className="rounded-md border border-slate-200 overflow-hidden max-h-[60vh] overflow-y-auto">
          {category === 'dead_letter' ? (
            <Table>
              <TableHeader className="bg-slate-50/75 sticky top-0">
                <TableRow>
                  <TableHead className="w-28">Issue ID</TableHead>
                  <TableHead className="w-32">SKU</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead className="w-36">Detected At</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="h-24 text-center text-slate-400 text-xs">
                      No quarantined items found.
                    </TableCell>
                  </TableRow>
                ) : (
                  (items as DeadLetterIssue[]).map((iss) => {
                    let parsed: any = {};
                    try {
                      parsed = JSON.parse(iss.raw_payload || '{}');
                    } catch {}
                    return (
                      <TableRow key={iss.issue_id}>
                        <TableCell className="font-mono text-xs text-slate-500">#{(iss.issue_id || '').substring(0, 8)}</TableCell>
                        <TableCell className="font-mono font-bold text-slate-900">{iss.sku || 'EMPTY'}</TableCell>
                        <TableCell>
                          <Badge variant="destructive" className="text-[10px] uppercase font-bold mr-2">
                            {iss.reason}
                          </Badge>
                          <span className="text-xs text-slate-600">{parsed.name || 'Unknown item'}</span>
                        </TableCell>
                        <TableCell className="text-slate-500 font-mono text-[11px]">{formatDateTime(iss.created_at)}</TableCell>
                      </TableRow>
                    );
                  })
                )}
              </TableBody>
            </Table>
          ) : (
            <Table>
              <TableHeader className="bg-slate-50/75 sticky top-0">
                <TableRow>
                  <TableHead className="w-36">SKU</TableHead>
                  <TableHead>Product Name</TableHead>
                  <TableHead className="w-28 text-right">Price</TableHead>
                  <TableHead className="w-24 text-center">Status</TableHead>
                  <TableHead className="w-36">Updated At</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="h-24 text-center text-slate-400 text-xs">
                      No matching products in current view.
                    </TableCell>
                  </TableRow>
                ) : (
                  (items as CatalogProduct[]).map((p) => (
                    <TableRow key={p.sku}>
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
                      <TableCell className="text-slate-500 font-mono text-[11px]">{formatDateTime(p.updated_at || p.last_updated_at || p.supplier_timestamp)}</TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
};
