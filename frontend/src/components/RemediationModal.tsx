import React, { useState, useEffect } from 'react';
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
import { AlertOctagon, CheckCircle2 } from 'lucide-react';

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

  useEffect(() => {
    if (issue) {
      let parsed: any = {};
      try {
        parsed = JSON.parse(issue.raw_payload || '{}');
      } catch {}
      setSku(issue.sku || parsed.sku || 'SKU-REMEDIED-001');
      setName(parsed.name || 'Remediated Product');
      const p = parseFloat(parsed.price);
      setPrice(!isNaN(p) && p > 0 ? p : 99.99);
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
      <DialogContent className="max-w-md p-6">
        <DialogHeader>
          <DialogTitle className="text-lg font-bold text-slate-900 flex items-center gap-2">
            <AlertOctagon className="h-5 w-5 text-red-600" />
            <span>Review & Remediate Quarantined Product</span>
          </DialogTitle>
          <DialogDescription className="text-xs text-slate-500">
            Correct invalid supplier attributes to safely ingest this product into the active catalog.
          </DialogDescription>
        </DialogHeader>

        {/* Defect Alert Box */}
        <div className="p-3 rounded-lg border border-red-200 bg-red-50 text-red-900 text-xs space-y-1">
          <div className="flex items-center justify-between">
            <span className="font-semibold text-red-800">Quarantine Reason:</span>
            <Badge variant="destructive" className="text-[10px] uppercase font-bold">
              {issue.reason}
            </Badge>
          </div>
          <div className="flex items-center justify-between text-slate-600 text-[11px]">
            <span>Issue ID: <code className="font-mono">{(issue.issue_id || '').substring(0, 10)}...</code></span>
            <span>Batch Index: #{issue.batch_index + 1}</span>
          </div>
        </div>

        {/* Remediation Form */}
        <form onSubmit={handleSubmit} className="space-y-3.5 text-xs">
          <div>
            <Label htmlFor="rem-sku" className="mb-1 block">Product SKU:</Label>
            <Input
              id="rem-sku"
              value={sku}
              onChange={(e) => setSku(e.target.value)}
              required
              className="h-8 font-mono text-xs"
              placeholder="e.g. PROD-1001"
            />
          </div>

          <div>
            <Label htmlFor="rem-name" className="mb-1 block">Product Name:</Label>
            <Input
              id="rem-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
              className="h-8 text-xs"
              placeholder="e.g. Ergonomic Office Chair"
            />
          </div>

          <div className="grid grid-cols-2 gap-3">
            <div>
              <Label htmlFor="rem-price" className="mb-1 block">Unit Price ($):</Label>
              <Input
                id="rem-price"
                type="number"
                step="0.01"
                min="0.01"
                value={price}
                onChange={(e) => setPrice(parseFloat(e.target.value) || 0)}
                required
                className="h-8 font-mono text-xs"
              />
            </div>

            <div>
              <Label htmlFor="rem-status" className="mb-1 block">Catalog Status:</Label>
              <select
                id="rem-status"
                value={status}
                onChange={(e) => setStatus(e.target.value)}
                className="h-8 w-full rounded-md border border-slate-200 bg-white px-2.5 py-1 text-xs text-slate-800 shadow-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
              >
                <option value="ACTIVE">ACTIVE</option>
                <option value="WITHDRAWN">WITHDRAWN</option>
              </select>
            </div>
          </div>

          <DialogFooter className="pt-3 border-t border-slate-100 gap-2 sm:gap-0">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              Cancel
            </Button>
            <Button
              type="submit"
              variant="primary"
              size="sm"
              disabled={isSubmitting}
              className="bg-emerald-600 hover:bg-emerald-700 text-white font-semibold"
            >
              <CheckCircle2 className="h-3.5 w-3.5 mr-1" />
              <span>Import Remediated Product</span>
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
};
