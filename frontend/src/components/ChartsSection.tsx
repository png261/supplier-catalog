import React, { useState, useMemo, useEffect } from 'react';
import { Card, CardHeader, CardTitle, CardContent } from './ui/card';
import { Button } from './ui/button';
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
  Brush,
} from 'recharts';
import { formatNumber } from '../lib/utils';
import { PieChart as PieIcon, Package, ZoomIn, ZoomOut, RotateCcw, Activity } from 'lucide-react';

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

function formatTimeTick(isoStr: string): string {
  if (!isoStr) return '';
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return isoStr;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function formatSubSecondTick(isoStr: string): string {
  if (!isoStr) return '';
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return isoStr;
  const pad = (n: number) => String(n).padStart(2, '0');
  const ms = String(d.getMilliseconds()).padStart(3, '0').slice(0, 2);
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${ms}`;
}

function formatMultiDayTick(isoStr: string): string {
  if (!isoStr) return '';
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return isoStr;
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${pad(d.getMonth() + 1)}/${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function formatFullDateTime(isoStr: string): string {
  if (!isoStr) return '';
  const d = new Date(isoStr);
  if (isNaN(d.getTime())) return isoStr;
  const pad = (n: number) => String(n).padStart(2, '0');
  const day = pad(d.getDate());
  const month = pad(d.getMonth() + 1);
  const year = d.getFullYear();
  const hours = pad(d.getHours());
  const mins = pad(d.getMinutes());
  const secs = pad(d.getSeconds());
  const ms = String(d.getMilliseconds()).padStart(3, '0');
  return `${day}/${month}/${year} ${hours}:${mins}:${secs}.${ms}`;
}

export const ChartsSection: React.FC<ChartsSectionProps> = ({
  totals,
  timeSeries,
}) => {
  const pieData = [
    { name: 'Active Catalog', value: totals.active_products, color: '#10b981' },
    { name: 'Withdrawn', value: totals.withdrawn_products, color: '#f59e0b' },
    { name: 'Quarantined (DLQ)', value: totals.quarantined_issues, color: '#ef4444' },
  ].filter((d) => d.value > 0);

  const totalCatalogItems =
    totals.active_products + totals.withdrawn_products + totals.quarantined_issues;

  const timeResolution = useMemo(() => {
    if (!timeSeries || timeSeries.length < 2) return 'seconds';
    const first = new Date(timeSeries[0].timestamp);
    const last = new Date(timeSeries[timeSeries.length - 1].timestamp);
    if (first.toDateString() !== last.toDateString()) {
      return 'multi-day';
    }
    const diffMs = Math.abs(last.getTime() - first.getTime());
    if (diffMs < 5000) {
      return 'sub-second';
    }
    return 'seconds';
  }, [timeSeries]);

  const getTimeLabel = (timeStr: string) => {
    if (timeResolution === 'multi-day') return formatMultiDayTick(timeStr);
    if (timeResolution === 'sub-second') return formatSubSecondTick(timeStr);
    return formatTimeTick(timeStr);
  };

  // Construct chart data points from real timeSeries telemetry
  const chartData = useMemo(() => {
    if (!timeSeries || timeSeries.length === 0) {
      return [];
    }

    const pts = timeSeries.map((p, idx) => {
      const timeStr = p.timestamp || new Date().toISOString();
      const timeLabel = getTimeLabel(timeStr);
      const fullTime = formatFullDateTime(timeStr);
      return {
        rawTime: timeStr,
        timeLabel,
        fullTime,
        batchIndex: typeof p.batch_index === 'number' ? p.batch_index : idx,
        added: p.added,
        updated: p.updated,
        withdrawn: p.withdrawn,
        defects: p.dead_letter,
      };
    });

    // If only 1 point, prepend a zero baseline so a continuous gradient curve is rendered
    if (pts.length === 1) {
      const single = pts[0];
      const singleDate = new Date(single.rawTime);
      const baselineDate = new Date(singleDate.getTime() - 1000);
      const baselineTimeStr = baselineDate.toISOString();
      return [
        {
          rawTime: baselineTimeStr,
          timeLabel: getTimeLabel(baselineTimeStr),
          fullTime: formatFullDateTime(baselineTimeStr) + ' (Sync Init)',
          batchIndex: Math.max(0, single.batchIndex - 1),
          added: 0,
          updated: 0,
          withdrawn: 0,
          defects: 0,
        },
        single,
      ];
    }

    return pts;
  }, [timeSeries, timeResolution]);

  // Zoom / Brush range state
  const [brushRange, setBrushRange] = useState<{ startIndex: number; endIndex: number }>({
    startIndex: 0,
    endIndex: 0,
  });

  useEffect(() => {
    if (chartData.length > 0) {
      setBrushRange({
        startIndex: 0,
        endIndex: chartData.length - 1,
      });
    }
  }, [chartData.length]);

  const isZoomed =
    chartData.length > 2 &&
    (brushRange.startIndex > 0 || brushRange.endIndex < chartData.length - 1);

  const handleZoomIn = () => {
    const count = brushRange.endIndex - brushRange.startIndex + 1;
    if (count <= 2) return;
    const delta = Math.max(1, Math.floor(count * 0.25));
    setBrushRange((prev) => ({
      startIndex: Math.min(prev.startIndex + delta, prev.endIndex - 1),
      endIndex: Math.max(prev.endIndex - delta, prev.startIndex + 1),
    }));
  };

  const handleZoomOut = () => {
    const count = brushRange.endIndex - brushRange.startIndex + 1;
    const delta = Math.max(1, Math.floor(count * 0.35));
    setBrushRange((prev) => ({
      startIndex: Math.max(0, prev.startIndex - delta),
      endIndex: Math.min(chartData.length - 1, prev.endIndex + delta),
    }));
  };

  const handleResetZoom = () => {
    setBrushRange({
      startIndex: 0,
      endIndex: Math.max(0, chartData.length - 1),
    });
  };

  return (
    <div className="grid grid-cols-1 lg:grid-cols-12 gap-4">
      {/* Donut Chart: Catalog Distribution */}
      <Card className="lg:col-span-5 border-slate-200/80 bg-white shadow-xs flex flex-col">
        <CardHeader className="p-4 pb-2 flex flex-row items-center justify-between space-y-0">
          <CardTitle className="text-sm font-bold text-slate-900 flex items-center gap-2">
            <PieIcon className="h-4 w-4 text-emerald-600" />
            <span>Catalog Distribution</span>
          </CardTitle>
          <span className="text-xs text-slate-500 font-medium">
            Active Breakdown
          </span>
        </CardHeader>

        <CardContent className="p-4 pt-1 flex-1 flex flex-col justify-between">
          <div className="h-48 w-full relative flex items-center justify-center">
            {pieData.length === 0 ? (
              <div className="text-center text-xs text-slate-400 space-y-1">
                <PieIcon className="h-7 w-7 text-slate-200 mx-auto" />
                <p>No catalog items loaded yet.</p>
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
                      <Cell key={`cell-${index}`} fill={entry.color} stroke="#ffffff" strokeWidth={2} />
                    ))}
                  </Pie>
                  <RechartsTooltip
                    formatter={(val: number) => [formatNumber(val), 'Count']}
                    contentStyle={{
                      backgroundColor: '#0f172a',
                      borderRadius: '6px',
                      color: '#ffffff',
                      fontSize: '12px',
                      border: 'none',
                    }}
                  />
                </PieChart>
              </ResponsiveContainer>
            )}

            {/* Total items badge inside Donut hole */}
            {pieData.length > 0 && (
              <div className="absolute inset-0 flex flex-col items-center justify-center pointer-events-none">
                <span className="text-lg font-extrabold text-slate-900 font-mono">
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
                <span className="h-2 w-2 rounded-full bg-emerald-500"></span>
                <span>Active</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 font-mono">
                {formatNumber(totals.active_products)}
              </strong>
            </div>
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500">
                <span className="h-2 w-2 rounded-full bg-amber-500"></span>
                <span>Withdrawn</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 font-mono">
                {formatNumber(totals.withdrawn_products)}
              </strong>
            </div>
            <div>
              <div className="flex items-center justify-center gap-1.5 text-xs text-slate-500">
                <span className="h-2 w-2 rounded-full bg-red-500"></span>
                <span>Quarantined</span>
              </div>
              <strong className="text-xs font-bold text-slate-800 font-mono">
                {formatNumber(totals.quarantined_issues)}
              </strong>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* Area Chart: Product Changes & Synchronization Activity */}
      <Card className="lg:col-span-7 border-slate-200/80 bg-white shadow-xs flex flex-col">
        <CardHeader className="p-4 pb-2 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 space-y-0">
          <CardTitle className="text-sm font-bold text-slate-900 flex items-center gap-2">
            <Package className="h-4 w-4 text-indigo-600" />
            <span>Product Ingestion & Updates Activity</span>
          </CardTitle>

          <div className="flex flex-wrap items-center gap-3">
            {/* Zoom Controls */}
            {chartData.length > 2 && (
              <div className="flex items-center gap-1 bg-slate-50 border border-slate-200 rounded-md p-0.5">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleZoomIn}
                  disabled={brushRange.endIndex - brushRange.startIndex <= 1}
                  className="h-6 w-6 p-0 text-slate-600 hover:text-slate-900 hover:bg-slate-200/60"
                  title="Zoom In"
                  aria-label="Zoom In"
                >
                  <ZoomIn className="h-3.5 w-3.5" />
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={handleZoomOut}
                  disabled={!isZoomed}
                  className="h-6 w-6 p-0 text-slate-600 hover:text-slate-900 hover:bg-slate-200/60"
                  title="Zoom Out"
                  aria-label="Zoom Out"
                >
                  <ZoomOut className="h-3.5 w-3.5" />
                </Button>
                {isZoomed && (
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={handleResetZoom}
                    className="h-6 px-1.5 text-[11px] text-indigo-600 font-medium hover:bg-indigo-50"
                    title="Reset Zoom to 100%"
                  >
                    <RotateCcw className="h-3 w-3 mr-1" />
                    Reset
                  </Button>
                )}
              </div>
            )}

            {/* Legend */}
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
          </div>
        </CardHeader>

        <CardContent className="p-4 pt-1 flex-1 flex flex-col justify-center">
          {chartData.length === 0 ? (
            <div className="h-[280px] w-full flex flex-col items-center justify-center text-center text-xs text-slate-400 space-y-1.5 border border-dashed border-slate-200/80 rounded-lg bg-slate-50/40 px-4">
              <Activity className="h-7 w-7 text-slate-300" />
              <p className="font-medium text-slate-600">No Ingestion Telemetry Recorded</p>
              <p className="text-slate-400 max-w-xs">
                Trigger any catalog workload scenario above to stream real-time batch progression and delta curves.
              </p>
            </div>
          ) : (
            <div className="h-[280px] w-full min-h-[280px]">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartData} margin={{ top: 10, right: 10, left: -20, bottom: 5 }}>
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
                    dataKey="timeLabel"
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
                    content={({ active, payload, label }) => {
                      if (!active || !payload || !payload.length) return null;
                      const data = payload[0].payload;
                      return (
                        <div className="bg-slate-900 text-white rounded-md p-2.5 shadow-lg text-xs space-y-1.5 border border-slate-700 min-w-[170px]">
                          <div className="font-semibold text-slate-200 border-b border-slate-700 pb-1 flex items-center justify-between gap-2">
                            <span>{data.fullTime || label}</span>
                            {typeof data.batchIndex === 'number' && (
                              <span className="text-[10px] text-slate-400 font-mono">Batch #{data.batchIndex + 1}</span>
                            )}
                          </div>
                          <div className="space-y-1 pt-0.5">
                            <div className="flex items-center justify-between text-emerald-400">
                              <span className="flex items-center gap-1.5">
                                <span className="h-1.5 w-1.5 rounded-full bg-emerald-400"></span>
                                <span>Added</span>
                              </span>
                              <span className="font-mono font-bold">{formatNumber(data.added || 0)}</span>
                            </div>
                            <div className="flex items-center justify-between text-indigo-400">
                              <span className="flex items-center gap-1.5">
                                <span className="h-1.5 w-1.5 rounded-full bg-indigo-400"></span>
                                <span>Updated</span>
                              </span>
                              <span className="font-mono font-bold">{formatNumber(data.updated || 0)}</span>
                            </div>
                            <div className="flex items-center justify-between text-amber-400">
                              <span className="flex items-center gap-1.5">
                                <span className="h-1.5 w-1.5 rounded-full bg-amber-400"></span>
                                <span>Withdrawn</span>
                              </span>
                              <span className="font-mono font-bold">{formatNumber(data.withdrawn || 0)}</span>
                            </div>
                          </div>
                        </div>
                      );
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
                  {chartData.length > 2 && (
                    <Brush
                      dataKey="timeLabel"
                      height={24}
                      stroke="#6366f1"
                      fill="#f8fafc"
                      startIndex={brushRange.startIndex}
                      endIndex={brushRange.endIndex}
                      onChange={(range: any) => {
                        if (typeof range.startIndex === 'number' && typeof range.endIndex === 'number') {
                          setBrushRange({ startIndex: range.startIndex, endIndex: range.endIndex });
                        }
                      }}
                      travellerWidth={8}
                    />
                  )}
                </AreaChart>
              </ResponsiveContainer>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  );
};
