import React from 'react';
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
import { CatalogProduct, ProductHistoryEntry } from '../types';
import { formatCurrency, formatDateTime } from '../lib/utils';
import {
  History,
  ArrowRight,
  Clock,
  User,
  Package,
  GitCommit,
  TrendingUp,
  TrendingDown,
} from 'lucide-react';

interface ProductHistoryModalProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  product: CatalogProduct | null;
  history: ProductHistoryEntry[];
  isLoading: boolean;
}

export const ProductHistoryModal: React.FC<ProductHistoryModalProps> = ({
  isOpen,
  onOpenChange,
  product,
  history,
  isLoading,
}) => {
  if (!product) return null;

  const getActionBadge = (action: string) => {
    switch (action) {
      case 'ADDED':
        return (
          <Badge className="bg-emerald-100 text-emerald-800 border-0 text-[10px] font-bold uppercase">
            ADDED
          </Badge>
        );
      case 'UPDATED':
        return (
          <Badge className="bg-blue-100 text-blue-800 border-0 text-[10px] font-bold uppercase">
            UPDATED
          </Badge>
        );
      case 'WITHDRAWN':
        return (
          <Badge className="bg-rose-100 text-rose-800 border-0 text-[10px] font-bold uppercase">
            WITHDRAWN
          </Badge>
        );
      default:
        return (
          <Badge variant="secondary" className="text-[10px] font-bold uppercase">
            {action}
          </Badge>
        );
    }
  };

  const getVersionTypeBadge = (type?: string) => {
    switch (type) {
      case 'ROLLBACK':
        return (
          <Badge
            variant="outline"
            className="border-amber-500 text-amber-700 bg-amber-50 font-mono text-[10px] font-semibold"
          >
            ROLLBACK
          </Badge>
        );
      case 'PATCH':
        return (
          <Badge
            variant="outline"
            className="border-indigo-500 text-indigo-700 bg-indigo-50 font-mono text-[10px] font-semibold"
          >
            PATCH
          </Badge>
        );
      case 'SYNC':
      default:
        return (
          <Badge
            variant="outline"
            className="border-emerald-500 text-emerald-700 bg-emerald-50 font-mono text-[10px] font-semibold"
          >
            {type || 'SYNC'}
          </Badge>
        );
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl max-h-[85vh] flex flex-col p-0 overflow-hidden bg-white">
        <DialogHeader className="p-6 pb-4 border-b border-slate-100 bg-slate-50/60">
          <div className="flex items-start justify-between gap-4">
            <div className="flex items-center gap-3">
              <div className="h-10 w-10 rounded-xl bg-indigo-50 border border-indigo-100 flex items-center justify-center text-indigo-600 shrink-0">
                <History className="h-5 w-5" />
              </div>
              <div>
                <DialogTitle className="text-base sm:text-lg font-semibold text-slate-900 flex items-center gap-2 flex-wrap">
                  <span>Product Modification History</span>
                  <span className="font-mono text-xs bg-slate-200/80 text-slate-800 px-2 py-0.5 rounded">
                    {product.sku}
                  </span>
                  <Badge
                    variant={product.status === 'ACTIVE' ? 'success' : 'warning'}
                    className="text-[10px] uppercase font-bold"
                  >
                    {product.status}
                  </Badge>
                </DialogTitle>
                <DialogDescription className="text-xs text-slate-500 mt-1 flex items-center gap-2 flex-wrap">
                  <span className="font-medium text-slate-700">{product.name}</span>
                  <span>•</span>
                  <span className="font-mono font-semibold text-slate-900">
                    Current Price: {formatCurrency(product.price)}
                  </span>
                </DialogDescription>
              </div>
            </div>
          </div>
        </DialogHeader>

        {/* Subheader Stats */}
        <div className="px-6 py-2.5 bg-slate-50/40 border-b border-slate-100 flex items-center justify-between text-xs text-slate-600">
          <span className="font-medium text-slate-700">
            Recorded Revisions:{' '}
            <span className="font-mono font-semibold text-slate-900">{history.length}</span>
          </span>
          <span className="text-[11px] text-slate-500">
            Showing newest to oldest SKU state transitions
          </span>
        </div>

        {/* Timeline Content */}
        <div className="flex-1 overflow-y-auto p-6 space-y-3">
          {isLoading ? (
            <div className="py-16 text-center text-slate-400 text-sm">
              <div className="animate-spin h-6 w-6 border-2 border-indigo-600 border-t-transparent rounded-full mx-auto mb-2" />
              Loading product modification history...
            </div>
          ) : history.length === 0 ? (
            <div className="py-12 text-center text-slate-500 text-xs">
              <Package className="h-8 w-8 text-slate-300 mx-auto mb-2" />
              No modification history found for SKU <span className="font-mono font-semibold">{product.sku}</span>.
            </div>
          ) : (
            <div className="relative pl-4 border-l-2 border-slate-200 space-y-4">
              {history.map((entry, idx) => {
                const isLatest = idx === 0;
                const hasOldPrice = entry.old_price !== null && entry.old_price !== undefined;
                const priceDiff = hasOldPrice ? entry.new_price - (entry.old_price || 0) : 0;
                const priceChanged = hasOldPrice && Math.abs(priceDiff) > 0.0001;
                const nameChanged =
                  Boolean(entry.old_name) && entry.old_name !== entry.new_name;
                const statusChanged =
                  Boolean(entry.old_status) && entry.old_status !== entry.new_status;

                return (
                  <div
                    key={`${entry.version_id}-${entry.delta_id || idx}`}
                    className={`relative rounded-xl border p-4 transition-colors ${
                      isLatest
                        ? 'border-indigo-200 bg-indigo-50/20 shadow-2xs'
                        : 'border-slate-200/80 bg-white hover:bg-slate-50/50'
                    }`}
                  >
                    {/* Timeline Node Dot */}
                    <div
                      className={`absolute -left-[23px] top-5 h-3 w-3 rounded-full border-2 ${
                        isLatest
                          ? 'bg-indigo-600 border-indigo-200 ring-4 ring-indigo-50'
                          : 'bg-white border-slate-400'
                      }`}
                    />

                    {/* Entry Top Bar */}
                    <div className="flex flex-wrap items-center justify-between gap-2 pb-2.5 border-b border-slate-100">
                      <div className="flex items-center gap-2 flex-wrap">
                        {getActionBadge(entry.action)}
                        {entry.version_number > 0 && (
                          <span className="font-mono text-xs font-bold text-slate-900 bg-slate-100 px-2 py-0.5 rounded flex items-center gap-1">
                            <GitCommit className="h-3 w-3 text-slate-500" />
                            v{entry.version_number}
                          </span>
                        )}
                        {getVersionTypeBadge(entry.version_type)}
                        {isLatest && (
                          <Badge
                            variant="outline"
                            className="text-[10px] border-indigo-200 bg-indigo-100/70 text-indigo-800 font-semibold"
                          >
                            CURRENT STATE
                          </Badge>
                        )}
                      </div>

                      <div className="flex items-center gap-3 text-[11px] text-slate-500 font-mono">
                        <span className="flex items-center gap-1">
                          <User className="h-3 w-3 text-slate-400" />
                          {entry.author || 'supplier-sync'}
                        </span>
                        <span className="flex items-center gap-1">
                          <Clock className="h-3 w-3 text-slate-400" />
                          {formatDateTime(entry.created_at || entry.new_timestamp)}
                        </span>
                      </div>
                    </div>

                    {/* Version / Sync Job Description */}
                    {entry.description && (
                      <div className="mt-2 text-xs text-slate-600 font-medium">
                        {entry.description}
                        <span className="ml-2 font-mono text-[11px] text-slate-400">
                          ({entry.version_id})
                        </span>
                      </div>
                    )}

                    {/* Detailed Field Diffs Grid */}
                    <div className="mt-3 grid grid-cols-1 sm:grid-cols-3 gap-2.5 pt-2">
                      {/* 1. Product Name Diff */}
                      <div className="p-2.5 rounded-lg bg-slate-50/80 border border-slate-200/60">
                        <div className="text-[10px] font-semibold uppercase tracking-wider text-slate-400 mb-1">
                          Product Name
                        </div>
                        {nameChanged ? (
                          <div className="text-xs space-y-0.5">
                            <div className="line-through text-slate-400 truncate" title={entry.old_name || ''}>
                              {entry.old_name}
                            </div>
                            <div className="font-semibold text-slate-900 flex items-center gap-1">
                              <ArrowRight className="h-3 w-3 text-blue-600 shrink-0" />
                              <span className="truncate" title={entry.new_name}>
                                {entry.new_name}
                              </span>
                            </div>
                          </div>
                        ) : (
                          <div className="text-xs font-medium text-slate-800 truncate" title={entry.new_name}>
                            {entry.new_name}
                            {entry.action === 'ADDED' && (
                              <span className="ml-1.5 text-[10px] text-emerald-600 font-normal">(Initial)</span>
                            )}
                          </div>
                        )}
                      </div>

                      {/* 2. Unit Price Diff */}
                      <div className="p-2.5 rounded-lg bg-slate-50/80 border border-slate-200/60">
                        <div className="text-[10px] font-semibold uppercase tracking-wider text-slate-400 mb-1">
                          Unit Price
                        </div>
                        {priceChanged ? (
                          <div className="font-mono text-xs space-y-0.5">
                            <div className="flex items-center gap-1.5">
                              <span className="line-through text-slate-400">
                                {formatCurrency(entry.old_price || 0)}
                              </span>
                              <ArrowRight className="h-3 w-3 text-slate-400" />
                              <span
                                className={
                                  priceDiff > 0
                                    ? 'text-emerald-700 font-bold'
                                    : 'text-blue-700 font-bold'
                                }
                              >
                                {formatCurrency(entry.new_price)}
                              </span>
                            </div>
                            <div className="flex items-center gap-1 text-[11px]">
                              {priceDiff > 0 ? (
                                <span className="text-emerald-600 flex items-center gap-0.5 font-medium">
                                  <TrendingUp className="h-3 w-3" />+{formatCurrency(priceDiff)}
                                </span>
                              ) : (
                                <span className="text-blue-600 flex items-center gap-0.5 font-medium">
                                  <TrendingDown className="h-3 w-3" />
                                  {formatCurrency(priceDiff)}
                                </span>
                              )}
                            </div>
                          </div>
                        ) : (
                          <div className="font-mono text-xs font-bold text-slate-900">
                            {formatCurrency(entry.new_price)}
                            {hasOldPrice && (
                              <span className="ml-1.5 text-[10px] font-sans font-normal text-slate-400">
                                (Unchanged)
                              </span>
                            )}
                          </div>
                        )}
                      </div>

                      {/* 3. Catalog Status Diff */}
                      <div className="p-2.5 rounded-lg bg-slate-50/80 border border-slate-200/60">
                        <div className="text-[10px] font-semibold uppercase tracking-wider text-slate-400 mb-1">
                          Lifecycle Status
                        </div>
                        {statusChanged ? (
                          <div className="flex items-center gap-1.5 text-xs font-mono pt-0.5">
                            <Badge
                              variant="outline"
                              className="text-[10px] text-slate-500 border-slate-300 line-through"
                            >
                              {entry.old_status}
                            </Badge>
                            <ArrowRight className="h-3 w-3 text-slate-400" />
                            <Badge
                              variant={entry.new_status === 'ACTIVE' ? 'success' : 'warning'}
                              className="text-[10px] uppercase font-bold"
                            >
                              {entry.new_status}
                            </Badge>
                          </div>
                        ) : (
                          <div className="pt-0.5 flex items-center gap-1.5">
                            <Badge
                              variant={entry.new_status === 'ACTIVE' ? 'success' : 'warning'}
                              className="text-[10px] uppercase font-bold"
                            >
                              {entry.new_status}
                            </Badge>
                            {entry.old_status && (
                              <span className="text-[10px] text-slate-400">(Unchanged)</span>
                            )}
                          </div>
                        )}
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        <DialogFooter className="p-4 border-t border-slate-100 bg-slate-50/50 flex sm:justify-between items-center">
          <span className="text-[11px] text-slate-500">
            Audit trail reconstructed from immutable catalog version deltas
          </span>
          <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
            Close
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
