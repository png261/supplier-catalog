import React, { useState } from 'react';
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
import { Badge } from './ui/badge';
import { Store, Sparkles, ArrowRight } from 'lucide-react';
import { getRecentShops } from '../services/api';

interface ShopModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  currentShopId: string;
  onSelectShop: (shopId: string) => void;
}

export const ShopModal: React.FC<ShopModalProps> = ({
  open,
  onOpenChange,
  currentShopId,
  onSelectShop,
}) => {
  const [inputShopId, setInputShopId] = useState(currentShopId);
  const [error, setError] = useState<string | null>(null);
  const recentShops = getRecentShops();

  const handleRandomize = () => {
    const prefixes = ['shop', 'store', 'mart', 'brand'];
    const p = prefixes[Math.floor(Math.random() * prefixes.length)];
    const num = Math.floor(100 + Math.random() * 900);
    const newId = `${p}-${num}`;
    setInputShopId(newId);
    setError(null);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = inputShopId.trim().toLowerCase();
    if (!clean) {
      setError('Shop name cannot be empty.');
      return;
    }
    if (!/^[a-z0-9_-]{1,64}$/.test(clean)) {
      setError('Lowercase letters, numbers, hyphens or underscores only.');
      return;
    }
    setError(null);
    onSelectShop(clean);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-sm p-6 border-slate-200 bg-white">
        <DialogHeader className="space-y-1">
          <div className="flex items-center gap-2">
            <div className="flex h-8 w-8 items-center justify-center rounded-lg bg-slate-100 text-slate-700">
              <Store className="h-4 w-4" />
            </div>
            <div>
              <DialogTitle className="text-base font-semibold text-slate-900">
                Switch Tenant
              </DialogTitle>
              <DialogDescription className="text-xs text-slate-500">
                Current: <span className="font-mono font-medium text-slate-700">{currentShopId || 'default'}</span>
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 pt-2">
          {/* Shop Input Field */}
          <div className="space-y-1.5">
            <div className="flex gap-2">
              <Input
                id="shop-id"
                value={inputShopId}
                onChange={(e) => {
                  setInputShopId(e.target.value);
                  setError(null);
                }}
                placeholder="Tenant ID (e.g. store-1)"
                className="h-9 text-xs font-mono bg-white border-slate-200"
                autoFocus
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleRandomize}
                className="h-9 px-2.5 text-xs text-slate-600 border-slate-200 hover:bg-slate-50"
                title="Generate random name"
              >
                <Sparkles className="h-3.5 w-3.5 text-slate-500" />
              </Button>
            </div>
            {error && <p className="text-[11px] text-rose-600 font-medium">{error}</p>}
          </div>

          {/* Recent Shops List */}
          {recentShops.length > 0 && (
            <div className="space-y-1.5">
              <span className="text-[11px] font-medium text-slate-400">Recent:</span>
              <div className="flex flex-wrap gap-1.5">
                {recentShops.map((s) => (
                  <Badge
                    key={s}
                    variant={s === currentShopId ? 'default' : 'outline'}
                    className={`cursor-pointer text-[11px] font-mono py-0.5 px-2 transition-colors ${
                      s === currentShopId
                        ? 'bg-slate-900 text-white border-transparent'
                        : 'bg-slate-50 hover:bg-slate-100 border-slate-200 text-slate-600 font-normal'
                    }`}
                    onClick={() => {
                      setInputShopId(s);
                      onSelectShop(s);
                      onOpenChange(false);
                    }}
                  >
                    {s}
                  </Badge>
                ))}
              </div>
            </div>
          )}

          <DialogFooter className="pt-2 sm:justify-end gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => onOpenChange(false)}
              className="h-8 text-xs text-slate-600 border-slate-200 hover:bg-slate-50"
            >
              Cancel
            </Button>
            <Button
              type="submit"
              size="sm"
              className="h-8 text-xs font-medium bg-slate-900 hover:bg-slate-800 text-white shadow-none"
            >
              Switch
              <ArrowRight className="h-3.5 w-3.5 ml-1" />
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
};
