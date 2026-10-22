import React from "react";
import {
  LayoutDashboard,
  Package,
  AlertTriangle,
  History,
  Layers,
  ChevronLeft,
  ChevronRight,
  Store,
} from "lucide-react";
import { Badge } from "../ui/badge";
import { Button } from "../ui/button";

export type NavItemKey = "overview" | "catalog" | "attention" | "history";

interface AppSidebarProps {
  activeTab: NavItemKey;
  onSelectTab: (tab: NavItemKey) => void;
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
      key: "overview" as NavItemKey,
      label: "Dashboard",
      icon: LayoutDashboard,
      badge: null,
    },
    {
      key: "catalog" as NavItemKey,
      label: "Catalog",
      icon: Package,
      badge: catalogCount > 0 ? catalogCount.toLocaleString() : null,
      badgeVariant: "secondary" as const,
    },
    {
      key: "attention" as NavItemKey,
      label: "Quarantine",
      icon: AlertTriangle,
      badge: quarantinedCount > 0 ? quarantinedCount : null,
      badgeVariant: quarantinedCount > 0 ? ("destructive" as const) : ("secondary" as const),
    },
    {
      key: "history" as NavItemKey,
      label: "History",
      icon: History,
      badge: historyCount > 0 ? historyCount : null,
      badgeVariant: "secondary" as const,
    },
  ];

  const handleNavClick = (key: NavItemKey) => {
    onSelectTab(key);
    onCloseMobile();
  };

  const sidebarContent = (
    <div className="flex flex-col h-full bg-white border-r border-slate-200/80 transition-all duration-300">
      {/* Brand Header */}
      {isCollapsed ? (
        <div className="h-14 flex items-center justify-center border-b border-slate-200/80">
          <button
            type="button"
            onClick={onToggleCollapse}
            className="h-8 w-8 rounded-lg bg-slate-900 hover:bg-slate-800 flex items-center justify-center text-white transition-all relative group cursor-pointer"
            title="Expand sidebar"
            aria-label="Expand sidebar"
          >
            <Layers className="h-4 w-4 transition-transform group-hover:scale-75 group-hover:opacity-0 absolute" />
            <ChevronRight className="h-4 w-4 transition-transform opacity-0 group-hover:opacity-100 group-hover:scale-100 absolute" />
          </button>
        </div>
      ) : (
        <div className="h-14 px-4 flex items-center justify-between border-b border-slate-200/80">
          <div className="flex items-center gap-2.5 overflow-hidden">
            <div className="h-8 w-8 rounded-lg bg-slate-900 flex items-center justify-center text-white shadow-xs flex-shrink-0">
              <Layers className="h-4 w-4" />
            </div>
            <span className="font-semibold text-sm text-slate-900 tracking-tight">
              Catalog
            </span>
          </div>

          <Button
            variant="ghost"
            size="sm"
            onClick={onToggleCollapse}
            className="hidden lg:flex h-7 w-7 p-0 rounded-md text-slate-400 hover:text-slate-700 hover:bg-slate-100"
            title="Collapse sidebar"
            aria-label="Collapse sidebar"
          >
            <ChevronLeft className="h-4 w-4" />
          </Button>
        </div>
      )}

      {/* Tenant Switcher */}
      {!isCollapsed ? (
        <div className="p-3 border-b border-slate-100 bg-slate-50/50">
          <button
            onClick={onOpenShopModal}
            className="w-full flex items-center justify-between p-2 rounded-lg bg-white border border-slate-200/80 shadow-xs hover:border-slate-300 transition-all text-left group"
          >
            <div className="flex items-center gap-2 overflow-hidden">
              <div className="h-6 w-6 rounded-md bg-slate-100 text-slate-700 flex items-center justify-center shrink-0">
                <Store className="h-3 w-3" />
              </div>
              <p className="text-xs font-medium text-slate-800 truncate">
                {activeShopId === "default" ? "Default Shop" : activeShopId}
              </p>
            </div>
            <span className="text-[10px] text-slate-400 group-hover:text-slate-700 font-medium">
              Switch
            </span>
          </button>
        </div>
      ) : (
        <div className="p-2 border-b border-slate-100 flex justify-center">
          <Button
            variant="ghost"
            size="sm"
            onClick={onOpenShopModal}
            className="h-8 w-8 p-0 rounded-md text-slate-600 hover:bg-slate-100"
            title="Switch Tenant Shop"
          >
            <Store className="h-4 w-4" />
          </Button>
        </div>
      )}

      {/* Navigation Links */}
      <div className="flex-1 overflow-y-auto px-2 py-3 space-y-1">
        {navItems.map((item) => {
          const Icon = item.icon;
          const isActive = activeTab === item.key;
          return (
            <button
              key={item.key}
              onClick={() => handleNavClick(item.key)}
              className={`w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-xs font-medium transition-all ${
                isActive
                  ? "bg-slate-100 text-slate-900 font-semibold"
                  : "text-slate-600 hover:text-slate-900 hover:bg-slate-50"
              } ${isCollapsed ? "justify-center px-0" : ""}`}
              title={isCollapsed ? item.label : undefined}
            >
              <Icon
                className={`h-4 w-4 flex-shrink-0 ${
                  isActive ? "text-slate-900" : "text-slate-400"
                }`}
              />
              {!isCollapsed && (
                <span className="flex-1 text-left truncate">{item.label}</span>
              )}
              {!isCollapsed && item.badge && (
                <Badge
                  variant={item.badgeVariant || "secondary"}
                  className="text-[10px] px-1.5 py-0 h-4 font-mono font-medium"
                >
                  {item.badge}
                </Badge>
              )}
            </button>
          );
        })}
      </div>

      {/* Footer Operator Info */}
      <div className="p-3 border-t border-slate-100 bg-slate-50/50">
        {!isCollapsed ? (
          <div className="flex items-center gap-2.5">
            <div className="h-7 w-7 rounded-full bg-slate-200 text-slate-700 flex items-center justify-center font-medium text-[11px] shrink-0">
              OP
            </div>
            <div className="flex flex-col min-w-0">
              <span className="text-xs font-medium text-slate-800 truncate">
                Operator
              </span>
              <span className="text-[10px] text-slate-400 truncate font-mono">
                catalog-admin
              </span>
            </div>
          </div>
        ) : (
          <div className="flex justify-center">
            <div className="h-6 w-6 rounded-full bg-slate-200 text-slate-700 flex items-center justify-center font-medium text-[10px]">
              OP
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

      {/* Mobile Sidebar Overlay */}
      {isMobileOpen && (
        <div
          className="fixed inset-0 z-40 bg-slate-900/20 backdrop-blur-xs lg:hidden"
          onClick={onCloseMobile}
        />
      )}

      {/* Mobile Sidebar Drawer */}
      <div
        className={`fixed inset-y-0 left-0 z-50 w-64 bg-white transform transition-transform duration-300 ease-in-out lg:hidden ${
          isMobileOpen ? "translate-x-0" : "-translate-x-full"
        }`}
      >
        {sidebarContent}
      </div>
    </>
  );
};
