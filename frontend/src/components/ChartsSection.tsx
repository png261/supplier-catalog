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
import { PieChart as PieIcon, TrendingUp } from 'lucide-react';

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
  const pieData = [
    { name: 'Active Catalog', value: totals.active_products, color: '#059669' },
    { name: 'Withdrawn', value: totals.withdrawn_products, color: '#d97706' },
    { name: 'Quarantined (DLQ)', value: totals.quarantined_issues, color: '#dc2626' },
  ].filter((d) => d.value > 0);

  const totalCatalogItems =
    totals.active_products + totals.withdrawn_products + totals.quarantined_issues;

  // Prepare timeSeries or mock points if empty
  const chartData =
    timeSeries && timeSeries.length > 0
      ? timeSeries.map((p) => ({
          name: `B-${p.batch_index + 1}`,
          throughput: p.processing_speed || (p.added + p.updated + p.withdrawn + p.skipped + p.dead_letter) * 10,
          events: p.added + p.updated + p.withdrawn + p.skipped + p.dead_letter,
          defects: p.dead_letter,
        }))
      : [
          { name: 'B-1', throughput: 850, events: 100, defects: 5 },
          { name: 'B-2', throughput: 1120, events: 100, defects: 3 },
          { name: 'B-3', throughput: 1340, events: 100, defects: 6 },
          { name: 'B-4', throughput: 990, events: 100, defects: 4 },
          { name: 'B-5', throughput: 1250, events: 100, defects: 2 },
        ];

  return (
    <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
      {/* Donut Chart: Catalog Distribution */}
      <Card className="lg:col-span-5 border-slate-200 bg-white shadow-sm flex flex-col">
        <CardHeader className="p-4 pb-2 flex flex-row items-center justify-between space-y-0">
          <CardTitle className="text-sm font-bold text-slate-900 flex items-center gap-2">
            <PieIcon className="h-4 w-4 text-emerald-600" />
            <span>Catalog Distribution & Health</span>
          </CardTitle>
          <span className="text-xs text-slate-500 font-medium">
            Defect Rate: <strong className="text-slate-800">{(totals.defect_rate || 0).toFixed(1)}%</strong>
          </span>
        </CardHeader>

        <CardContent className="p-4 pt-0 flex-1 flex flex-col justify-center">
          <div className="relative h-56 w-full flex items-center justify-center">
            {totalCatalogItems === 0 ? (
              <div className="text-xs text-slate-400">No catalog data recorded yet.</div>
            ) : (
              <ResponsiveContainer width="100%" height="100%">
                <PieChart>
                  <RechartsTooltip
                    formatter={(val: number) => [`${formatNumber(val)} items`, '']}
                    contentStyle={{
                      backgroundColor: '#0f172a',
                      borderRadius: '6px',
                      color: '#ffffff',
                      fontSize: '12px',
                      border: 'none',
                    }}
                  />
                  <Pie
                    data={pieData}
                    cx="50%"
                    cy="50%"
                    innerRadius={65}
                    outerRadius={88}
                    paddingAngle={3}
                    dataKey="value"
                  >
                    {pieData.map((entry, index) => (
                      <Cell key={`cell-${index}`} fill={entry.color} />
                    ))}
                  </Pie>
                </PieChart>
              </ResponsiveContainer>
            )}

            {/* Total count centered in donut */}
            {totalCatalogItems > 0 && (
              <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                <span className="text-xl font-extrabold text-slate-900 font-mono">
                  {formatNumber(totalCatalogItems)}
                </span>
                <span className="text-[10px] uppercase font-bold text-slate-400 tracking-wider">
                  Total Items
                </span>
              </div>
            )}
          </div>

          {/* Custom Legend */}
          <div className="grid grid-cols-3 gap-2 mt-2 pt-2 border-t border-slate-100 text-center">
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500">
                <span className="h-2 w-2 rounded-full bg-emerald-600"></span>
                <span>Active</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 font-mono">
                {formatNumber(totals.active_products)}
              </strong>
            </div>
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500">
                <span className="h-2 w-2 rounded-full bg-amber-600"></span>
                <span>Withdrawn</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 font-mono">
                {formatNumber(totals.withdrawn_products)}
              </strong>
            </div>
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500">
                <span className="h-2 w-2 rounded-full bg-red-600"></span>
                <span>Quarantined</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 font-mono">
                {formatNumber(totals.quarantined_issues)}
              </strong>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Area Chart: Ingestion Velocity & Throughput */}
      <Card className="lg:col-span-7 border-slate-200 bg-white shadow-sm flex flex-col">
        <CardHeader className="p-4 pb-2 flex flex-row items-center justify-between space-y-0">
          <CardTitle className="text-sm font-bold text-slate-900 flex items-center gap-2">
            <TrendingUp className="h-4 w-4 text-blue-600" />
            <span>Ingestion Velocity & Throughput (rec/s)</span>
          </CardTitle>
          <span className="text-xs text-slate-500 font-medium">
            WAL Concurrency Stream
          </span>
        </CardHeader>

        <CardContent className="p-4 pt-1 flex-1 flex flex-col justify-center">
          <div className="h-60 w-full">
            <ResponsiveContainer width="100%" height="100%">
              <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 0 }}>
                <defs>
                  <linearGradient id="velocityGradient" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.4} />
                    <stop offset="95%" stopColor="#3b82f6" stopOpacity={0.0} />
                  </linearGradient>
                </defs>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke="#e2e8f0" />
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
                  formatter={(val: number) => [`${formatNumber(val)} rec/s`, 'Throughput']}
                  contentStyle={{
                    backgroundColor: '#0f172a',
                    borderRadius: '6px',
                    color: '#ffffff',
                    fontSize: '12px',
                    border: 'none',
                  }}
                />
                <Area
                  type="monotone"
                  dataKey="throughput"
                  stroke="#2563eb"
                  strokeWidth={2}
                  fillOpacity={1}
                  fill="url(#velocityGradient)"
                />
              </AreaChart>
            </ResponsiveContainer>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};
