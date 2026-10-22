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
import { Label } from './ui/label';
import { Badge } from './ui/badge';
import { Store, Sparkles, History, ShieldCheck, ArrowRight } from 'lucide-react';
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
    const prefixes = ['shop', 'store', 'outlet', 'boutique', 'mall'];
    const p = prefixes[Math.floor(Math.random() * prefixes.length)];
    const num = Math.floor(1000 + Math.random() * 9000);
    const newId = `${p}-${num}`;
    setInputShopId(newId);
    setError(null);
  };

  const handleSubmit = (e: React.FormEvent) => {
    e.preventDefault();
    const clean = inputShopId.trim().toLowerCase();
    if (!clean) {
      setError('Tên shop không được để trống.');
      return;
    }
    if (!/^[a-z0-9_-]{1,64}$/.test(clean)) {
      setError('Tên shop chỉ chứa chữ cái, số, dấu gạch ngang (-) hoặc gạch dưới (_), tối đa 64 ký tự.');
      return;
    }
    setError(null);
    onSelectShop(clean);
    onOpenChange(false);
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-blue-100 text-blue-700">
              <Store className="h-5 w-5" />
            </div>
            <div>
              <DialogTitle className="text-lg font-bold">Môi Trường Shop Demo Riêng</DialogTitle>
              <DialogDescription className="text-xs text-slate-500">
                Mỗi shop là 1 Database SQLite độc lập, tách biệt 100% dữ liệu.
              </DialogDescription>
            </div>
          </div>
        </DialogHeader>

        <form onSubmit={handleSubmit} className="space-y-4 py-2">
          {/* Isolation Guarantee Banner */}
          <div className="rounded-lg border border-emerald-200 bg-emerald-50/70 p-3 text-xs text-emerald-900 flex items-start gap-2">
            <ShieldCheck className="h-4 w-4 text-emerald-600 shrink-0 mt-0.5" />
            <span>
              <strong>Zero-impact sandbox:</strong> Mọi thao tác sync batch, xem dead-letter hoặc reset database chỉ áp dụng cho shop này. Shop sẽ <strong>tự động xóa sau 1 giờ</strong> không hoạt động.
            </span>
          </div>

          {/* Shop Input Field */}
          <div className="space-y-2">
            <Label htmlFor="shop-id" className="text-xs font-semibold text-slate-700">
              Tên / Định Danh Shop (Tenant ID)
            </Label>
            <div className="flex gap-2">
              <Input
                id="shop-id"
                value={inputShopId}
                onChange={(e) => {
                  setInputShopId(e.target.value);
                  setError(null);
                }}
                placeholder="VD: nike-store-88, tech-mart"
                className="font-mono text-sm"
              />
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={handleRandomize}
                className="shrink-0 gap-1 text-xs"
                title="Tạo tên shop ngẫu nhiên"
              >
                <Sparkles className="h-3.5 w-3.5 text-amber-500" />
                Ngẫu nhiên
              </Button>
            </div>
            {error && <p className="text-xs text-red-600 font-medium">{error}</p>}
          </div>

          {/* Recent Shops List */}
          {recentShops.length > 0 && (
            <div className="space-y-1.5 pt-1">
              <div className="flex items-center gap-1 text-xs text-slate-500 font-medium">
                <History className="h-3.5 w-3.5" />
                <span>Shop gần đây của bạn:</span>
              </div>
              <div className="flex flex-wrap gap-1.5">
                {recentShops.map((s) => (
                  <Badge
                    key={s}
                    variant={s === currentShopId ? 'default' : 'outline'}
                    className="cursor-pointer hover:bg-slate-100 font-mono text-[11px] py-1 px-2.5 transition-colors"
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

          <DialogFooter className="pt-2 sm:justify-between">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => onOpenChange(false)}
            >
              Hủy
            </Button>
            <Button type="submit" size="sm" className="gap-1.5 bg-blue-600 hover:bg-blue-700">
              Vào Shop Này
              <ArrowRight className="h-3.5 w-3.5" />
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
};
