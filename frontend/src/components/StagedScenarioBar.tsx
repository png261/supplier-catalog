import React from 'react';
import { Card } from './ui/card';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { TestScenario } from '../types';
import { Play, RotateCw, Square, SlidersHorizontal, AlertTriangle, CheckCircle2 } from 'lucide-react';

interface StagedScenarioBarProps {
  scenario: TestScenario;
  isSyncing: boolean;
  isInterrupted: boolean;
  onStartSync: () => void;
  onCancelSync: () => void;
  onOpenScenarioModal: () => void;
}

export const StagedScenarioBar: React.FC<StagedScenarioBarProps> = ({
  scenario,
  isSyncing,
  isInterrupted,
  onStartSync,
  onCancelSync,
  onOpenScenarioModal,
}) => {
  const getStatusBadge = () => {
    if (isSyncing) {
      return (
        <Badge variant="info" className="flex items-center gap-1">
          <RotateCw className="h-3 w-3 animate-spin" />
          <span>Syncing...</span>
        </Badge>
      );
    }
    if (isInterrupted) {
      return (
        <Badge variant="warning" className="flex items-center gap-1">
          <AlertTriangle className="h-3 w-3" />
          <span>Interrupted (Checkpoint Saved)</span>
        </Badge>
      );
    }
    return (
      <Badge variant="success" className="flex items-center gap-1">
        <CheckCircle2 className="h-3 w-3" />
        <span>Ready to Sync</span>
      </Badge>
    );
  };

  return (
    <Card className="p-4 sm:p-5 border-slate-200 bg-white shadow-sm overflow-hidden">
      <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
        {/* Left: Scenario Info */}
        <div className="flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-bold uppercase tracking-wider text-slate-500">
              Active Workload Scenario:
            </span>
            <h2 className="text-base sm:text-lg font-bold text-slate-900 tracking-tight">
              {scenario.name}
            </h2>
            <Badge variant="outline" className="font-mono text-xs bg-slate-50">
              {scenario.batches} Batches • {scenario.volume.toLocaleString()} Records
            </Badge>
            {getStatusBadge()}
          </div>
          <p className="text-xs text-slate-600 line-clamp-2 max-w-3xl leading-relaxed">
            {scenario.desc}
          </p>
        </div>

        {/* Right: Actions */}
        <div className="flex items-center gap-2.5 shrink-0 self-start lg:self-center">
          <Button
            variant="outline"
            size="sm"
            onClick={onOpenScenarioModal}
            disabled={isSyncing}
            className="flex items-center gap-1.5 text-xs text-slate-700"
          >
            <SlidersHorizontal className="h-3.5 w-3.5" />
            <span>Select Scenario</span>
          </Button>

          {isSyncing ? (
            <Button
              variant="destructive"
              size="sm"
              onClick={onCancelSync}
              className="flex items-center gap-1.5 font-semibold text-xs"
            >
              <Square className="h-3 w-3 fill-current" />
              <span>Stop Sync</span>
            </Button>
          ) : (
            <Button
              variant="primary"
              size="sm"
              onClick={onStartSync}
              className={`flex items-center gap-1.5 font-semibold text-xs shadow-sm ${
                isInterrupted ? 'bg-amber-600 hover:bg-amber-700 text-white' : 'bg-blue-600 hover:bg-blue-700 text-white'
              }`}
            >
              {isInterrupted ? (
                <>
                  <RotateCw className="h-3.5 w-3.5" />
                  <span>Resume Sync</span>
                </>
              ) : (
                <>
                  <Play className="h-3.5 w-3.5 fill-current" />
                  <span>Start Sync</span>
                </>
              )}
            </Button>
          )}
        </div>
      </div>

      {/* Mini Details Strip */}
      <div className="mt-3.5 pt-3 border-t border-slate-100 grid grid-cols-2 sm:grid-cols-4 gap-2 text-xs">
        <div>
          <span className="text-slate-400">Total Volume: </span>
          <strong className="text-slate-700 font-semibold">{scenario.volume.toLocaleString()} items</strong>
        </div>
        <div>
          <span className="text-slate-400">Batch Structure: </span>
          <strong className="text-slate-700 font-semibold">{scenario.batches} batches ({scenario.size}/batch)</strong>
        </div>
        <div>
          <span className="text-slate-400">Feed Quality: </span>
          <strong className="text-slate-700 font-semibold">{scenario.quality}</strong>
        </div>
        <div>
          <span className="text-slate-400">Storage Mode: </span>
          <strong className="text-slate-700 font-semibold font-mono text-[11px]">SQLite WAL Atomic</strong>
        </div>
      </div>
    </Card>
  );
};
