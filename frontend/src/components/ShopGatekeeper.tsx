import React, { useState } from 'react';
import { Button } from './ui/button';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Badge } from './ui/badge';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from './ui/card';
import { Store, Sparkles, History, ShieldCheck, ArrowRight, Database, Clock, Zap } from 'lucide-react';
import { getRecentShops } from '../services/api';

interface ShopGatekeeperProps {
  onSelectShop: (shopId: string) => void;
}

export const ShopGatekeeper: React.FC<ShopGatekeeperProps> = ({ onSelectShop }) => {
  const [shopInput, setShopInput] = useState('');
  const [error, setError] = useState<string | null>(null);
  const recentShops = getRecentShops();

  const handleGenerateRandom = () => {
    const prefixes = ['shop', 'store', 'outlet', 'mart', 'boutique', 'brand'];
    const p = prefixes[Math.floor(Math.random() * prefixes.length)];
    const num = Math.floor(1000 + Math.random() * 9000);
    const newId = `${p}-${num}`;
    onSelectShop(newId);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = shopInput.trim().toLowerCase();
    if (!clean) {
      setError('Vui lòng nhập tên shop hoặc bấm tạo ngẫu nhiên.');
      return;
    }
    if (!/^[a-z0-9_-]{1,64}$/.test(clean)) {
      setError('Tên shop chỉ chứa chữ cái (a-z), số (0-9), gạch ngang (-) hoặc gạch dưới (_), tối đa 64 ký tự.');
      return;
    }
    setError(null);
    onSelectShop(clean);
  };

  return (
    <div className="min-h-screen bg-slate-900 flex flex-col justify-center items-center px-4 py-12 relative overflow-hidden">
      {/* Background ambient gradient glow */}
      <div className="absolute top-1/4 left-1/2 -translate-x-1/2 -translate-y-1/2 w-[600px] h-[400px] bg-blue-600/20 blur-[120px] rounded-full pointer-events-none" />
      <div className="absolute bottom-10 right-10 w-[300px] h-[300px] bg-indigo-600/15 blur-[100px] rounded-full pointer-events-none" />

      <div className="w-full max-w-xl relative z-10 space-y-6">
        {/* Brand Header */}
        <div className="text-center space-y-2">
          <div className="inline-flex h-14 w-14 items-center justify-center rounded-2xl bg-blue-600 text-white font-extrabold text-2xl shadow-lg shadow-blue-500/25 ring-4 ring-blue-500/20 mb-2">
            SC
          </div>
          <h1 className="text-3xl font-extrabold text-white tracking-tight sm:text-4xl">
            Supplier Catalog
          </h1>
          <p className="text-slate-400 text-sm max-w-md mx-auto">
            Hệ thống Đồng Bộ Danh Mục Nhà Cung Cấp & Multi-Tenant Demo Sandbox
          </p>
        </div>

        {/* Main Selection Card */}
        <Card className="border-slate-800 bg-slate-800/90 backdrop-blur shadow-2xl text-slate-100">
          <CardHeader className="space-y-1 pb-4">
            <div className="flex items-center justify-between">
              <CardTitle className="text-xl font-bold text-white flex items-center gap-2">
                <Store className="h-5 w-5 text-blue-400" />
                <span>Chọn hoặc Tạo Shop Demo</span>
              </CardTitle>
              <Badge variant="outline" className="text-emerald-400 border-emerald-500/30 bg-emerald-500/10 text-[10px] font-bold">
                100% ISOLATED
              </Badge>
            </div>
            <CardDescription className="text-slate-400 text-xs">
              Mỗi người vào sẽ sử dụng 1 Shop (Tenant) riêng để việc thử nghiệm và demo không bị ảnh hưởng lẫn nhau.
            </CardDescription>
          </CardHeader>

          <CardContent className="space-y-5">
            {/* 1-Click Fast Button */}
            <div className="p-4 rounded-xl border border-blue-500/30 bg-gradient-to-r from-blue-950/60 to-indigo-950/60 flex flex-col sm:flex-row sm:items-center justify-between gap-3">
              <div className="space-y-0.5">
                <div className="text-sm font-semibold text-white flex items-center gap-1.5">
                  <Sparkles className="h-4 w-4 text-amber-400" />
                  <span>Vào demo nhanh</span>
                </div>
                <p className="text-xs text-slate-400">
                  Tự động sinh 1 shop mới sạch sẽ và vào làm việc ngay
                </p>
              </div>
              <Button
                type="button"
                onClick={handleGenerateRandom}
                className="bg-blue-600 hover:bg-blue-500 text-white font-semibold shadow-md shadow-blue-600/30 shrink-0 gap-1.5"
              >
                <span>Tạo Shop Tự Động</span>
                <ArrowRight className="h-4 w-4" />
              </Button>
            </div>

            <div className="relative flex items-center justify-center">
              <div className="border-t border-slate-700/80 w-full" />
              <span className="bg-slate-800 px-3 text-[11px] font-semibold text-slate-500 uppercase tracking-wider">
                hoặc nhập tên shop tùy ý
              </span>
              <div className="border-t border-slate-700/80 w-full" />
            </div>

            {/* Manual Form */}
            <form onSubmit={handleSubmit} className="space-y-3">
              <div className="space-y-1.5">
                <Label htmlFor="shop-name-input" className="text-xs font-medium text-slate-300">
                  Tên Shop (Tenant ID)
                </Label>
                <div className="flex gap-2">
                  <Input
                    id="shop-name-input"
                    value={shopInput}
                    onChange={(e) => {
                      setShopInput(e.target.value);
                      setError(null);
                    }}
                    placeholder="VD: zara-store, apple-vietnam, test-01"
                    className="bg-slate-900 border-slate-700 text-white placeholder:text-slate-500 font-mono text-sm focus-visible:ring-blue-500"
                  />
                  <Button
                    type="submit"
                    variant="outline"
                    className="bg-slate-700 hover:bg-slate-600 text-white border-slate-600 shrink-0 font-semibold"
                  >
                    Vào Shop
                  </Button>
                </div>
                {error && <p className="text-xs text-rose-400 font-medium">{error}</p>}
              </div>
            </form>

            {/* Recent Shops */}
            {recentShops.length > 0 && (
              <div className="space-y-2 pt-2 border-t border-slate-700/60">
                <div className="flex items-center gap-1.5 text-xs font-medium text-slate-400">
                  <History className="h-3.5 w-3.5 text-slate-500" />
                  <span>Shop bạn đã từng truy cập:</span>
                </div>
                <div className="flex flex-wrap gap-2">
                  {recentShops.map((s) => (
                    <Badge
                      key={s}
                      variant="outline"
                      className="cursor-pointer bg-slate-900/80 hover:bg-blue-950 hover:border-blue-500 border-slate-700 text-slate-300 font-mono text-xs py-1 px-3 transition-colors flex items-center gap-1"
                      onClick={() => onSelectShop(s)}
                    >
                      <span>{s}</span>
                      <ArrowRight className="h-3 w-3 opacity-60" />
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>

        {/* Feature Guarantees */}
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 text-xs text-slate-400">
          <div className="flex items-center gap-2 p-3 rounded-lg border border-slate-800 bg-slate-800/40">
            <Database className="h-4 w-4 text-blue-400 shrink-0" />
            <span>SQLite WAL riêng biệt cho từng shop</span>
          </div>
          <div className="flex items-center gap-2 p-3 rounded-lg border border-slate-800 bg-slate-800/40">
            <Zap className="h-4 w-4 text-amber-400 shrink-0" />
            <span>Server-Sent Events streaming độc lập</span>
          </div>
          <div className="flex items-center gap-2 p-3 rounded-lg border border-slate-800 bg-slate-800/40">
            <Clock className="h-4 w-4 text-emerald-400 shrink-0" />
            <span>Tự động xóa shop sau 1 giờ idle</span>
          </div>
        </div>
      </div>
    </div>
  );
};
