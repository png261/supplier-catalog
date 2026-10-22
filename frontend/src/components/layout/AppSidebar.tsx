import React from "react";
import {
  LayoutDashboard,
  BarChart3,
  Package,
  AlertTriangle,
  History,
  PlayCircle,
  FileCode,
  Layers,
  ChevronLeft,
  ChevronRight,
  Store,
  CheckCircle2,
  Database,
} from "lucide-react";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";

export type NavItemKey = "overview" | "catalog" | "attention" | "history" | "scenarios";

interface AppSidebarProps {
  activeTab: NavItemKey;
  onSelectTab: (tab: NavItemKey) => void;
  openScenarioModal: (preselectedScenario?: string) => void;
  isCollapsed: boolean;
  onToggleCollapse: () => void;
  isMobileOpen: boolean;
  onCloseMobile: () => void;
  catalogCount: number;
  quarantinedCount: number;
  historyCount: number;
  activeShopId: string;
  onOpenShopModal: () => void;
}

export const AppSidebar: React.FC<AppSidebarProps> = ({
  activeTab,
  onSelectTab,
  openScenarioModal,
  isCollapsed,
  onToggleCollapse,
  isMobileOpen,
  onCloseMobile,
  catalogCount,
  quarantinedCount,
  historyCount,
  activeShopId,
  onOpenShopModal,
}) => {
  const navItems = [
    {
      group: "Overview",
      items: [
        {
          key: "overview" as NavItemKey,
          label: "Dashboard",
          icon: LayoutDashboard,
          badge: null,
        },
        {
          key: "scenarios" as NavItemKey,
          label: "Supplier Scenarios",
          icon: PlayCircle,
          badge: "6 tests",
          badgeVariant: "outline" as const,
        },
      ],
    },
    {
      group: "Catalog Management",
      items: [
        {
          key: "catalog" as NavItemKey,
          label: "Live Catalog",
          icon: Package,
          badge: catalogCount > 0 ? catalogCount.toLocaleString() : null,
          badgeVariant: "secondary" as const,
        },
        {
          key: "attention" as NavItemKey,
          label: "Attention / DLQ",
          icon: AlertTriangle,
          badge: quarantinedCount > 0 ? quarantinedCount : null,
          badgeVariant: quarantinedCount > 0 ? ("destructive" as const) : ("secondary" as const),
        },
        {
          key: "history" as NavItemKey,
          label: "Sync Run History",
          icon: History,
          badge: historyCount > 0 ? historyCount : null,
          badgeVariant: "secondary" as const,
        },
      ],
    },
  ];

  const handleNavClick = (key: NavItemKey) => {
    onSelectTab(key);
    onCloseMobile();
  };

  const sidebarContent = (
    <div className="flex flex-col h-full bg-white dark:bg-slate-900 border-r border-slate-200 dark:border-slate-800 transition-all duration-300">
      {/* Brand Header */}
      <div className="h-16 px-4 flex items-center justify-between border-b border-slate-200 dark:border-slate-800">
        <div className="flex items-center gap-3 overflow-hidden">
          <div className="h-9 w-9 rounded-lg bg-indigo-600 flex items-center justify-center text-white shadow-sm flex-shrink-0">
            <Layers className="h-5 w-5" />
          </div>
          {!isCollapsed && (
            <div className="flex flex-col min-w-0">
              <span className="font-bold text-sm text-slate-900 dark:text-slate-100 tracking-tight truncate">
                Coderpush Engine
              </span>
              <span className="text-xs text-slate-500 dark:text-slate-400 font-mono truncate">
                WAL Resilient Sync
              </span>
            </div>
          )}
        </div>

        {/* Collapse button on desktop */}
        <Button
          variant="ghost"
          size="sm"
          onClick={onToggleCollapse}
          className="hidden lg:flex h-7 w-7 p-0 rounded-md text-slate-500 hover:text-slate-900 dark:hover:text-slate-100 hover:bg-slate-100 dark:hover:bg-slate-800"
          title={isCollapsed ? "Expand sidebar" : "Collapse sidebar"}
        >
          {isCollapsed ? <ChevronRight className="h-4 w-4" /> : <ChevronLeft className="h-4 w-4" />}
        </Button>
      </div>

      {/* Shop Switcher / Multi-Tenant Widget */}
      {!isCollapsed ? (
        <div className="p-3 border-b border-slate-100 dark:border-slate-800/80 bg-slate-50/50 dark:bg-slate-900/50">
          <button
            onClick={onOpenShopModal}
            className="w-full flex items-center justify-between p-2 rounded-lg bg-white dark:bg-slate-800/80 border border-slate-200 dark:border-slate-700/60 shadow-xs hover:border-indigo-300 dark:hover:border-indigo-600 transition-all text-left group"
          >
            <div className="flex items-center gap-2.5 overflow-hidden">
              <div className="h-7 w-7 rounded-md bg-indigo-50 dark:bg-indigo-950/60 text-indigo-600 dark:text-indigo-400 flex items-center justify-center flex-shrink-0">
                <Store className="h-3.5 w-3.5" />
              </div>
              <div className="min-w-0">
                <p className="text-xs font-semibold text-slate-800 dark:text-slate-200 truncate">
                  {activeShopId === "default" ? "Default Production Shop" : `Shop: ${activeShopId}`}
                </p>
                <p className="text-[10px] text-emerald-600 dark:text-emerald-400 font-medium flex items-center gap-1">
                  <span className="h-1.5 w-1.5 rounded-full bg-emerald-500 inline-block animate-pulse"></span>
                  SQLite WAL Active
                </p>
              </div>
            </div>
            <span className="text-[10px] text-indigo-600 dark:text-indigo-400 font-semibold group-hover:underline">
              Switch
            </span>
          </button>
        </div>
      ) : (
        <div className="p-2 border-b border-slate-100 dark:border-slate-800/80 flex justify-center">
          <Button
            variant="ghost"
            size="sm"
            onClick={onOpenShopModal}
            className="h-8 w-8 p-0 rounded-md text-indigo-600 dark:text-indigo-400 hover:bg-indigo-50 dark:hover:bg-indigo-950/50"
            title="Switch Tenant Shop"
          >
            <Store className="h-4 w-4" />
          </Button>
        </div>
      )}

      {/* Navigation Links */}
      <div className="flex-1 overflow-y-auto px-2 py-3 space-y-4">
        {navItems.map((group, idx) => (
          <div key={idx} className="space-y-1">
            {!isCollapsed && (
              <h4 className="px-2.5 text-[11px] font-semibold tracking-wider text-slate-400 dark:text-slate-500 uppercase">
                {group.group}
              </h4>
            )}
            <div className="space-y-0.5">
              {group.items.map((item) => {
                const Icon = item.icon;
                const isActive = activeTab === item.key;
                return (
                  <button
                    key={item.key}
                    onClick={() => handleNavClick(item.key)}
                    className={`w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-sm font-medium transition-all ${
                      isActive
                        ? "bg-indigo-50 dark:bg-indigo-950/60 text-indigo-700 dark:text-indigo-300 font-semibold shadow-xs"
                        : "text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100 hover:bg-slate-100/70 dark:hover:bg-slate-800/60"
                    } ${isCollapsed ? "justify-center px-0" : ""}`}
                    title={isCollapsed ? item.label : undefined}
                  >
                    <Icon
                      className={`h-4 w-4 flex-shrink-0 ${
                        isActive
                          ? "text-indigo-600 dark:text-indigo-400"
                          : "text-slate-500 dark:text-slate-400"
                      }`}
                    />
                    {!isCollapsed && (
                      <span className="flex-1 text-left truncate">{item.label}</span>
                    )}
                    {!isCollapsed && item.badge && (
                      <Badge
                        variant={item.badgeVariant || "secondary"}
                        className="text-[10px] px-1.5 py-0.5 h-4.5 font-semibold"
                      >
                        {item.badge}
                      </Badge>
                    )}
                  </button>
                );
              })}
            </div>
          </div>
        ))}

        {/* Quick Operations Group */}
        <div className="space-y-1 pt-2 border-t border-slate-100 dark:border-slate-800">
          {!isCollapsed && (
            <h4 className="px-2.5 text-[11px] font-semibold tracking-wider text-slate-400 dark:text-slate-500 uppercase">
              Operations
            </h4>
          )}
          <div className="space-y-0.5">
            <button
              onClick={() => {
                openScenarioModal("custom");
                onCloseMobile();
              }}
              className={`w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-sm font-medium text-slate-600 dark:text-slate-400 hover:text-slate-900 dark:hover:text-slate-100 hover:bg-slate-100/70 dark:hover:bg-slate-800/60 transition-all ${
                isCollapsed ? "justify-center px-0" : ""
              }`}
              title={isCollapsed ? "Custom JSON Ingestion" : undefined}
            >
              <FileCode className="h-4 w-4 flex-shrink-0 text-slate-500 dark:text-slate-400" />
              {!isCollapsed && <span className="flex-1 text-left truncate">Custom JSON Feed</span>}
            </button>
          </div>
        </div>
      </div>

      {/* Footer / User Profile Card */}
      <div className="p-3 border-t border-slate-200 dark:border-slate-800 bg-slate-50/60 dark:bg-slate-900/60">
        {!isCollapsed ? (
          <div className="flex items-center gap-3">
            <div className="h-8 w-8 rounded-full bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 flex items-center justify-center font-bold text-xs ring-1 ring-slate-300 dark:ring-slate-700 flex-shrink-0">
              ST
            </div>
            <div className="flex flex-col min-w-0">
              <span className="text-xs font-semibold text-slate-900 dark:text-slate-100 truncate">
                Staff Operator
              </span>
              <span className="text-[11px] text-slate-500 dark:text-slate-400 truncate font-mono">
                catalog-admin
              </span>
            </div>
          </div>
        ) : (
          <div className="flex justify-center">
            <div className="h-7 w-7 rounded-full bg-slate-200 dark:bg-slate-800 text-slate-700 dark:text-slate-300 flex items-center justify-center font-bold text-xs">
              ST
            </div>
          </div>
        )}
      </div>
    </div>
  );

  return (
    <>
      {/* Desktop Sidebar */}
      <aside
        className={`hidden lg:block fixed inset-y-0 left-0 z-30 transition-all duration-300 ${
          isCollapsed ? "w-16" : "w-64"
        }`}
      >
        {sidebarContent}
      </aside>

      {/* Mobile Drawer Backdrop & Sidebar */}
      {isMobileOpen && (
        <div
          className="lg:hidden fixed inset-0 z-50 bg-black/60 backdrop-blur-xs transition-opacity"
          onClick={onCloseMobile}
        >
          <div
            className="fixed inset-y-0 left-0 w-64 max-w-[80vw] bg-white dark:bg-slate-900 shadow-xl"
            onClick={(e) => e.stopPropagation()}
          >
            {sidebarContent}
          </div>
        </div>
      )}
    </>
  );
};
