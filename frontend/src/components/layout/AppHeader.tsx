import React from "react";
import {
  Menu,
  Play,
  RotateCcw,
  Radio,
  Search,
  ChevronRight,
  Database,
} from "lucide-react";
import { Button } from "../ui/button";
import { Badge } from "../ui/badge";
import { ThemeToggle } from "../ThemeToggle";
import { NavItemKey } from "./AppSidebar";

interface AppHeaderProps {
  activeTab: NavItemKey;
  onOpenMobileSidebar: () => void;
  openScenarioModal: (preselectedScenario?: string) => void;
  onResetDatabase: () => void;
  activeJobId: string | null;
  onSearchFocus?: () => void;
}

export const AppHeader: React.FC<AppHeaderProps> = ({
  activeTab,
  onOpenMobileSidebar,
  openScenarioModal,
  onResetDatabase,
  activeJobId,
}) => {
  const tabTitles: Record<NavItemKey, string> = {
    overview: "Dashboard Overview",
    catalog: "Live Catalog Explorer",
    attention: "Quarantined DLQ Attention",
    history: "Sync Run History",
    scenarios: "Supplier Workload Scenarios",
  };

  return (
    <header className="sticky top-0 z-20 h-16 bg-white/95 dark:bg-slate-900/95 backdrop-blur-md border-b border-slate-200 dark:border-slate-800 transition-colors">
      <div className="h-full px-4 lg:px-6 flex items-center justify-between gap-4">
        {/* Left: Mobile Menu Trigger & Breadcrumbs */}
        <div className="flex items-center gap-3">
          <Button
            variant="ghost"
            size="sm"
            onClick={onOpenMobileSidebar}
            className="lg:hidden h-9 w-9 p-0 rounded-md text-slate-600 dark:text-slate-300"
            aria-label="Open navigation menu"
          >
            <Menu className="h-5 w-5" />
          </Button>

          {/* Breadcrumbs matching shadcn-admin */}
          <nav className="hidden sm:flex items-center gap-1.5 text-xs text-slate-500 dark:text-slate-400 font-medium">
            <span className="hover:text-slate-700 dark:hover:text-slate-200">Catalog Engine</span>
            <ChevronRight className="h-3.5 w-3.5 text-slate-400 dark:text-slate-600" />
            <span className="text-slate-900 dark:text-slate-100 font-semibold">
              {tabTitles[activeTab]}
            </span>
          </nav>
        </div>

        {/* Center / Search bar trigger */}
        <div className="hidden md:flex items-center flex-1 max-w-md mx-4">
          <div className="relative w-full">
            <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-slate-400 dark:text-slate-500" />
            <input
              type="text"
              placeholder="Search catalog by SKU, product name..."
              className="w-full h-9 pl-9 pr-12 rounded-lg bg-slate-100/80 dark:bg-slate-800/80 border border-transparent dark:border-slate-700/50 text-xs text-slate-900 dark:text-slate-100 placeholder:text-slate-400 dark:placeholder:text-slate-500 focus:outline-none focus:bg-white dark:focus:bg-slate-800 focus:border-indigo-500 transition-all"
            />
            <kbd className="absolute right-2.5 top-1/2 -translate-y-1/2 text-[10px] bg-white dark:bg-slate-700 px-1.5 py-0.5 rounded border border-slate-200 dark:border-slate-600 text-slate-500 dark:text-slate-400 font-mono">
              ⌘K
            </kbd>
          </div>
        </div>

        {/* Right: Actions, Theme Toggle */}
        <div className="flex items-center gap-2 sm:gap-3">
          {/* Active Job badge */}
          {activeJobId && (
            <Badge
              variant="outline"
              className="hidden lg:inline-flex items-center gap-1 text-[11px] bg-amber-50 dark:bg-amber-950/50 text-amber-700 dark:text-amber-300 border-amber-300 dark:border-amber-700 font-mono animate-pulse"
            >
              <Radio className="h-3 w-3" />
              Sync In Flight
            </Badge>
          )}

          {/* Theme Toggle Component */}
          <ThemeToggle />

          {/* Reset Database Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onResetDatabase}
            className="h-8 text-xs font-medium text-slate-700 dark:text-slate-300 border-slate-200 dark:border-slate-800 hover:bg-slate-100 dark:hover:bg-slate-800 hover:text-slate-900 dark:hover:text-slate-100"
            title="Wipe and reset catalog database"
          >
            <RotateCcw className="h-3.5 w-3.5 sm:mr-1.5 text-slate-500 dark:text-slate-400" />
            <span className="hidden sm:inline">Reset</span>
          </Button>

          {/* Primary Action: Simulate Supplier Feed */}
          <Button
            size="sm"
            onClick={() => openScenarioModal()}
            className="h-8 text-xs font-semibold bg-indigo-600 hover:bg-indigo-700 text-white shadow-xs"
          >
            <Play className="h-3.5 w-3.5 sm:mr-1.5 fill-current" />
            <span className="hidden sm:inline">Simulate Feed</span>
            <span className="sm:hidden">Feed</span>
          </Button>
        </div>
      </div>
    </header>
  );
};
