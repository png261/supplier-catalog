import React from 'react';
import { Card } from './ui/card';
import { formatNumber } from '../lib/utils';
import { PackagePlus, RefreshCw, PackageX, CopyCheck, AlertOctagon } from 'lucide-react';

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
      bgClass: 'bg-emerald-50 text-emerald-700',
      icon: PackagePlus,
    },
    {
      key: 'updated' as const,
      title: 'Price / Name Updated',
      value: metrics.updated,
      delta: deltas.updated || 0,
      desc: 'Latest timestamps accepted',
      borderClass: 'hover:border-blue-500',
      bgClass: 'bg-blue-50 text-blue-700',
      icon: RefreshCw,
    },
    {
      key: 'withdrawn' as const,
      title: 'Withdrawn Products',
      value: metrics.withdrawn,
      delta: deltas.withdrawn || 0,
      desc: 'Tombstoned items hidden from sales',
      borderClass: 'hover:border-amber-500',
      bgClass: 'bg-amber-50 text-amber-700',
      icon: PackageX,
    },
    {
      key: 'skipped' as const,
      title: 'Duplicates Skipped',
      value: metrics.skipped,
      delta: deltas.skipped || 0,
      desc: 'Out-of-order & identical feeds ignored',
      borderClass: 'hover:border-slate-500',
      bgClass: 'bg-slate-100 text-slate-700',
      icon: CopyCheck,
    },
    {
      key: 'dead_letter' as const,
      title: 'Quarantined (DLQ)',
      value: metrics.dead_letter,
      delta: deltas.dead_letter || 0,
      desc: 'Invalid price, empty SKU, malformed',
      borderClass: 'hover:border-rose-500',
      bgClass: 'bg-rose-50 text-rose-700',
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
            className={`p-4 sm:p-4.5 border border-slate-200/80 bg-white transition-all duration-200 cursor-pointer shadow-xs hover:shadow-md hover:-translate-y-0.5 group ${c.borderClass}`}
          >
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-slate-600 truncate max-w-[130px]">
                {c.title}
              </span>
              <div className={`p-1.5 rounded-md ${c.bgClass}`}>
                <Icon className="h-3.5 w-3.5" />
              </div>
            </div>

            <div className="flex items-baseline justify-between gap-1">
              <span className="text-2xl sm:text-3xl font-extrabold text-slate-900 tracking-tight font-mono">
                {formatNumber(c.value)}
              </span>
              {c.delta > 0 && (
                <span className="text-[11px] font-bold text-emerald-600 font-mono bg-emerald-50 px-1.5 py-0.5 rounded">
                  +{formatNumber(c.delta)}
                </span>
              )}
            </div>

            <div className="mt-2 pt-2 border-t border-slate-100 flex items-center justify-between text-[11px] text-slate-400">
              <span className="truncate">{c.desc}</span>
              <span className="text-slate-600 font-medium opacity-0 group-hover:opacity-100 transition-opacity shrink-0 ml-1">
                Inspect →
              </span>
            </div>
          </Card>
        );
      })}
    </div>
  );
};
