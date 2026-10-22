import React from "react";
import {
  Menu,
  RotateCcw,
  Radio,
} from "lucide-react";
import { Button } from "../ui/button";
import { Badge } from "../ui/badge";

interface AppHeaderProps {
  onOpenMobileSidebar: () => void;
  onResetDatabase: () => void;
  activeJobId: string | null;
  isRunning?: boolean;
}

export const AppHeader: React.FC<AppHeaderProps> = ({
  onOpenMobileSidebar,
  onResetDatabase,
  activeJobId,
  isRunning,
}) => {
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
              className="inline-flex items-center gap-1 text-[11px] bg-slate-50 text-slate-700 border-slate-200 font-mono animate-pulse"
            >
              <Radio className="h-3 w-3 text-slate-600" />
              Syncing
            </Badge>
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
        </div>
      </div>
    </header>
  );
};
