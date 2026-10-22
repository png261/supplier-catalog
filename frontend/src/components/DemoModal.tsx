import React, { useState } from 'react';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from './ui/dialog';
import { Button } from './ui/button';
import { Badge } from './ui/badge';
import { Input } from './ui/input';
import { Label } from './ui/label';
import { Slider } from './ui/slider';
import { TestScenario } from '../types';
import {
  RotateCw,
  Play,
  CheckCircle2,
  AlertTriangle,
  SlidersHorizontal,
  Flame,
  Zap,
  FileCode,
  Dices,
} from 'lucide-react';

interface DemoModalProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  stagedScenarioKey: string;
  interruptedJob: { job_id: string; committed_batch_index: number; total_batches: number } | null;
  onSelectScenario: (scenario: TestScenario, customRecords?: any[]) => void;
  onStartImmediately: (scenario: TestScenario, customRecords?: any[]) => void;
  onResumeInterrupted: (jobId: string) => void;
}

export const SCENARIOS: TestScenario[] = [
  {
    key: 'default',
    name: 'Standard Ingestion Profile',
    desc: '10 batches (1,000 records). Balanced operational workload with updates, duplicates, and defects.',
    batches: 10,
    size: 100,
    volume: 1000,
    quality: '5% anomalies (DLQ test)',
    seed: 42,
    updateRatio: 20,
    dupRatio: 5,
    oooRatio: 10,
    withdrawnRatio: 5,
    invalidRatio: 5,
  },
  {
    key: 'clean',
    name: 'Clean Ingestion (Zero Defects)',
    desc: '10 batches (1,000 records). 100% compliant supplier feed with zero synthetic anomalies.',
    batches: 10,
    size: 100,
    volume: 1000,
    quality: '0% anomalies (100% compliant)',
    seed: 101,
    updateRatio: 20,
    dupRatio: 0,
    oooRatio: 0,
    withdrawnRatio: 0,
    invalidRatio: 0,
  },
  {
    key: 'stress',
    name: 'Fault Tolerance & DLQ Stress',
    desc: '15 batches (2,250 records). Injects price defects, empty SKUs, and schema errors to verify quarantine.',
    batches: 15,
    size: 150,
    volume: 2250,
    quality: '15% anomalies (Heavy DLQ test)',
    seed: 999,
    updateRatio: 15,
    dupRatio: 10,
    oooRatio: 15,
    withdrawnRatio: 10,
    invalidRatio: 15,
  },
  {
    key: 'large',
    name: 'High-Volume Stress Load',
    desc: '50 batches (10,000 records). High-throughput streaming verification for pipeline stability.',
    batches: 50,
    size: 200,
    volume: 10000,
    quality: '5% anomalies (High throughput)',
    seed: 777,
    updateRatio: 20,
    dupRatio: 5,
    oooRatio: 10,
    withdrawnRatio: 5,
    invalidRatio: 5,
  },
  {
    key: 'interrupted',
    name: 'Mid-Feed Crash & Checkpoint Recovery',
    desc: '10 batches (1,000 records). Automatically halts at batch 5/10 to test atomic checkpoints & 1-click recovery.',
    batches: 10,
    size: 100,
    volume: 1000,
    quality: 'Crash simulation at batch 5/10',
    seed: 555,
    updateRatio: 20,
    dupRatio: 5,
    oooRatio: 10,
    withdrawnRatio: 5,
    invalidRatio: 5,
  },
  {
    key: 'custom',
    name: 'Custom Supplier Batch (Direct JSON)',
    desc: 'Ad-hoc supplier verification. Paste or edit custom JSON records to ingest directly into the pipeline.',
    batches: 1,
    size: 3,
    volume: 3,
    quality: 'Direct JSON validation',
    seed: 1,
    updateRatio: 0,
    dupRatio: 0,
    oooRatio: 0,
    withdrawnRatio: 0,
    invalidRatio: 0,
  },
];

const SAMPLE_JSON = [
  {
    sku: 'DESK-OAK-101',
    name: 'Minimalist Solid Oak Desk',
    price: 249.99,
    status: 'ACTIVE',
    timestamp: new Date().toISOString(),
  },
  {
    sku: 'CHAIR-ERGONOMIC-202',
    name: 'Ergonomic Precision Mesh Chair',
    price: 189.50,
    status: 'ACTIVE',
    timestamp: new Date().toISOString(),
  },
  {
    sku: 'LAMP-TASK-303',
    name: 'Architectural LED Task Lamp',
    price: 59.0,
    status: 'ACTIVE',
    timestamp: new Date().toISOString(),
  },
];

export const DemoModal: React.FC<DemoModalProps> = ({
  open,
  onOpenChange,
  stagedScenarioKey,
  interruptedJob,
  onSelectScenario,
  onStartImmediately,
  onResumeInterrupted,
}) => {
  const [selectedKey, setSelectedKey] = useState<string>(stagedScenarioKey || 'default');
  const [showAdvanced, setShowAdvanced] = useState<boolean>(false);
  const [jsonText, setJsonText] = useState<string>(JSON.stringify(SAMPLE_JSON, null, 2));
  const [jsonError, setJsonError] = useState<string | null>(null);

  // Advanced form states
  const selectedScenario = SCENARIOS.find((s) => s.key === selectedKey) || SCENARIOS[0];
  const [seed, setSeed] = useState<number>(selectedScenario.seed);
  const [batches, setBatches] = useState<number>(selectedScenario.batches);
  const [batchSize, setBatchSize] = useState<number>(selectedScenario.size);
  const [updateRatio, setUpdateRatio] = useState<number>(selectedScenario.updateRatio);
  const [dupRatio, setDupRatio] = useState<number>(selectedScenario.dupRatio);
  const [oooRatio, setOooRatio] = useState<number>(selectedScenario.oooRatio);
  const [withdrawnRatio, setWithdrawnRatio] = useState<number>(selectedScenario.withdrawnRatio);
  const [invalidRatio, setInvalidRatio] = useState<number>(selectedScenario.invalidRatio);

  React.useEffect(() => {
    if (open) {
      const active = SCENARIOS.find((s) => s.key === stagedScenarioKey) || SCENARIOS[0];
      setSelectedKey(active.key);
      setSeed(active.seed);
      setBatches(active.batches);
      setBatchSize(active.size);
      setUpdateRatio(active.updateRatio);
      setDupRatio(active.dupRatio);
      setOooRatio(active.oooRatio);
      setWithdrawnRatio(active.withdrawnRatio);
      setInvalidRatio(active.invalidRatio);
    }
  }, [open, stagedScenarioKey]);

  const handleSelectCard = (s: TestScenario) => {
    setSelectedKey(s.key);
    setSeed(s.seed);
    setBatches(s.batches);
    setBatchSize(s.size);
    setUpdateRatio(s.updateRatio);
    setDupRatio(s.dupRatio);
    setOooRatio(s.oooRatio);
    setWithdrawnRatio(s.withdrawnRatio);
    setInvalidRatio(s.invalidRatio);
  };

  const getCustomRecords = () => {
    if (selectedKey !== 'custom') return undefined;
    try {
      const parsed = JSON.parse(jsonText.trim());
      if (!Array.isArray(parsed)) throw new Error('Must be an array of records');
      setJsonError(null);
      return parsed;
    } catch (err: any) {
      setJsonError(err.message);
      return null;
    }
  };

  const handleApply = () => {
    let customRecords: any[] | undefined = undefined;
    if (selectedKey === 'custom') {
      const records = getCustomRecords();
      if (records === null) return;
      customRecords = records;
    }
    const target = SCENARIOS.find((s) => s.key === selectedKey) || SCENARIOS[0];
    const finalScenario: TestScenario = {
      ...target,
      seed,
      batches: selectedKey === 'custom' ? 1 : batches,
      size: selectedKey === 'custom' ? (customRecords?.length || 1) : batchSize,
      volume: selectedKey === 'custom' ? (customRecords?.length || 1) : batches * batchSize,
      updateRatio,
      dupRatio,
      oooRatio,
      withdrawnRatio,
      invalidRatio,
    };
    onSelectScenario(finalScenario, customRecords);
    onOpenChange(false);
  };

  const handleStartNow = () => {
    let customRecords: any[] | undefined = undefined;
    if (selectedKey === 'custom') {
      const records = getCustomRecords();
      if (records === null) return;
      customRecords = records;
    }
    const target = SCENARIOS.find((s) => s.key === selectedKey) || SCENARIOS[0];
    const finalScenario: TestScenario = {
      ...target,
      seed,
      batches: selectedKey === 'custom' ? 1 : batches,
      size: selectedKey === 'custom' ? (customRecords?.length || 1) : batchSize,
      volume: selectedKey === 'custom' ? (customRecords?.length || 1) : batches * batchSize,
      updateRatio,
      dupRatio,
      oooRatio,
      withdrawnRatio,
      invalidRatio,
    };
    onStartImmediately(finalScenario, customRecords);
    onOpenChange(false);
  };

  const getScenarioIcon = (key: string) => {
    switch (key) {
      case 'clean':
        return <CheckCircle2 className="h-4 w-4 text-emerald-600" />;
      case 'stress':
        return <Flame className="h-4 w-4 text-red-500" />;
      case 'large':
        return <Zap className="h-4 w-4 text-amber-500" />;
      case 'interrupted':
        return <AlertTriangle className="h-4 w-4 text-amber-600" />;
      case 'custom':
        return <FileCode className="h-4 w-4 text-blue-600" />;
      default:
        return <SlidersHorizontal className="h-4 w-4 text-slate-600" />;
    }
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-3xl sm:max-w-4xl p-6">
        <DialogHeader>
          <DialogTitle className="text-xl font-bold text-slate-900">
            Supplier Ingestion Test Scenarios
          </DialogTitle>
          <DialogDescription className="text-xs text-slate-500">
            Select a test scenario to stage synthetic supplier feed data in the workspace, then click Start Sync on the main page.
          </DialogDescription>
        </DialogHeader>

        {/* Active Interrupted Job Banner */}
        {interruptedJob && (
          <div className="flex items-center justify-between p-3 rounded-lg border border-amber-300 bg-amber-50 text-amber-900 text-xs">
            <div className="flex items-center gap-2">
              <AlertTriangle className="h-4 w-4 text-amber-600 shrink-0" />
              <span>
                <strong>Interrupted Job Detected:</strong> Job #{(interruptedJob.job_id || '').substring(0, 14)}... halted at batch{' '}
                {interruptedJob.committed_batch_index + 1}/{interruptedJob.total_batches}. Checkpoint preserved.
              </span>
            </div>
            <Button
              variant="default"
              size="xs"
              onClick={() => {
                onResumeInterrupted(interruptedJob.job_id);
                onOpenChange(false);
              }}
              className="bg-amber-600 hover:bg-amber-700 text-white shrink-0 ml-3"
            >
              <RotateCw className="h-3 w-3 mr-1" />
              <span>Resume Now</span>
            </Button>
          </div>
        )}

        {/* 6 Scenarios Grid */}
        <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3 my-2">
          {SCENARIOS.map((s) => {
            const isSelected = selectedKey === s.key;
            return (
              <div
                key={s.key}
                onClick={() => handleSelectCard(s)}
                className={`p-3.5 rounded-lg border transition-all duration-150 cursor-pointer flex flex-col justify-between gap-1.5 ${
                  isSelected
                    ? 'border-blue-600 bg-blue-50/50 shadow-sm ring-1 ring-blue-500'
                    : 'border-slate-200 bg-white hover:border-slate-300 hover:bg-slate-50'
                }`}
              >
                <div>
                  <div className="flex items-center justify-between mb-1">
                    <span className="text-xs font-bold text-slate-900 flex items-center gap-1.5">
                      {getScenarioIcon(s.key)}
                      <span>{s.name}</span>
                    </span>
                  </div>
                  <p className="text-[11px] text-slate-600 leading-relaxed">
                    {s.desc}
                  </p>
                </div>
                <div className="pt-2 border-t border-slate-100 flex items-center justify-between text-[10px] text-slate-400 font-medium">
                  <span>{s.batches} batches</span>
                  <Badge variant={isSelected ? 'info' : 'outline'} className="text-[9px] px-1.5 py-0">
                    {s.quality}
                  </Badge>
                </div>
              </div>
            );
          })}
        </div>

        {/* Custom JSON Batch Panel (shown when 'custom' scenario is selected) */}
        {selectedKey === 'custom' && (
          <div className="p-3.5 rounded-lg border border-slate-200 bg-slate-50 space-y-2 animate-in fade-in-50">
            <div className="flex items-center justify-between text-xs">
              <span className="font-bold text-slate-800 flex items-center gap-1.5">
                <FileCode className="h-3.5 w-3.5 text-blue-600" />
                <span>Custom Supplier Records (JSON Array)</span>
              </span>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="xs"
                  onClick={() => {
                    setJsonText(JSON.stringify(SAMPLE_JSON, null, 2));
                    setJsonError(null);
                  }}
                  className="h-6 text-[11px]"
                >
                  Load Sample
                </Button>
                <Button
                  variant="outline"
                  size="xs"
                  onClick={() => {
                    setJsonText('');
                    setJsonError(null);
                  }}
                  className="h-6 text-[11px]"
                >
                  Clear
                </Button>
              </div>
            </div>

            <textarea
              value={jsonText}
              onChange={(e) => {
                setJsonText(e.target.value);
                setJsonError(null);
              }}
              rows={5}
              placeholder="[ { sku: 'PROD-101', name: 'Oak Desk', price: 149.99, status: 'ACTIVE', timestamp: '...' } ]"
              className="w-full rounded-md border border-slate-200 bg-white p-2.5 font-mono text-xs text-slate-900 shadow-sm focus:outline-none focus:ring-1 focus:ring-blue-500"
            />
            {jsonError && (
              <span className="text-[11px] font-semibold text-red-600">
                Invalid JSON: {jsonError}
              </span>
            )}
          </div>
        )}

        {/* Advanced Settings Accordion */}
        {selectedKey !== 'custom' && (
          <div className="border-t border-slate-100 pt-2">
            <button
              type="button"
              onClick={() => setShowAdvanced(!showAdvanced)}
              className="text-xs font-semibold text-slate-600 hover:text-slate-900 flex items-center gap-1.5 mb-2"
            >
              <SlidersHorizontal className="h-3 w-3" />
              <span>{showAdvanced ? 'Hide Advanced Generator Parameters' : 'Show Advanced Generator Parameters'}</span>
            </button>

            {showAdvanced && (
              <div className="grid grid-cols-1 sm:grid-cols-2 md:grid-cols-3 gap-3 p-3.5 rounded-lg border border-slate-200 bg-slate-50 text-xs animate-in fade-in-50">
                <div>
                  <Label className="mb-1 block">PRNG Seed:</Label>
                  <div className="flex items-center gap-1.5">
                    <Input
                      type="number"
                      value={seed}
                      onChange={(e) => setSeed(parseInt(e.target.value) || 0)}
                      className="h-8 text-xs font-mono"
                    />
                    <Button
                      variant="outline"
                      size="xs"
                      onClick={() => setSeed(Math.floor(Math.random() * 1000000))}
                      title="Randomize PRNG seed"
                    >
                      <Dices className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                </div>

                <div>
                  <Label className="mb-1 block">Batches count:</Label>
                  <Input
                    type="number"
                    value={batches}
                    min={1}
                    max={100}
                    onChange={(e) => setBatches(parseInt(e.target.value) || 1)}
                    className="h-8 text-xs font-mono"
                  />
                </div>

                <div>
                  <Label className="mb-1 block">Records per Batch:</Label>
                  <Input
                    type="number"
                    value={batchSize}
                    min={10}
                    max={1000}
                    onChange={(e) => setBatchSize(parseInt(e.target.value) || 10)}
                    className="h-8 text-xs font-mono"
                  />
                </div>

                <div className="space-y-1">
                  <div className="flex justify-between text-[11px]">
                    <span className="text-slate-500">Update Ratio:</span>
                    <strong className="font-mono">{updateRatio}%</strong>
                  </div>
                  <Slider
                    value={[updateRatio]}
                    max={100}
                    step={1}
                    onValueChange={(val) => setUpdateRatio(val[0])}
                  />
                </div>

                <div className="space-y-1">
                  <div className="flex justify-between text-[11px]">
                    <span className="text-slate-500">Duplicate Ratio:</span>
                    <strong className="font-mono">{dupRatio}%</strong>
                  </div>
                  <Slider
                    value={[dupRatio]}
                    max={100}
                    step={1}
                    onValueChange={(val) => setDupRatio(val[0])}
                  />
                </div>

                <div className="space-y-1">
                  <div className="flex justify-between text-[11px]">
                    <span className="text-slate-500">Out-of-Order Ratio:</span>
                    <strong className="font-mono">{oooRatio}%</strong>
                  </div>
                  <Slider
                    value={[oooRatio]}
                    max={100}
                    step={1}
                    onValueChange={(val) => setOooRatio(val[0])}
                  />
                </div>

                <div className="space-y-1">
                  <div className="flex justify-between text-[11px]">
                    <span className="text-slate-500">Withdrawn Ratio:</span>
                    <strong className="font-mono">{withdrawnRatio}%</strong>
                  </div>
                  <Slider
                    value={[withdrawnRatio]}
                    max={100}
                    step={1}
                    onValueChange={(val) => setWithdrawnRatio(val[0])}
                  />
                </div>

                <div className="space-y-1">
                  <div className="flex justify-between text-[11px]">
                    <span className="text-slate-500">Schema Error Ratio:</span>
                    <strong className="font-mono text-red-600">{invalidRatio}%</strong>
                  </div>
                  <Slider
                    value={[invalidRatio]}
                    max={100}
                    step={1}
                    onValueChange={(val) => setInvalidRatio(val[0])}
                  />
                </div>
              </div>
            )}
          </div>
        )}

        <DialogFooter className="gap-2 sm:gap-0 pt-2 border-t border-slate-100">
          <Button
            variant="primary"
            size="sm"
            onClick={handleApply}
            className="bg-blue-600 hover:bg-blue-700 text-white font-semibold"
          >
            Apply Scenario & Stage Data
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={handleStartNow}
            className="text-slate-700"
          >
            <Play className="h-3.5 w-3.5 fill-current mr-1" />
            <span>Start Immediately</span>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};
