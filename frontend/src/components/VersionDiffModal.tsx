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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from './ui/table';
import { CatalogVersion, VersionDiffItem } from '../types';
import { formatCurrency, formatDateTime } from '../lib/utils';
import { GitCommit, ArrowRight, CheckCircle2, AlertTriangle, XCircle, Clock } from 'lucide-react';

interface VersionDiffModalProps {
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  version: CatalogVersion | null;
  changes: VersionDiffItem[];
  isLoading: boolean;
  onRollbackClick?: (version: CatalogVersion) => void;
  isCurrentVersion?: boolean;
}

export const VersionDiffModal: React.FC<VersionDiffModalProps> = ({
  isOpen,
  onOpenChange,
  version,
  changes,
  isLoading,
  onRollbackClick,
  isCurrentVersion = false,
}) => {
  if (!version) return null;

  const versionTypeBadge = (type?: string) => {
    switch (type) {
      case 'ROLLBACK':
        return <Badge variant="outline" className="border-amber-500 text-amber-700 bg-amber-50 font-mono text-xs">ROLLBACK</Badge>;
      case 'PATCH':
        return <Badge variant="outline" className="border-indigo-500 text-indigo-700 bg-indigo-50 font-mono text-xs">PATCH</Badge>;
      case 'SYNC':
        return <Badge variant="outline" className="border-emerald-500 text-emerald-700 bg-emerald-50 font-mono text-xs">SYNC</Badge>;
      default:
        return <Badge variant="secondary" className="font-mono text-xs">{type || 'MILESTONE'}</Badge>;
    }
  };

  const getActionBadge = (action: string) => {
    switch (action) {
      case 'ADDED':
        return <Badge className="bg-emerald-100 text-emerald-800 border-0 text-xs font-semibold">ADDED</Badge>;
      case 'UPDATED':
        return <Badge className="bg-blue-100 text-blue-800 border-0 text-xs font-semibold">UPDATED</Badge>;
      case 'WITHDRAWN':
        return <Badge className="bg-rose-100 text-rose-800 border-0 text-xs font-semibold">WITHDRAWN</Badge>;
      default:
        return <Badge variant="secondary" className="text-xs">{action}</Badge>;
    }
  };

  return (
    <Dialog open={isOpen} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-3xl max-h-[85vh] flex flex-col p-0 overflow-hidden bg-white">
        <DialogHeader className="p-6 pb-4 border-b border-slate-100 bg-slate-50/50">
          <div className="flex items-center justify-between gap-4">
            <div className="flex items-center gap-2.5">
              <div className="h-8 w-8 rounded-lg bg-indigo-50 border border-indigo-100 flex items-center justify-center text-indigo-600">
                <GitCommit className="h-4 w-4" />
              </div>
              <div>
                <DialogTitle className="text-lg font-semibold text-slate-900 flex items-center gap-2">
                  Version v{version.version_number} Diff
                  {versionTypeBadge(version.type || version.trigger_type)}
                </DialogTitle>
                <DialogDescription className="text-xs text-slate-500 flex items-center gap-3 mt-1">
                  <span className="font-mono text-slate-600 font-medium">{version.version_id}</span>
                  <span>•</span>
                  <span>Author: {version.author || version.created_by || 'system'}</span>
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

        {/* Changes Summary Header */}
        <div className="px-6 py-3 bg-slate-50/30 border-b border-slate-100 flex items-center justify-between text-xs text-slate-600">
          <span className="font-medium text-slate-700">
            Total Changes: <span className="font-semibold text-slate-900">{changes.length} items</span>
          </span>
          <div className="flex items-center gap-3 font-mono text-[11px]">
            <span className="text-emerald-700">+{version.items_added || 0} added</span>
            <span className="text-blue-700">~{version.items_updated || 0} updated</span>
            <span className="text-rose-700">-{version.items_withdrawn || 0} withdrawn</span>
          </div>
        </div>

        {/* Changes Table / Content */}
        <div className="flex-1 overflow-y-auto p-6 pt-2">
          {isLoading ? (
            <div className="py-16 text-center text-slate-400 text-sm">
              <div className="animate-spin h-6 w-6 border-2 border-indigo-600 border-t-transparent rounded-full mx-auto mb-2" />
              Loading item deltas and pre-image state...
            </div>
          ) : changes.length === 0 ? (
            <div className="py-12 text-center text-slate-500 text-sm">
              No item deltas recorded for this version.
            </div>
          ) : (
            <div className="rounded-md border border-slate-200 overflow-hidden">
              <Table>
                <TableHeader className="bg-slate-50">
                  <TableRow>
                    <TableHead className="w-[100px] text-xs">Action</TableHead>
                    <TableHead className="text-xs">SKU / Product</TableHead>
                    <TableHead className="text-xs">Price Change</TableHead>
                    <TableHead className="text-xs">Status Change</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {changes.map((change, idx) => (
                    <TableRow key={`${change.sku}-${idx}`} className="text-xs hover:bg-slate-50/50">
                      <TableCell className="py-2.5">
                        {getActionBadge(change.action || change.operation || 'UPDATED')}
                      </TableCell>
                      <TableCell className="py-2.5 font-medium">
                        <div className="font-mono text-slate-900 font-semibold">{change.sku}</div>
                        <div className="text-[11px] text-slate-500 truncate max-w-[200px]" title={change.new_name}>
                          {change.old_name && change.old_name !== change.new_name ? (
                            <span>
                              <span className="line-through text-slate-400 mr-1">{change.old_name}</span>
                              <ArrowRight className="inline h-2.5 w-2.5 text-slate-400 mx-0.5" />
                              <span>{change.new_name}</span>
                            </span>
                          ) : (
                            change.new_name
                          )}
                        </div>
                      </TableCell>
                      <TableCell className="py-2.5">
                        {change.old_price !== null && change.old_price !== undefined ? (
                          <div className="flex items-center gap-1.5 font-mono">
                            <span className="line-through text-slate-400">
                              {formatCurrency(change.old_price)}
                            </span>
                            <ArrowRight className="h-3 w-3 text-slate-400" />
                            <span className={change.new_price > change.old_price ? 'text-emerald-600 font-semibold' : 'text-blue-600 font-semibold'}>
                              {formatCurrency(change.new_price)}
                            </span>
                          </div>
                        ) : (
                          <span className="font-mono text-emerald-600 font-semibold">
                            {formatCurrency(change.new_price)}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="py-2.5">
                        {change.old_status && change.old_status !== change.new_status ? (
                          <div className="flex items-center gap-1.5">
                            <span className={change.old_status === 'ACTIVE' ? 'text-emerald-700' : 'text-slate-400'}>
                              {change.old_status}
                            </span>
                            <ArrowRight className="h-3 w-3 text-slate-400" />
                            <span className={change.new_status === 'ACTIVE' ? 'text-emerald-700 font-semibold' : 'text-rose-600 font-semibold'}>
                              {change.new_status}
                            </span>
                          </div>
                        ) : (
                          <Badge variant="outline" className={change.new_status === 'ACTIVE' ? 'border-emerald-200 text-emerald-700 bg-emerald-50/50' : 'border-rose-200 text-rose-700 bg-rose-50/50'}>
                            {change.new_status}
                          </Badge>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </div>

        <DialogFooter className="p-4 border-t border-slate-100 bg-slate-50/50 flex sm:justify-between items-center gap-2">
          <div className="text-[11px] text-slate-500">
            {isCurrentVersion ? (
              <span className="text-emerald-600 font-medium flex items-center gap-1">
                <CheckCircle2 className="h-3.5 w-3.5" /> Current Active Version
              </span>
            ) : (
              <span>Rollback will generate a forward-moving audit milestone</span>
            )}
          </div>
          <div className="flex items-center gap-2">
            <Button variant="outline" size="sm" onClick={() => onOpenChange(false)}>
              Close
            </Button>
            {!isCurrentVersion && onRollbackClick && (
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
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
