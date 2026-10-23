import React, { useState } from "react";
import {
  Menu,
  RotateCcw,
  Radio,
  AlertTriangle,
  RefreshCw,
} from "lucide-react";
import { Button } from "../ui/button";
import { Badge } from "../ui/badge";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "../ui/dialog";
import { NavItemKey } from "./AppSidebar";

interface AppHeaderProps {
  activeTab: NavItemKey;
  activeShopId: string;
  onOpenMobileSidebar: () => void;
  onResetDatabase: () => Promise<void> | void;
  activeJobId: string | null;
  isRunning?: boolean;
  isResettingDb?: boolean;
  sseConnected?: boolean;
  currentBatch?: number;
  totalBatches?: number;
  onNavigateToOverview?: () => void;
}

const TAB_METADATA: Record<NavItemKey, { title: string; subtitle: string }> = {
  overview: {
    title: "Dashboard & Sync Control",
    subtitle: "Trigger supplier workloads and monitor real-time ingestion telemetry",
  },
  catalog: {
    title: "Live Product Catalog",
    subtitle: "Inspect active and withdrawn SKUs synchronized for this tenant",
  },
  attention: {
    title: "Quarantine (Dead-Letter Queue)",
    subtitle: "Review and remediate malformed or invalid supplier records",
  },
  history: {
    title: "Sync Run Audit History",
    subtitle: "Session logs, batch checkpoints, and crash recovery controls",
  },
  versions: {
    title: "Catalog Versions & Rollback",
    subtitle: "Milestone journaling, SKU pre-image diffs, and atomic Git-revert",
  },
};

export const AppHeader: React.FC<AppHeaderProps> = ({
  activeTab,
  activeShopId,
  onOpenMobileSidebar,
  onResetDatabase,
  activeJobId,
  isRunning,
  isResettingDb = false,
  sseConnected = false,
  currentBatch = 0,
  totalBatches = 10,
  onNavigateToOverview,
}) => {
  const [isResetDialogOpen, setIsResetDialogOpen] = useState(false);
  const meta = TAB_METADATA[activeTab] || TAB_METADATA.overview;
  const percent =
    totalBatches > 0 ? Math.min(100, Math.round((currentBatch / totalBatches) * 100)) : 0;

  const handleConfirmReset = async () => {
    await onResetDatabase();
    setIsResetDialogOpen(false);
  };

  return (
    <>
      <header className="sticky top-0 z-20 h-14 bg-white/95 backdrop-blur-md border-b border-slate-200/80 transition-colors">
        <div className="h-full px-4 lg:px-6 flex items-center justify-between gap-3">
          {/* Left: Mobile Menu Trigger + Page Context Title */}
          <div className="flex items-center gap-3 min-w-0">
            <Button
              variant="ghost"
              size="sm"
              onClick={onOpenMobileSidebar}
              className="lg:hidden h-8 w-8 p-0 text-slate-600 hover:bg-slate-100 shrink-0"
              aria-label="Open navigation menu"
            >
              <Menu className="h-4 w-4" />
            </Button>

            <div className="min-w-0">
              <div className="flex items-center gap-2">
                <h1 className="text-sm font-semibold text-slate-900 truncate">
                  {meta.title}
                </h1>
                <span className="hidden sm:inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-mono font-medium bg-slate-100 text-slate-600">
                  {activeShopId || "default"}
                </span>
              </div>
              <p className="hidden md:block text-[11px] text-slate-400 truncate">
                {meta.subtitle}
              </p>
            </div>
          </div>

          {/* Right: Live Telemetry Status & Controls */}
          <div className="flex items-center gap-2 shrink-0">
            {/* Active Job Progress Pill */}
            {activeJobId && (
              <button
                type="button"
                onClick={onNavigateToOverview}
                className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-[11px] bg-blue-50 text-blue-700 border border-blue-200 font-mono hover:bg-blue-100/80 transition-colors cursor-pointer"
                title="Click to view live sync progress on Dashboard"
              >
                <Radio className="h-3 w-3 text-blue-600 animate-pulse" />
                <span className="font-semibold">
                  Syncing {currentBatch}/{totalBatches}
                </span>
                <span className="text-blue-500">({percent}%)</span>
              </button>
            )}

            {/* SSE Stream Connection Indicator */}
            <div
              className="hidden sm:inline-flex items-center gap-1.5 px-2.5 py-1 rounded-md border border-slate-200/80 bg-slate-50/70 text-[11px] text-slate-600 font-medium"
              title={
                sseConnected
                  ? "Real-time Server-Sent Events stream connected"
                  : "Polling fallback active (SSE reconnecting)"
              }
            >
              <span
                className={`h-2 w-2 rounded-full ${
                  sseConnected ? "bg-emerald-500" : "bg-amber-400 animate-pulse"
                }`}
              />
              <span>{sseConnected ? "Live SSE" : "Sync Ready"}</span>
            </div>

            {/* Reset Database Button */}
            <Button
              variant="outline"
              size="sm"
              onClick={() => setIsResetDialogOpen(true)}
              disabled={isRunning || Boolean(activeJobId) || isResettingDb}
              className="h-8 px-2.5 text-xs font-medium text-slate-600 border-slate-200 hover:bg-rose-50 hover:text-rose-700 hover:border-rose-200 transition-colors"
              title="Reset tenant catalog database"
            >
              <RotateCcw
                className={`h-3 w-3 sm:mr-1 text-slate-400 ${
                  isResettingDb ? "animate-spin" : ""
                }`}
              />
              <span className="hidden sm:inline">
                {isResettingDb ? "Resetting..." : "Reset DB"}
              </span>
            </Button>
          </div>
        </div>
      </header>

      {/* Reset Database Confirmation Modal */}
      <Dialog open={isResetDialogOpen} onOpenChange={setIsResetDialogOpen}>
        <DialogContent className="sm:max-w-md bg-white">
          <DialogHeader>
            <div className="flex items-center gap-2.5 text-rose-600 mb-1">
              <div className="h-8 w-8 rounded-full bg-rose-50 border border-rose-200 flex items-center justify-center shrink-0">
                <AlertTriangle className="h-4 w-4 text-rose-600" />
              </div>
              <DialogTitle className="text-base font-semibold text-slate-900">
                Reset Tenant Catalog Database?
              </DialogTitle>
            </div>
            <DialogDescription className="text-xs text-slate-500 pt-1">
              This will wipe all synchronized products, quarantined DLQ issues, sync run logs, and version milestones for tenant{" "}
              <strong className="font-mono text-slate-800">{activeShopId}</strong>.
            </DialogDescription>
          </DialogHeader>

          <div className="p-3 rounded-lg bg-slate-50 border border-slate-200/80 text-xs text-slate-600 space-y-1">
            <p className="font-semibold text-slate-800">What happens on Reset:</p>
            <ul className="list-disc pl-4 space-y-0.5 text-[11px] text-slate-500">
              <li>All 5 workload scenarios are unlocked to run fresh from v1.0.0.</li>
              <li>Other tenant stores remain 100% untouched (strict tenant isolation).</li>
            </ul>
          </div>

          <DialogFooter className="flex sm:justify-end gap-2 pt-2">
            <Button
              variant="outline"
              size="sm"
              onClick={() => setIsResetDialogOpen(false)}
              disabled={isResettingDb}
              className="h-8 text-xs"
            >
              Cancel
            </Button>
            <Button
              variant="destructive"
              size="sm"
              onClick={handleConfirmReset}
              disabled={isResettingDb}
              className="h-8 bg-rose-600 hover:bg-rose-700 text-white text-xs font-semibold"
            >
              {isResettingDb ? (
                <>
                  <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                  Resetting...
                </>
              ) : (
                "Confirm Reset Database"
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
};
