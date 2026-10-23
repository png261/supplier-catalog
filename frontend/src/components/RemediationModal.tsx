import React, { useState, useEffect, useMemo } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Badge } from './ui/badge';
import { DeadLetterIssue } from '../types';
import { AlertOctagon, CheckCircle2, FileCode2, Sparkles } from 'lucide-react';

interface RemediationModalProps {
  issue: DeadLetterIssue | null;
  onOpenChange: (open: boolean) => void;
  onSubmitRemediation: (payload: {
    issue_id: string;
    sku: string;
    name: string;
    price: number;
    status: string;
  }) => Promise<void>;
}

export const RemediationModal: React.FC<RemediationModalProps> = ({
  issue,
  onOpenChange,
  onSubmitRemediation,
}) => {
  const [sku, setSku] = useState('');
  const [name, setName] = useState('');
  const [price, setPrice] = useState(0);
  const [status, setStatus] = useState('ACTIVE');
  const [isSubmitting, setIsSubmitting] = useState(false);

  const formattedPayload = useMemo(() => {
    if (!issue?.raw_payload) return '// Empty payload';
    try {
      const obj = JSON.parse(issue.raw_payload);
      return JSON.stringify(obj, null, 2);
    } catch {
      return issue.raw_payload;
    }
  }, [issue?.raw_payload]);

  useEffect(() => {
    if (issue) {
      let parsed: any = {};
      try {
        parsed = JSON.parse(issue.raw_payload || '{}');
      } catch {}
      const suffix = (issue.issue_id || '001').replace(/[^a-zA-Z0-9]/g, '').slice(-4).toUpperCase();
      setSku(issue.sku || parsed.sku || `SKU-FIXED-${suffix}`);
      setName(parsed.name || 'Remediated Supplier Product');
      const p = parseFloat(parsed.price);
      setPrice(!isNaN(p) && p > 0 ? p : 49.99);
      setStatus(parsed.status === 'WITHDRAWN' ? 'WITHDRAWN' : 'ACTIVE');
    }
  }, [issue]);

  if (!issue) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!sku.trim()) return;
    if (price <= 0) return;

    setIsSubmitting(true);
    try {
      await onSubmitRemediation({
        issue_id: issue.issue_id,
        sku: sku.trim(),
        name: name.trim() || 'Remediated Product',
        price: Number(price),
        status,
      });
      onOpenChange(false);
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <Dialog open={!!issue} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-lg p-6 bg-white">
        <DialogHeader>
          <DialogTitle className="text-base font-bold text-slate-900 flex items-center gap-2">
            <AlertOctagon className="h-5 w-5 text-rose-600" />
            <span>Review & Remediate Quarantined Product</span>
          </DialogTitle>
          <DialogDescription className="text-xs text-slate-500">
            Inspect the raw supplier transmission below and repair invalid attributes to safely ingest this item into the live catalog.
          </DialogDescription>
        </DialogHeader>

        {/* Defect Context & Raw Payload Inspector */}
        <div className="space-y-2.5">
          <div className="p-3 rounded-lg border border-rose-200 bg-rose-50/80 text-rose-900 text-xs space-y-1.5">
            <div className="flex items-center justify-between">
              <span className="font-semibold text-rose-800">Quarantine Reason:</span>
              <Badge variant="destructive" className="text-[10px] uppercase font-bold">
                {issue.reason}
              </Badge>
            </div>
            <div className="flex items-center justify-between text-slate-600 text-[11px] font-mono">
              <span>Issue ID: #{(issue.issue_id || '').substring(0, 12)}</span>
              <span>Job: {issue.job_id} (Batch #{issue.batch_index + 1})</span>
            </div>
          </div>

          <div className="rounded-lg border border-slate-200 bg-slate-900 text-slate-100 overflow-hidden">
            <div className="px-3 py-1.5 bg-slate-800/90 border-b border-slate-700/80 flex items-center justify-between text-[11px] text-slate-300 font-mono">
              <span className="flex items-center gap-1.5">
                <FileCode2 className="h-3.5 w-3.5 text-slate-400" />
                Raw Supplier Payload (Defective)
              </span>
              <span className="text-[10px] text-rose-400">Read-Only</span>
            </div>
            <pre className="p-3 text-[11px] font-mono text-slate-200 overflow-x-auto max-h-32 leading-relaxed">
              {formattedPayload}
            </pre>
          </div>
        </div>

        {/* Remediation Form */}
        <form onSubmit={handleSubmit} className="space-y-3.5 text-xs pt-1">
          <div className="flex items-center justify-between text-[11px] text-indigo-700 bg-indigo-50/70 border border-indigo-100 px-2.5 py-1.5 rounded-md">
            <span className="flex items-center gap-1.5 font-medium">
              <Sparkles className="h-3.5 w-3.5 text-indigo-600" />
              Defective fields have been pre-populated with valid suggested defaults.
            </span>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
            <div>
              <Label htmlFor="rem-sku" className="mb-1 block text-slate-700">
                Product SKU {issue.reason === 'EMPTY_SKU' && <span className="text-rose-600 font-semibold">(Repaired)</span>}
              </Label>
              <Input
                id="rem-sku"
                value={sku}
                onChange={(e) => setSku(e.target.value)}
                required
                className="h-8 font-mono text-xs bg-white border-slate-200"
                placeholder="e.g. PROD-1001"
              />
            </div>

            <div>
              <Label htmlFor="rem-status" className="mb-1 block text-slate-700">Catalog Status</Label>
              <select
                id="rem-status"
                value={status}
                onChange={(e) => setStatus(e.target.value)}
                className="h-8 w-full rounded-md border border-slate-200 bg-white px-2.5 py-1 text-xs text-slate-800 focus:outline-none focus:ring-1 focus:ring-slate-400"
              >
                <option value="ACTIVE">ACTIVE</option>
                <option value="WITHDRAWN">WITHDRAWN</option>
              </select>
            </div>
          </div>

          <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
            <div className="sm:col-span-2">
              <Label htmlFor="rem-name" className="mb-1 block text-slate-700">Product Name</Label>
              <Input
                id="rem-name"
                value={name}
                onChange={(e) => setName(e.target.value)}
                required
                className="h-8 text-xs bg-white border-slate-200"
                placeholder="e.g. Ergonomic Office Chair"
              />
            </div>

            <div>
              <Label htmlFor="rem-price" className="mb-1 block text-slate-700">
                Unit Price ($) {issue.reason === 'NEGATIVE_OR_ZERO_PRICE' && <span className="text-rose-600 font-semibold">(Repaired)</span>}
              </Label>
              <Input
                id="rem-price"
                type="number"
                step="0.01"
                min="0.01"
                value={price}
                onChange={(e) => setPrice(parseFloat(e.target.value) || 0)}
                required
                className="h-8 font-mono text-xs bg-white border-slate-200"
              />
            </div>
          </div>

          <DialogFooter className="pt-3 border-t border-slate-100 gap-2 sm:gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
              className="h-8 text-xs"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              disabled={isSubmitting || !sku.trim() || price <= 0}
              className="h-8 bg-emerald-600 hover:bg-emerald-700 text-white text-xs font-semibold"
            >
              <CheckCircle2 className="h-3.5 w-3.5 mr-1.5" />
              <span>{isSubmitting ? 'Importing...' : 'Import Remediated Product'}</span>
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
};
