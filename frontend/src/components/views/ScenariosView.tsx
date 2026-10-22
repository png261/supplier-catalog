import React from 'react';
import { Card } from '../ui/card';
import { Button } from '../ui/button';
import { Badge } from '../ui/badge';
import { StagedScenarioBar } from '../StagedScenarioBar';
import { TestScenario } from '../../types';
import { SCENARIOS } from '../DemoModal';
import { Play, PlayCircle, SlidersHorizontal, FileCode } from 'lucide-react';

interface ScenariosViewProps {
  stagedScenario: TestScenario;
  isSyncing: boolean;
  isInterrupted: boolean;
  onStartSync: () => void;
  onCancelSync: () => void;
  onSelectScenario: (scenario: TestScenario) => void;
  onStartImmediately: (scenario: TestScenario) => void;
  onOpenScenarioModal: (preselectedKey?: string) => void;
}

export const ScenariosView: React.FC<ScenariosViewProps> = ({
  stagedScenario,
  isSyncing,
  isInterrupted,
  onStartSync,
  onCancelSync,
  onSelectScenario,
  onStartImmediately,
  onOpenScenarioModal,
}) => {
  return (
    <div className="space-y-5">
      {/* Active Workload Action Bar */}
      <StagedScenarioBar
        scenario={stagedScenario}
        isSyncing={isSyncing}
        isInterrupted={isInterrupted}
        onStartSync={onStartSync}
        onCancelSync={onCancelSync}
        onOpenScenarioModal={() => onOpenScenarioModal()}
      />

      {/* Scenarios Header */}
      <div className="flex items-center justify-between pt-1">
        <div>
          <h3 className="text-base font-bold text-slate-900 dark:text-slate-100 flex items-center gap-2">
            <PlayCircle className="h-4.5 w-4.5 text-indigo-600 dark:text-indigo-400" />
            <span>Select & Execute Test Scenarios</span>
          </h3>
          <p className="text-xs text-slate-500 dark:text-slate-400 mt-0.5">
            Click any workload card below to stage it, or run immediately to benchmark SQLite WAL concurrency.
          </p>
        </div>

        <Button
          variant="outline"
          size="sm"
          onClick={() => onOpenScenarioModal('custom')}
          className="h-8 text-xs font-semibold border-slate-300 dark:border-slate-700"
        >
          <FileCode className="h-3.5 w-3.5 mr-1.5 text-indigo-600 dark:text-indigo-400" />
          Direct JSON Editor
        </Button>
      </div>

      {/* 6 Scenario Cards Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {SCENARIOS.map((sc) => {
          const isStaged = stagedScenario.key === sc.key;
          return (
            <Card
              key={sc.key}
              onClick={() => onSelectScenario(sc)}
              className={`p-4.5 border transition-all cursor-pointer flex flex-col justify-between shadow-xs hover:shadow-md ${
                isStaged
                  ? 'border-indigo-600 dark:border-indigo-500 bg-indigo-50/40 dark:bg-indigo-950/20 ring-1 ring-indigo-600'
                  : 'border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-slate-300 dark:hover:border-slate-700'
              }`}
            >
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="font-bold text-sm text-slate-900 dark:text-slate-100 tracking-tight">
                    {sc.name}
                  </span>
                  {isStaged && (
                    <Badge variant="success" className="text-[10px] px-1.5 py-0">
                      Active Staged
                    </Badge>
                  )}
                </div>
                <p className="text-xs text-slate-600 dark:text-slate-400 line-clamp-3 leading-relaxed">
                  {sc.desc}
                </p>

                <div className="grid grid-cols-2 gap-2 pt-1 font-mono text-[11px] text-slate-500 dark:text-slate-400">
                  <div>
                    <span className="text-slate-400">Batches:</span>{' '}
                    <span className="font-semibold text-slate-700 dark:text-slate-200">{sc.batches}</span>
                  </div>
                  <div>
                    <span className="text-slate-400">Defects:</span>{' '}
                    <span className="font-semibold text-slate-700 dark:text-slate-200">{sc.invalidRatio}%</span>
                  </div>
                  <div>
                    <span className="text-slate-400">Volume:</span>{' '}
                    <span className="font-semibold text-slate-700 dark:text-slate-200">~{sc.volume}</span>
                  </div>
                  <div>
                    <span className="text-slate-400">Quality:</span>{' '}
                    <span className="font-semibold text-slate-700 dark:text-slate-200 truncate">{sc.quality}</span>
                  </div>
                </div>
              </div>

              <div className="pt-3.5 border-t border-slate-100 dark:border-slate-800 mt-3 flex items-center justify-between">
                <Button
                  size="xs"
                  variant="ghost"
                  onClick={(e) => {
                    e.stopPropagation();
                    onOpenScenarioModal(sc.key);
                  }}
                  className="text-[11px] text-slate-500 hover:text-slate-900 dark:hover:text-slate-100 p-0 h-auto"
                >
                  <SlidersHorizontal className="h-3 w-3 mr-1" />
                  Configure
                </Button>

                <Button
                  size="xs"
                  variant={isStaged ? 'default' : 'outline'}
                  onClick={(e) => {
                    e.stopPropagation();
                    onStartImmediately(sc);
                  }}
                  className={isStaged ? 'bg-indigo-600 hover:bg-indigo-700 text-white font-semibold' : 'font-semibold'}
                >
                  <Play className="h-3 w-3 mr-1 fill-current" />
                  Run Now
                </Button>
              </div>
            </Card>
          );
        })}
      </div>
    </div>
  );
};
