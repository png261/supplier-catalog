import React from 'react';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { JobStatus } from '../types';
import { Activity, Play, RotateCcw, Wifi, WifiOff, Store, Copy, LogOut } from 'lucide-react';

interface HeaderProps {
  sseConnected: boolean;
  activeJob: { job_id: string; status: JobStatus } | null;
  shopId: string;
  onOpenShopModal: () => void;
  onShareShop: () => void;
  onLogoutShop: () => void;
  onOpenScenarioModal: () => void;
  onResetDatabase: () => void;
  isResetting: boolean;
}

export const Header: React.FC<HeaderProps> = ({
  sseConnected,
  activeJob,
  shopId,
  onOpenShopModal,
  onShareShop,
  onLogoutShop,
  onOpenScenarioModal,
  onResetDatabase,
  isResetting,
}) => {
  const isRunning = Boolean(activeJob && activeJob.status === 'RUNNING');
  const isInterrupted = Boolean(activeJob && activeJob.status === 'INTERRUPTED');

  return (
    <header className="sticky top-0 z-40 w-full border-b border-slate-200 bg-white/95 backdrop-blur shadow-sm">
      <div className="container max-w-7xl mx-auto flex h-16 items-center justify-between px-4 sm:px-6">
        {/* Brand & Identity */}
        <div className="flex items-center gap-3">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-blue-600 text-white font-extrabold text-lg shadow-sm">
            SC
          </div>
          <div>
            <div className="flex items-center gap-2">
              <h1 className="text-base font-bold text-slate-900 tracking-tight sm:text-lg">
                Supplier Catalog
              </h1>
              <Badge variant="secondary" className="hidden sm:inline-flex text-[10px] uppercase font-bold tracking-wider">
                Multi-Tenant Portal
              </Badge>
            </div>
            <p className="text-xs text-slate-500 hidden sm:block">
              Resilient SQLite WAL Supplier Catalog Ingestion & Quarantine Pipeline
            </p>
          </div>
        </div>

        {/* Status Indicators & Actions */}
        <div className="flex items-center gap-2 sm:gap-3">
          {/* Active Demo Shop Badge & Switcher */}
          {shopId && (
            <div className="flex items-center gap-1.5 px-2.5 py-1 rounded-lg border border-blue-200 bg-blue-50/80 text-xs shadow-xs">
              <Store className="h-3.5 w-3.5 text-blue-600 shrink-0" />
              <span className="text-slate-500 font-medium hidden md:inline">Shop:</span>
              <span className="font-bold text-blue-900 tracking-tight max-w-[130px] truncate" title={`Active Shop: ${shopId}`}>
                {shopId}
              </span>
              <button
                type="button"
                onClick={onShareShop}
                className="ml-1 p-0.5 text-blue-600 hover:text-blue-900 font-medium cursor-pointer"
                title="Sao chép link demo shop này"
              >
                <Copy className="h-3.5 w-3.5" />
              </button>
              <button
                type="button"
                onClick={onLogoutShop}
                className="ml-1.5 px-2 py-0.5 rounded text-[11px] font-semibold text-rose-700 bg-rose-50 hover:bg-rose-100 border border-rose-200 transition-colors flex items-center gap-1 cursor-pointer"
                title="Rời khỏi shop này và đổi/tạo shop khác"
              >
                <LogOut className="h-3 w-3" />
                <span>Rời Shop</span>
              </button>
            </div>
          )}

          {/* SSE Stream Status */}
          <div className="hidden md:flex items-center gap-1.5 px-2.5 py-1 rounded-full border border-slate-200 bg-slate-50 text-xs">
            {sseConnected ? (
              <>
                <span className="relative flex h-2 w-2">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                  <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                </span>
                <span className="text-slate-600 font-medium">Live</span>
              </>
            ) : (
              <>
                <WifiOff className="h-3 w-3 text-red-500" />
                <span className="text-red-600 font-medium">Off</span>
              </>
            )}
          </div>

          {/* Active Job Status Badge */}
          {isRunning && activeJob && (
            <Badge variant="info" className="animate-pulse flex items-center gap-1.5 hidden lg:inline-flex">
              <Activity className="h-3 w-3" />
              <span>Running</span>
            </Badge>
          )}
          {isInterrupted && (
            <Badge variant="warning" className="flex items-center gap-1.5 hidden lg:inline-flex">
              <span>Halted</span>
            </Badge>
          )}

          {/* Open Simulation Modal Button */}
          <Button
            variant="default"
            size="sm"
            onClick={onOpenScenarioModal}
            className="flex items-center gap-1.5 font-semibold bg-blue-600 hover:bg-blue-700 text-white shadow-sm"
          >
            <Play className="h-3.5 w-3.5 fill-current" />
            <span className="hidden sm:inline">Simulate Feed</span>
          </Button>

          {/* Reset Catalog Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onResetDatabase}
            disabled={isResetting || isRunning}
            className="text-slate-700 hover:text-red-700 hover:border-red-200"
            title="Reset catalog database của shop này"
          >
            <RotateCcw className={`h-3.5 w-3.5 ${isResetting ? 'animate-spin' : ''}`} />
            <span className="hidden sm:inline ml-1.5">Reset</span>
          </Button>
        </div>
      </div>
    </header>
  );
};
