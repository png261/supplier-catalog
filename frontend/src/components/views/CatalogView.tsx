import React, { useState, useEffect } from 'react';
import { Card } from '../ui/card';
import { Input } from '../ui/input';
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
import { CatalogProduct, ProductHistoryEntry } from '../../types';
import { formatCurrency, formatDateTime, formatNumber } from '../../lib/utils';
import { fetchProductHistory } from '../../services/api';
import { ProductHistoryModal } from '../ProductHistoryModal';
import { Package, Search, ChevronLeft, ChevronRight, RotateCcw, X, History } from 'lucide-react';

interface CatalogViewProps {
  products: CatalogProduct[];
  total: number;
  page: number;
  limit: number;
  search: string;
  status: string;
  onSearchChange: (search: string) => void;
  onStatusChange: (status: string) => void;
  onPageChange: (page: number) => void;
  onResetFilters: () => void;
}

export const CatalogView: React.FC<CatalogViewProps> = ({
  products,
  total,
  page,
  limit,
  search,
  status,
  onSearchChange,
  onStatusChange,
  onPageChange,
  onResetFilters,
}) => {
  const [localSearch, setLocalSearch] = useState(search);
  const [selectedProduct, setSelectedProduct] = useState<CatalogProduct | null>(null);
  const [productHistory, setProductHistory] = useState<ProductHistoryEntry[]>([]);
  const [isLoadingHistory, setIsLoadingHistory] = useState(false);
  const [isHistoryModalOpen, setIsHistoryModalOpen] = useState(false);
  const totalPages = Math.max(1, Math.ceil(total / limit));

  const handleInspectProductHistory = async (product: CatalogProduct) => {
    setSelectedProduct(product);
    setIsHistoryModalOpen(true);
    setIsLoadingHistory(true);
    try {
      const res = await fetchProductHistory(product.sku);
      setProductHistory(res.history || []);
    } catch (err) {
      console.error('Failed to fetch product history:', err);
      setProductHistory([]);
    } finally {
      setIsLoadingHistory(false);
    }
  };

  useEffect(() => {
    setLocalSearch(search);
  }, [search]);

  useEffect(() => {
    const timer = setTimeout(() => {
      if (localSearch !== search) {
        onSearchChange(localSearch);
      }
    }, 250);
    return () => clearTimeout(timer);
  }, [localSearch, search, onSearchChange]);

  return (
    <div className="space-y-4">
      {/* View Header & Toolbar */}
      <Card className="p-3.5 sm:p-4 border-slate-200/80 bg-white shadow-xs">
        <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3">
          <div className="flex items-center gap-2">
            <Package className="h-4 w-4 text-slate-700" />
            <h2 className="text-base font-semibold text-slate-900 tracking-tight">
              Catalog
            </h2>
            <Badge variant="secondary" className="font-mono text-xs font-medium px-2 py-0 h-5">
              {formatNumber(total)}
            </Badge>
          </div>

          {/* Search & Status Filters */}
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative w-full sm:w-64">
              <Search className="absolute left-2.5 top-2.5 h-3.5 w-3.5 text-slate-400 pointer-events-none" />
              <Input
                placeholder="Search SKU or Name..."
                value={localSearch}
                onChange={(e) => setLocalSearch(e.target.value)}
                className="pl-8 pr-7 h-8 text-xs bg-white border-slate-200"
              />
              {localSearch && (
                <button
                  type="button"
                  onClick={() => {
                    setLocalSearch('');
                    onSearchChange('');
                  }}
                  className="absolute right-2 top-2 text-slate-400 hover:text-slate-700"
                  title="Clear search"
                  aria-label="Clear search"
                >
                  <X className="h-3.5 w-3.5" />
                </button>
              )}
            </div>

            <select
              value={status}
              onChange={(e) => onStatusChange(e.target.value)}
              className="h-8 rounded-md border border-slate-200 bg-white px-2.5 py-1 text-xs text-slate-700 font-medium shadow-none focus:outline-none focus:ring-1 focus:ring-slate-400"
            >
              <option value="">All Statuses</option>
              <option value="ACTIVE">ACTIVE only</option>
              <option value="WITHDRAWN">WITHDRAWN only</option>
            </select>

            {(localSearch || search || status) && (
              <Button
                variant="ghost"
                size="sm"
                onClick={() => {
                  setLocalSearch('');
                  onResetFilters();
                }}
                className="h-8 px-2.5 text-xs text-slate-500 hover:text-slate-900"
                title="Clear all filters"
              >
                <RotateCcw className="h-3.5 w-3.5 mr-1" />
                Clear
              </Button>
            )}
          </div>
        </div>
      </Card>

      {/* Products Table Card */}
      <Card className="border-slate-200/80 bg-white shadow-xs overflow-hidden">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader className="bg-slate-50/75">
              <TableRow className="border-slate-200">
                <TableHead className="w-36 text-slate-700">SKU</TableHead>
                <TableHead className="text-slate-700">Product Name</TableHead>
                <TableHead className="w-32 text-right text-slate-700">Unit Price</TableHead>
                <TableHead className="w-28 text-center text-slate-700">Status</TableHead>
                <TableHead className="w-44 text-slate-700">Last Synchronized</TableHead>
                <TableHead className="w-28 text-right text-slate-700">Edit History</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {products.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="h-36 text-center text-slate-400 text-xs">
                    No products found matching filter criteria. Start a sync or adjust filters.
                  </TableCell>
                </TableRow>
              ) : (
                products.map((p) => (
                  <TableRow
                    key={p.sku}
                    onClick={() => handleInspectProductHistory(p)}
                    className="hover:bg-slate-50/90 border-slate-200 cursor-pointer transition-colors group"
                    title={`Click to view modification history for ${p.sku}`}
                  >
                    <TableCell className="font-mono font-semibold text-indigo-700 group-hover:underline">
                      {p.sku}
                    </TableCell>
                    <TableCell className="font-medium text-slate-800">
                      {p.name}
                    </TableCell>
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
                    <TableCell className="text-right" onClick={(e) => e.stopPropagation()}>
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={() => handleInspectProductHistory(p)}
                        className="h-7 px-2.5 text-xs text-slate-700 border-slate-200 hover:bg-indigo-50 hover:text-indigo-700 hover:border-indigo-200 font-medium shadow-none"
                      >
                        <History className="h-3 w-3 mr-1" />
                        History
                      </Button>
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
            Showing {products.length > 0 ? (page - 1) * limit + 1 : 0} to{' '}
            {Math.min(page * limit, total)} of {formatNumber(total)} products
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

      <ProductHistoryModal
        isOpen={isHistoryModalOpen}
        onOpenChange={setIsHistoryModalOpen}
        product={selectedProduct}
        history={productHistory}
        isLoading={isLoadingHistory}
      />
    </div>
  );
};
