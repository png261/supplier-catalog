import React from 'react';
import { Card } from './ui/card';
import { formatNumber } from '../lib/utils';
import { PackagePlus, RefreshCw, PackageX, CopyCheck, AlertOctagon, ArrowUpRight } from 'lucide-react';

interface MetricsGridProps {
  metrics: {
    added: number;
    updated: number;
    withdrawn: number;
    skipped: number;
    dead_letter: number;
  };
  deltas: {
    added?: number;
    updated?: number;
    withdrawn?: number;
    skipped?: number;
    dead_letter?: number;
  };
  onCardClick: (category: 'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter') => void;
}

export const MetricsGrid: React.FC<MetricsGridProps> = ({
  metrics,
  deltas,
  onCardClick,
}) => {
  const cards = [
    {
      key: 'added' as const,
      title: 'Products Added',
      value: metrics.added,
      delta: deltas.added || 0,
      desc: 'New items inserted into catalog',
      borderClass: 'hover:border-emerald-500',
      textClass: 'text-emerald-600 dark:text-emerald-400',
      bgClass: 'bg-emerald-50 dark:bg-emerald-950/60 text-emerald-700 dark:text-emerald-300',
      icon: PackagePlus,
    },
    {
      key: 'updated' as const,
      title: 'Price / Name Updated',
      value: metrics.updated,
      delta: deltas.updated || 0,
      desc: 'Latest timestamps accepted',
      borderClass: 'hover:border-blue-500',
      textClass: 'text-blue-600 dark:text-blue-400',
      bgClass: 'bg-blue-50 dark:bg-blue-950/60 text-blue-700 dark:text-blue-300',
      icon: RefreshCw,
    },
    {
      key: 'withdrawn' as const,
      title: 'Withdrawn Products',
      value: metrics.withdrawn,
      delta: deltas.withdrawn || 0,
      desc: 'Tombstoned items hidden from sales',
      borderClass: 'hover:border-amber-500',
      textClass: 'text-amber-600 dark:text-amber-400',
      bgClass: 'bg-amber-50 dark:bg-amber-950/60 text-amber-700 dark:text-amber-300',
      icon: PackageX,
    },
    {
      key: 'skipped' as const,
      title: 'Duplicates Skipped',
      value: metrics.skipped,
      delta: deltas.skipped || 0,
      desc: 'Out-of-order & identical feeds ignored',
      borderClass: 'hover:border-slate-500',
      textClass: 'text-slate-600 dark:text-slate-400',
      bgClass: 'bg-slate-100 dark:bg-slate-800 text-slate-700 dark:text-slate-300',
      icon: CopyCheck,
    },
    {
      key: 'dead_letter' as const,
      title: 'Quarantined (DLQ)',
      value: metrics.dead_letter,
      delta: deltas.dead_letter || 0,
      desc: 'Invalid price, empty SKU, malformed',
      borderClass: 'hover:border-red-500',
      textClass: 'text-red-600 dark:text-red-400',
      bgClass: 'bg-red-50 dark:bg-red-950/60 text-red-700 dark:text-red-300',
      icon: AlertOctagon,
    },
  ];

  return (
    <div className="grid grid-cols-2 md:grid-cols-3 lg:grid-cols-5 gap-3 sm:gap-4">
      {cards.map((c) => {
        const Icon = c.icon;
        return (
          <Card
            key={c.key}
            onClick={() => onCardClick(c.key)}
            className={`p-4 sm:p-4.5 border border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 transition-all duration-200 cursor-pointer shadow-xs hover:shadow-md hover:-translate-y-0.5 group ${c.borderClass}`}
          >
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-slate-600 dark:text-slate-400 truncate max-w-[120px]">
                {c.title}
              </span>
              <div className={`p-1.5 rounded-md ${c.bgClass}`}>
                <Icon className="h-3.5 w-3.5" />
              </div>
            </div>

            <div className="flex items-baseline justify-between gap-1 mb-1.5">
              <span className="text-2xl sm:text-3xl font-extrabold text-slate-900 dark:text-slate-100 tracking-tight font-mono">
                {formatNumber(c.value)}
              </span>
              {c.delta > 0 && (
                <span className="text-[11px] font-bold text-emerald-600 dark:text-emerald-400 font-mono bg-emerald-50 dark:bg-emerald-950/60 px-1.5 py-0.5 rounded">
                  +{formatNumber(c.delta)}
                </span>
              )}
            </div>

            <p className="text-[11px] text-slate-500 dark:text-slate-400 line-clamp-1 mb-2">
              {c.desc}
            </p>

            <div className="flex items-center justify-end text-[11px] font-medium text-slate-400 dark:text-slate-500 group-hover:text-indigo-600 dark:group-hover:text-indigo-400 transition-colors">
              <span>View records</span>
              <ArrowUpRight className="h-3 w-3 ml-0.5" />
            </div>
          </Card>
        );
      })}
    </div>
  );
};
