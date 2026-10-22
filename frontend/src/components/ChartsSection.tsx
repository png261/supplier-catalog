import React from 'react';
import { Card, CardHeader, CardTitle, CardContent } from './ui/card';
import {
  PieChart,
  Pie,
  Cell,
  Tooltip as RechartsTooltip,
  ResponsiveContainer,
  AreaChart,
  Area,
  XAxis,
  YAxis,
  CartesianGrid,
} from 'recharts';
import { formatNumber } from '../lib/utils';
import { PieChart as PieIcon, Package } from 'lucide-react';

interface ChartsSectionProps {
  totals: {
    active_products: number;
    withdrawn_products: number;
    quarantined_issues: number;
    total_events_processed: number;
    defect_rate: number;
  };
  timeSeries: {
    timestamp: string;
    batch_index: number;
    added: number;
    updated: number;
    withdrawn: number;
    skipped: number;
    dead_letter: number;
    processing_speed: number;
  }[];
}

export const ChartsSection: React.FC<ChartsSectionProps> = ({
  totals,
  timeSeries,
}) => {
  const isDark = false; // Light mode only

  const pieData = [
    { name: 'Active Catalog', value: totals.active_products, color: '#10b981' },
    { name: 'Withdrawn', value: totals.withdrawn_products, color: '#f59e0b' },
    { name: 'Quarantined (DLQ)', value: totals.quarantined_issues, color: '#ef4444' },
  ].filter((d) => d.value > 0);

  const totalCatalogItems =
    totals.active_products + totals.withdrawn_products + totals.quarantined_issues;

  // Prepare product timeSeries or sample product progression points
  const chartData =
    timeSeries && timeSeries.length > 0
      ? timeSeries.map((p) => ({
          name: `Batch ${p.batch_index + 1}`,
          added: p.added,
          updated: p.updated,
          withdrawn: p.withdrawn,
          defects: p.dead_letter,
        }))
      : [
          { name: 'Batch 1', added: 120, updated: 35, withdrawn: 5, defects: 0 },
          { name: 'Batch 2', added: 280, updated: 85, withdrawn: 15, defects: 2 },
          { name: 'Batch 3', added: 450, updated: 140, withdrawn: 25, defects: 5 },
          { name: 'Batch 4', added: 390, updated: 110, withdrawn: 18, defects: 1 },
          { name: 'Batch 5', added: 520, updated: 165, withdrawn: 30, defects: 3 },
        ];

  return (
    <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
      {/* Donut Chart: Catalog Distribution */}
      <Card className="lg:col-span-5 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs flex flex-col transition-colors">
        <CardHeader className="p-4 pb-2 flex flex-row items-center justify-between space-y-0">
          <CardTitle className="text-sm font-bold text-slate-900 dark:text-slate-100 flex items-center gap-2">
            <PieIcon className="h-4 w-4 text-emerald-600 dark:text-emerald-400" />
            <span>Catalog Distribution</span>
          </CardTitle>
          <span className="text-xs text-slate-500 dark:text-slate-400 font-medium">
            Active Breakdown
          </span>
        </CardHeader>

        <CardContent className="p-4 pt-1 flex-1 flex flex-col justify-between">
          <div className="h-48 w-full relative flex items-center justify-center">
            {pieData.length === 0 ? (
              <div className="text-center text-xs text-slate-400 dark:text-slate-500">
                No catalog items loaded yet.
              </div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <Pie
                    data={pieData}
                    cx="50%"
                    cy="50%"
                    innerRadius={52}
                    outerRadius={75}
                    paddingAngle={3}
                    dataKey="value"
                  >
                    {pieData.map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.color} stroke={isDark ? '#0f172a' : '#ffffff'} strokeWidth={2} />
                    ))}
                  </Pie>
                  <RechartsTooltip
                    formatter={(val: number) => [formatNumber(val), 'Count']}
                    contentStyle={{
                      backgroundColor: isDark ? '#1e293b' : '#0f172a',
                      borderRadius: '6px',
                      color: '#ffffff',
                      fontSize: '12px',
                      border: isDark ? '1px solid #334155' : 'none',
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
            )}

            {/* Total items badge inside Donut hole */}
            {pieData.length > 0 && (
              <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                <span className="text-lg font-extrabold text-slate-900 dark:text-slate-100 font-mono">
                  {formatNumber(totalCatalogItems)}
                </span>
                <span className="text-[10px] uppercase font-bold text-slate-400 dark:text-slate-500 tracking-wider">
                  Total Items
                </span>
              </div>
            )}
          </div>

          {/* Custom Legend */}
          <div className="grid grid-cols-3 gap-2 mt-2 pt-2 border-t border-slate-100 dark:border-slate-800 text-center">
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500 dark:text-slate-400">
                <span className="h-2 w-2 rounded-full bg-emerald-500"></span>
                <span>Active</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 dark:text-slate-200 font-mono">
                {formatNumber(totals.active_products)}
              </strong>
            </div>
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500 dark:text-slate-400">
                <span className="h-2 w-2 rounded-full bg-amber-500"></span>
                <span>Withdrawn</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 dark:text-slate-200 font-mono">
                {formatNumber(totals.withdrawn_products)}
              </strong>
            </div>
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500 dark:text-slate-400">
                <span className="h-2 w-2 rounded-full bg-red-500"></span>
                <span>Quarantined</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 dark:text-slate-200 font-mono">
                {formatNumber(totals.quarantined_issues)}
              </strong>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Area Chart: Product Changes & Synchronization Activity */}
      <Card className="lg:col-span-7 border-slate-200 bg-white shadow-xs flex flex-col">
        <CardHeader className="p-4 pb-2 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-2 space-y-0">
          <CardTitle className="text-sm font-bold text-slate-900 flex items-center gap-2">
            <Package className="h-4 w-4 text-indigo-600" />
            <span>Product Ingestion & Updates Activity</span>
          </CardTitle>
          <div className="flex items-center gap-3 text-xs">
            <div className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-emerald-500"></span>
              <span className="text-slate-600 font-medium">Added</span>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-indigo-500"></span>
              <span className="text-slate-600 font-medium">Updated</span>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="h-2 w-2 rounded-full bg-amber-500"></span>
              <span className="text-slate-600 font-medium">Withdrawn</span>
            </div>
          </div>
        </CardHeader>

        <CardContent className="p-4 pt-1 flex-1 flex flex-col justify-center">
          <div className="h-60 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                <defs>
                  <linearGradient id="addedGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#10b981" stopOpacity={0.35} />
                    <stop offset="95%" stopColor="#10b981" stopOpacity={0.0} />
                  </linearGradient>
                  <linearGradient id="updatedGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#6366f1" stopOpacity={0.35} />
                    <stop offset="95%" stopColor="#6366f1" stopOpacity={0.0} />
                  </linearGradient>
                  <linearGradient id="withdrawnGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#f59e0b" stopOpacity={0.35} />
                    <stop offset="95%" stopColor="#f59e0b" stopOpacity={0.0} />
                  </linearGradient>
                </defs>
                <CartesianGrid
                  strokeDasharray="3 3"
                  vertical={false}
                  stroke="#f1f5f9"
                />
                <XAxis
                  dataKey="name"
                  tickLine={false}
                  axisLine={{ stroke: '#cbd5e1' }}
                  tick={{ fill: '#64748b', fontSize: 11 }}
                />
                <YAxis
                  tickLine={false}
                  axisLine={{ stroke: '#cbd5e1' }}
                  tick={{ fill: '#64748b', fontSize: 11 }}
                  tickFormatter={(v) => `${v}`}
                />
                <RechartsTooltip
                  formatter={(val: number, name: string) => [
                    `${formatNumber(val)} products`,
                    name,
                  ]}
                  contentStyle={{
                    backgroundColor: '#0f172a',
                    borderRadius: '6px',
                    color: '#ffffff',
                    fontSize: '12px',
                    border: 'none',
                    boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)',
                  }}
                />
                <Area
                  type="monotone"
                  dataKey="added"
                  name="Products Added"
                  stroke="#10b981"
                  strokeWidth={2}
                  fillOpacity={1}
                  fill="url(#addedGradient)"
                />
                <Area
                  type="monotone"
                  dataKey="updated"
                  name="Price / Name Updated"
                  stroke="#6366f1"
                  strokeWidth={2}
                  fillOpacity={1}
                  fill="url(#updatedGradient)"
                />
                <Area
                  type="monotone"
                  dataKey="withdrawn"
                  name="Withdrawn Products"
                  stroke="#f59e0b"
                  strokeWidth={2}
                  fillOpacity={1}
                  fill="url(#withdrawnGradient)"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};
