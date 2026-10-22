import React from "react";
import {
  Menu,
  Play,
  RotateCcw,
  Radio,
  ChevronRight,
  SlidersHorizontal,
} from "lucide-react";
import { Button } from "../ui/button";
import { Badge } from "../ui/badge";
import { NavItemKey } from "./AppSidebar";
import { TestScenario } from "../../types";

interface AppHeaderProps {
  activeTab: NavItemKey;
  onOpenMobileSidebar: () => void;
  openScenarioModal: (preselectedScenario?: string) => void;
  onResetDatabase: () => void;
  activeJobId: string | null;
  stagedScenario?: TestScenario;
  onStartSync?: () => void;
  isRunning?: boolean;
}

export const AppHeader: React.FC<AppHeaderProps> = ({
  activeTab,
  onOpenMobileSidebar,
  openScenarioModal,
  onResetDatabase,
  activeJobId,
  stagedScenario,
  onStartSync,
  isRunning,
}) => {
  const tabTitles: Record<NavItemKey, string> = {
    overview: "Dashboard",
    catalog: "Catalog",
    attention: "Quarantine",
    history: "History",
  };

  const getShortScenarioName = (s: TestScenario) => {
    switch (s.key) {
      case 'default':
        return 'Standard';
      case 'clean':
        return 'Clean';
      case 'stress':
        return 'Stress';
      case 'large':
        return 'High-Volume';
      case 'interrupted':
        return 'Recovery';
      default:
        return s.name.split(' ')[0] || 'Default';
    }
  };

  return (
    <header className="sticky top-0 z-20 h-14 bg-white/95 backdrop-blur-md border-b border-slate-200/80 transition-colors">
      <div className="h-full px-4 lg:px-6 flex items-center justify-between gap-3">
        {/* Left: Mobile Menu Trigger */}
        <div className="flex items-center">
          <Button
            variant="ghost"
            size="sm"
            onClick={onOpenMobileSidebar}
            className="lg:hidden h-8 w-8 p-0 text-slate-600 hover:bg-slate-100"
            aria-label="Open navigation menu"
          >
            <Menu className="h-4 w-4" />
          </Button>
        </div>

        {/* Right: Minimal Actions & Controls */}
        <div className="flex items-center gap-2">
          {/* Active Job badge */}
          {activeJobId && (
            <Badge
              variant="outline"
              className="hidden sm:inline-flex items-center gap-1 text-[11px] bg-slate-50 text-slate-700 border-slate-200 font-mono animate-pulse"
            >
              <Radio className="h-3 w-3 text-slate-600" />
              Syncing
            </Badge>
          )}

          {/* Staged Scenario Selector Pill */}
          {stagedScenario && (
            <button
              type="button"
              onClick={() => openScenarioModal(stagedScenario.key)}
              className="flex items-center gap-1.5 h-8 px-2.5 rounded-lg border border-slate-200 bg-slate-50 hover:bg-slate-100 text-xs font-medium text-slate-700 transition-colors cursor-pointer"
              title={`Active Scenario: ${stagedScenario.name} (${stagedScenario.batches} batches). Click to switch.`}
            >
              <SlidersHorizontal className="h-3 w-3 text-slate-500" />
              <span className="font-medium text-slate-800">
                {getShortScenarioName(stagedScenario)}
              </span>
              <span className="text-[10px] text-slate-400 font-mono">
                {stagedScenario.batches}b
              </span>
            </button>
          )}

          {/* Reset Database Button */}
          <Button
            variant="outline"
            size="sm"
            onClick={onResetDatabase}
            disabled={isRunning || Boolean(activeJobId)}
            className="h-8 px-2.5 text-xs font-medium text-slate-600 border-slate-200 hover:bg-slate-50 hover:text-slate-900"
            title="Reset catalog database"
          >
            <RotateCcw className="h-3 w-3 sm:mr-1 text-slate-400" />
            <span className="hidden sm:inline">Reset</span>
          </Button>

          {/* Primary Action: Simulate */}
          <Button
            size="sm"
            onClick={onStartSync || (() => openScenarioModal())}
            disabled={isRunning || Boolean(activeJobId)}
            className="h-8 px-3 text-xs font-medium bg-slate-900 hover:bg-slate-800 text-white shadow-none"
            title={stagedScenario ? `Start simulation with ${stagedScenario.name}` : "Simulate feed"}
          >
            <Play className="h-3 w-3 sm:mr-1 fill-current" />
            <span>Simulate</span>
          </Button>
        </div>
      </div>
    </header>
  );
};
