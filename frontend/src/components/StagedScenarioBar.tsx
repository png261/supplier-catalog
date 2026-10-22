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
    <Card className="p-4 sm:p-5 border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 shadow-xs overflow-hidden transition-colors">
      <div className="flex flex-col lg:flex-row lg:items-center lg:justify-between gap-4">
        {/* Left: Scenario Info */}
        <div className="flex-1 space-y-1.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-xs font-bold uppercase tracking-wider text-slate-500 dark:text-slate-400">
              Active Workload Scenario:
            </span>
            <h2 className="text-base sm:text-lg font-bold text-slate-900 dark:text-slate-100 tracking-tight">
              {scenario.name}
            </h2>
            {getStatusBadge()}
          </div>

          <p className="text-xs sm:text-sm text-slate-600 dark:text-slate-400 max-w-3xl leading-relaxed">
            {scenario.desc}
          </p>

          <div className="flex flex-wrap items-center gap-4 pt-1 text-xs text-slate-500 dark:text-slate-400">
            <span className="flex items-center gap-1.5 font-mono">
              <span className="font-semibold text-slate-700 dark:text-slate-300">Batches:</span> {scenario.batches}
            </span>
            <span className="flex items-center gap-1.5 font-mono">
              <span className="font-semibold text-slate-700 dark:text-slate-300">Volume:</span> ~{scenario.volume} records
            </span>
            <span className="flex items-center gap-1.5 font-mono">
              <span className="font-semibold text-slate-700 dark:text-slate-300">Defect Ratio:</span> {scenario.invalidRatio}%
            </span>
            {scenario.key === 'interrupted' && (
              <span className="flex items-center gap-1.5 font-mono text-amber-600 dark:text-amber-400 font-semibold">
                Crash Simulation: Auto-halts at Batch 5/10
              </span>
            )}
          </div>
        </div>

        {/* Right: Actions */}
        <div className="flex flex-wrap items-center gap-2.5 sm:self-end lg:self-center">
          <Button
            variant="outline"
            size="sm"
            onClick={onOpenScenarioModal}
            className="h-9 px-3.5 text-xs font-semibold border-slate-300 dark:border-slate-700 hover:bg-slate-50 dark:hover:bg-slate-800 text-slate-700 dark:text-slate-200"
          >
            <SlidersHorizontal className="h-3.5 w-3.5 mr-1.5 text-slate-500 dark:text-slate-400" />
            Select Scenario
          </Button>

          {isSyncing ? (
            <Button
              variant="destructive"
              size="sm"
              onClick={onCancelSync}
              className="h-9 px-4 text-xs font-semibold shadow-xs"
            >
              <Square className="h-3.5 w-3.5 mr-1.5 fill-current" />
              Stop Sync
            </Button>
          ) : isInterrupted ? (
            <Button
              variant="default"
              size="sm"
              onClick={onStartSync}
              className="h-9 px-4 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs"
            >
              <RotateCw className="h-3.5 w-3.5 mr-1.5" />
              Resume Sync
            </Button>
          ) : (
            <Button
              variant="default"
              size="sm"
              onClick={onStartSync}
              className="h-9 px-4 text-xs font-semibold bg-indigo-600 hover:bg-indigo-700 text-white shadow-xs"
            >
              <Play className="h-3.5 w-3.5 mr-1.5 fill-current" />
              Start Sync
            </Button>
          )}
        </div>
      </div>
    </Card>
  );
};
