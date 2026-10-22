import React, { useState } from 'react';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Badge } from './ui/badge';
import { Card, CardContent } from './ui/card';
import { Layers, Sparkles, ArrowRight } from 'lucide-react';
import { getRecentShops } from '../services/api';

interface ShopGatekeeperProps {
  onSelectShop: (shopId: string) => void;
}

export const ShopGatekeeper: React.FC<ShopGatekeeperProps> = ({ onSelectShop }) => {
  const [shopInput, setShopInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const recentShops = getRecentShops();

  const handleGenerateRandom = () => {
    const prefixes = ['shop', 'store', 'mart', 'brand'];
    const p = prefixes[Math.floor(Math.random() * prefixes.length)];
    const num = Math.floor(100 + Math.random() * 900);
    onSelectShop(`${p}-${num}`);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = shopInput.trim().toLowerCase();
    if (!clean) {
      setError('Please enter a shop name.');
      return;
    }
    if (!/^[a-z0-9_-]{1,64}$/.test(clean)) {
      setError('Lowercase letters, numbers, hyphens or underscores only.');
      return;
    }
    setError(null);
    onSelectShop(clean);
  };

  return (
    <div className="min-h-screen bg-slate-50 flex flex-col justify-center items-center px-4 py-12">
      <Card className="w-full max-w-sm border border-slate-200/80 bg-white shadow-sm rounded-xl">
        <CardContent className="p-6 space-y-5">
          {/* Minimal Brand Header */}
          <div className="space-y-1.5 text-center">
            <div className="inline-flex h-10 w-10 items-center justify-center rounded-lg bg-slate-100 text-slate-800 mb-1">
              <Layers className="h-5 w-5" />
            </div>
            <h1 className="text-lg font-semibold tracking-tight text-slate-900">
              Supplier Catalog
            </h1>
            <p className="text-xs text-slate-500">
              Enter a tenant name to access your catalog
            </p>
          </div>

          {/* Form */}
          <form onSubmit={handleSubmit} className="space-y-3">
            <div className="space-y-1.5">
              <Input
                value={shopInput}
                onChange={(e) => {
                  setShopInput(e.target.value);
                  setError(null);
                }}
                placeholder="Tenant ID (e.g. store-1)"
                className="h-9 text-xs font-mono bg-white border-slate-200 focus-visible:ring-slate-400"
                autoFocus
              />
              {error && <p className="text-[11px] text-rose-600 font-medium">{error}</p>}
            </div>

            <div className="flex gap-2">
              <Button
                type="submit"
                size="sm"
                className="flex-1 h-9 text-xs font-medium bg-slate-900 hover:bg-slate-800 text-white shadow-none"
              >
                Enter
                <ArrowRight className="h-3.5 w-3.5 ml-1" />
              </Button>
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleGenerateRandom}
                className="h-9 px-3 text-xs font-medium text-slate-600 border-slate-200 hover:bg-slate-50"
                title="Generate random tenant"
              >
                <Sparkles className="h-3.5 w-3.5 mr-1 text-slate-500" />
                Random
              </Button>
            </div>
          </form>

          {/* Recent Tenants */}
          {recentShops.length > 0 && (
            <div className="pt-3 border-t border-slate-100 space-y-1.5">
              <span className="text-[11px] font-medium text-slate-400">Recent:</span>
              <div className="flex flex-wrap gap-1.5">
                {recentShops.map((s) => (
                  <Badge
                    key={s}
                    variant="outline"
                    className="cursor-pointer text-[11px] font-mono py-0.5 px-2 bg-slate-50 hover:bg-slate-100 border-slate-200 text-slate-600 font-normal transition-colors"
                    onClick={() => onSelectShop(s)}
                  >
                    {s}
                  </Badge>
                ))}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
};
