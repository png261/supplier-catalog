import React, { useState, useEffect, useRef } from 'react';
import { Header } from './components/Header';
import { StagedScenarioBar } from './components/StagedScenarioBar';
import { ProgressBanner } from './components/ProgressBanner';
import { AnalyticsFilterBar } from './components/AnalyticsFilterBar';
import { MetricsGrid } from './components/MetricsGrid';
import { ChartsSection } from './components/ChartsSection';
import { ExplorerSection } from './components/ExplorerSection';
import { DemoModal, SCENARIOS } from './components/DemoModal';
import { RemediationModal } from './components/RemediationModal';
import { MetricDrilldownModal } from './components/MetricDrilldownModal';
import { ShopModal } from './components/ShopModal';
import { Toaster, toast } from 'sonner';

import {
  fetchCatalog,
  fetchDLQ,
  fetchHistory,
  fetchInterruptedJobs,
  fetchAnalytics,
  startJob,
  resumeJob,
  cancelJob,
  resetDatabase,
  remediateProduct,
  getActiveShopId,
  setActiveShopId,
  getSSEUrl,
} from './services/api';

import {
  SyncJob,
  CatalogProduct,
  DeadLetterIssue,
  AnalyticsResponse,
  TestScenario,
  JobStatus,
} from './types';

export const App: React.FC = () => {
  // --- Shop Tenant Isolation ---
  const [shopId, setShopId] = useState<string>(() => getActiveShopId());
  const [isShopModalOpen, setIsShopModalOpen] = useState<boolean>(false);

  // --- Server Sent Events & Connection State ---
  const [sseConnected, setSseConnected] = useState<boolean>(false);
  const eventSourceRef = useRef<EventSource | null>(null);

  // --- Active Job & Execution State ---
  const [activeJob, setActiveJob] = useState<{ job_id: string; status: JobStatus } | null>(null);
  const [interruptedJob, setInterruptedJob] = useState<SyncJob | null>(null);
  const [currentBatch, setCurrentBatch] = useState<number>(0);
  const [totalBatches, setTotalBatches] = useState<number>(10);
  const [throughput, setThroughput] = useState<number>(0);
  const [eta, setEta] = useState<string>('—');
  const [elapsedTime, setElapsedTime] = useState<string>('00:00');
  const [smartStatusText, setSmartStatusText] = useState<string>(
    'Catalog is up to date. Use "Simulate Supplier Feed" to ingest new batches.'
  );
  const [smartStatusType, setSmartStatusType] = useState<'normal' | 'running' | 'warning' | 'success'>('normal');

  // Timers and references
  const jobStartTimeRef = useRef<number | null>(null);
  const runtimeTimerRef = useRef<any>(null);
  const autoHaltBatchRef = useRef<number | null>(null);

  // --- Staged Scenario ---
  const [stagedScenario, setStagedScenario] = useState<TestScenario>(SCENARIOS[0]);
  const [customRecordsStaged, setCustomRecordsStaged] = useState<any[] | undefined>(undefined);

  // --- Analytics Filters & Metrics ---
  const [analyticsRange, setAnalyticsRange] = useState<string>('today');
  const [analyticsFrom, setAnalyticsFrom] = useState<string>(new Date().toISOString().slice(0, 10));
  const [analyticsTo, setAnalyticsTo] = useState<string>(new Date().toISOString().slice(0, 10));
  const [selectedJobId, setSelectedJobId] = useState<string>('all');
  const [isRefreshingAnalytics, setIsRefreshingAnalytics] = useState<boolean>(false);

  const [metrics, setMetrics] = useState({
    added: 0,
    updated: 0,
    withdrawn: 0,
    skipped: 0,
    dead_letter: 0,
  });

  const [deltas, setDeltas] = useState({
    added: 0,
    updated: 0,
    withdrawn: 0,
    skipped: 0,
    dead_letter: 0,
  });

  const [analyticsData, setAnalyticsData] = useState<AnalyticsResponse>({
    period: 'today',
    job_id: 'all',
    time_range: { start: '', end: '' },
    totals: {
      total_jobs: 0,
      active_products: 0,
      withdrawn_products: 0,
      quarantined_issues: 0,
      total_events_processed: 0,
      total_events_succeeded: 0,
      defect_rate: 0,
    },
    summary: { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 },
    time_series: [],
    sync_jobs: [],
  });

  // --- Explorer Tabs State ---
  const [catalog, setCatalog] = useState<{ products: CatalogProduct[]; total: number }>({ products: [], total: 0 });
  const [catalogPage, setCatalogPage] = useState<number>(1);
  const [catalogLimit] = useState<number>(20);
  const [catalogSearch, setCatalogSearch] = useState<string>('');
  const [catalogStatus, setCatalogStatus] = useState<string>('');

  const [dlq, setDlq] = useState<{ issues: DeadLetterIssue[]; total: number }>({ issues: [], total: 0 });
  const [dlqPage, setDlqPage] = useState<number>(1);
  const [dlqLimit] = useState<number>(20);
  const [dlqReason, setDlqReason] = useState<string>('');

  const [history, setHistory] = useState<{ jobs: SyncJob[]; total: number }>({ jobs: [], total: 0 });
  const [historyPage, setHistoryPage] = useState<number>(1);
  const [historyLimit] = useState<number>(20);

  // --- Modals State ---
  const [isScenarioModalOpen, setIsScenarioModalOpen] = useState<boolean>(false);
  const [activeRemediationIssue, setActiveRemediationIssue] = useState<DeadLetterIssue | null>(null);
  const [drilldownCategory, setDrilldownCategory] = useState<'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter' | null>(null);
  const [isResettingDb, setIsResettingDb] = useState<boolean>(false);

  // --- Initial Data Load ---
  const loadAllData = async () => {
    try {
      const [catData, dlqData, histData, intData] = await Promise.all([
        fetchCatalog(catalogLimit, (catalogPage - 1) * catalogLimit, catalogSearch, catalogStatus),
        fetchDLQ(dlqLimit, (dlqPage - 1) * dlqLimit, dlqReason),
        fetchHistory(historyLimit, (historyPage - 1) * historyLimit),
        fetchInterruptedJobs(1),
      ]);
      setCatalog(catData);
      setDlq(dlqData);
      setHistory(histData);
      if (intData.jobs.length > 0) {
        setInterruptedJob(intData.jobs[0]);
      } else {
        setInterruptedJob(null);
      }
    } catch (err: any) {
      console.error('Failed to load initial data:', err);
    }
  };

  const loadAnalytics = async () => {
    setIsRefreshingAnalytics(true);
    try {
      const data = await fetchAnalytics({
        range: analyticsRange,
        from: analyticsFrom,
        to: analyticsTo,
        jobId: selectedJobId,
      });

      const summary = data?.metrics?.summary || {
        added: 0,
        updated: 0,
        withdrawn: 0,
        skipped: 0,
        dead_letter: 0,
      };
      setMetrics(summary);

      const totalProc = data?.metrics?.total_processed || 0;
      const defectRate = totalProc > 0 ? ((summary.dead_letter || 0) / totalProc) * 100 : 0;

      setAnalyticsData({
        period: data.filter?.time_range || analyticsRange,
        job_id: data.filter?.job_id || selectedJobId,
        time_range: { start: data.filter?.from || '', end: data.filter?.to || '' },
        totals: {
          total_jobs: data.metrics?.total_syncs || 0,
          active_products: catalog.total,
          withdrawn_products: summary.withdrawn,
          quarantined_issues: summary.dead_letter,
          total_events_processed: totalProc,
          total_events_succeeded: Math.max(0, totalProc - summary.dead_letter),
          defect_rate: defectRate,
        },
        summary: summary,
        time_series: [],
        sync_jobs: (data.sync_jobs || []).map((j: any) => ({
          job_id: j.job_id,
          status: j.status,
          total_batches: j.total_batches,
          created_at: j.created_at,
        })),
      });
    } catch (err: any) {
      console.error('Failed to load analytics:', err);
    } finally {
      setIsRefreshingAnalytics(false);
    }
  };

  const handleSelectShop = (newShopId: string) => {
    if (newShopId === shopId) return;
    setActiveShopId(newShopId);
    setShopId(newShopId);
    toast.success(`Đã vào shop: ${newShopId}`);
  };

  const handleShareShop = () => {
    const url = window.location.origin + window.location.pathname + '?shop=' + encodeURIComponent(shopId);
    if (navigator?.clipboard?.writeText) {
      navigator.clipboard.writeText(url)
        .then(() => toast.success('Đã sao chép link shop vào bộ nhớ tạm!'))
        .catch(() => toast.info(`Link shop: ${url}`));
    } else {
      toast.info(`Link shop: ${url}`);
    }
  };

  useEffect(() => {
    loadAllData();
    loadAnalytics();
  }, [shopId]);

  useEffect(() => {
    fetchCatalog(catalogLimit, (catalogPage - 1) * catalogLimit, catalogSearch, catalogStatus)
      .then(setCatalog)
      .catch(console.error);
  }, [catalogPage, catalogSearch, catalogStatus, shopId]);

  useEffect(() => {
    fetchDLQ(dlqLimit, (dlqPage - 1) * dlqLimit, dlqReason)
      .then(setDlq)
      .catch(console.error);
  }, [dlqPage, dlqReason, shopId]);

  useEffect(() => {
    fetchHistory(historyLimit, (historyPage - 1) * historyLimit)
      .then(setHistory)
      .catch(console.error);
  }, [historyPage, shopId]);

  useEffect(() => {
    loadAnalytics();
  }, [analyticsRange, selectedJobId, shopId]);

  // --- SSE Connection Setup ---
  useEffect(() => {
    let reconnectTimer: any;

    const connect = () => {
      if (eventSourceRef.current) {
        eventSourceRef.current.close();
      }

      const es = new EventSource(getSSEUrl());
      eventSourceRef.current = es;

      es.onopen = () => {
        setSseConnected(true);
      };

      es.onerror = () => {
        setSseConnected(false);
        es.close();
        reconnectTimer = setTimeout(connect, 3000);
      };

      es.addEventListener('job_started', (e: any) => {
        try {
          const data = JSON.parse(e.data);
          setActiveJob({ job_id: data.job_id, status: 'RUNNING' });
          setCurrentBatch(0);
          setTotalBatches(data.total_batches || 10);
          setSmartStatusText(`Ingesting supplier feed #${(data.job_id || '').substring(0, 8)}... Processing atomic batches.`);
          setSmartStatusType('running');
          jobStartTimeRef.current = Date.now();
          setDeltas({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });

          // Start runtime ticker
          clearInterval(runtimeTimerRef.current);
          runtimeTimerRef.current = setInterval(() => {
            if (jobStartTimeRef.current) {
              const elapsedSec = Math.floor((Date.now() - jobStartTimeRef.current) / 1000);
              const mm = String(Math.floor(elapsedSec / 60)).padStart(2, '0');
              const ss = String(elapsedSec % 60).padStart(2, '0');
              setElapsedTime(`${mm}:${ss}`);
            }
          }, 1000);
        } catch (err) {
          console.error('Invalid job_started event', err);
        }
      });

      es.addEventListener('progress', (e: any) => {
        try {
          const data = JSON.parse(e.data);
          const bIdx = data.batch_index;
          const tBatches = data.total_batches || 10;
          setCurrentBatch(bIdx + 1);
          setTotalBatches(tBatches);

          if (data.summary) {
            setMetrics(data.summary);
          }
          if (data.batch_delta) {
            setDeltas(data.batch_delta);
          }

          // Throughput & ETA
          if (jobStartTimeRef.current) {
            const elapsed = (Date.now() - jobStartTimeRef.current) / 1000;
            if (elapsed > 0.5) {
              const totalRecs = (data.summary?.added || 0) + (data.summary?.updated || 0) + (data.summary?.withdrawn || 0) + (data.summary?.skipped || 0) + (data.summary?.dead_letter || 0);
              const spd = Math.round(totalRecs / elapsed);
              setThroughput(spd);
              const remainingBatches = tBatches - (bIdx + 1);
              if (remainingBatches > 0 && bIdx > 0) {
                const avgBatch = elapsed / (bIdx + 1);
                setEta(`${Math.round(remainingBatches * avgBatch)}s`);
              } else {
                setEta('Finishing');
              }
            }
          }

          setSmartStatusText(
            `Batch ${bIdx + 1}/${tBatches} committed: Added +${data.batch_delta?.added || 0}, Updated ~${data.batch_delta?.updated || 0}, Withdrawn -${data.batch_delta?.withdrawn || 0}, Duplicates skipped ${data.batch_delta?.skipped || 0}.`
          );
          setSmartStatusType('running');

          // Auto-halt simulation check for 'interrupted' scenario
          if (autoHaltBatchRef.current && (bIdx + 1) >= autoHaltBatchRef.current) {
            autoHaltBatchRef.current = null;
            toast.warning('Simulation: Mid-feed interruption triggered! Checkpoint saved.');
            cancelJob(data.job_id).catch(() => {});
          }
        } catch (err) {
          console.error('Invalid progress event', err);
        }
      });

      es.addEventListener('dead_letter', () => {
        setSmartStatusType('warning');
        fetchDLQ(dlqLimit, (dlqPage - 1) * dlqLimit, dlqReason).then(setDlq).catch(console.error);
      });

      es.addEventListener('job_completed', (e: any) => {
        try {
          const data = JSON.parse(e.data);
          setActiveJob(null);
          setInterruptedJob(null);
          autoHaltBatchRef.current = null;
          setCurrentBatch(data.total_batches || totalBatches);
          setEta('Done');
          clearInterval(runtimeTimerRef.current);
          setSmartStatusText('All supplier batches synchronized and committed successfully to storage.');
          setSmartStatusType('success');
          toast.success('Synchronization completed successfully!');
          loadAllData();
          loadAnalytics();
        } catch (err) {
          console.error('Invalid job_completed event', err);
        }
      });

      es.addEventListener('job_interrupted', (e: any) => {
        try {
          const data = JSON.parse(e.data);
          setActiveJob({ job_id: data.job_id, status: 'INTERRUPTED' });
          setInterruptedJob({
            job_id: data.job_id,
            status: 'INTERRUPTED',
            total_batches: data.total_batches || 10,
            committed_batch_index: data.committed_batch_index || 4,
            summary: metrics,
            created_at: new Date().toISOString(),
            updated_at: new Date().toISOString(),
          });
          clearInterval(runtimeTimerRef.current);
          setEta('Paused');
          setSmartStatusText(
            `Synchronization paused safely at batch ${data.committed_batch_index + 1}. Checkpoint saved. Click "Resume Sync" to continue.`
          );
          setSmartStatusType('warning');
          toast.info('Job paused safely at last committed checkpoint.');
          loadAllData();
        } catch (err) {
          console.error('Invalid job_interrupted event', err);
        }
      });
    };

    connect();

    return () => {
      clearTimeout(reconnectTimer);
      clearInterval(runtimeTimerRef.current);
      if (eventSourceRef.current) eventSourceRef.current.close();
    };
  }, [shopId]);

  // --- Handlers ---
  const handleStartSync = async () => {
    // If interrupted, resume it
    if (interruptedJob || (activeJob && activeJob.status === 'INTERRUPTED')) {
      const jobId = interruptedJob?.job_id || activeJob?.job_id;
      if (jobId) {
        try {
          await resumeJob(jobId);
          toast.success(`Resumed synchronization job ${jobId}`);
          setInterruptedJob(null);
        } catch (err: any) {
          toast.error(err.message || 'Failed to resume job');
        }
        return;
      }
    }

    // Configure payload based on scenario
    let payload: any;
    if (stagedScenario.key === 'custom') {
      if (!customRecordsStaged || customRecordsStaged.length === 0) {
        setIsScenarioModalOpen(true);
        toast.error('Please load or enter custom JSON records first');
        return;
      }
      payload = {
        mode: 'batches',
        batches: [{ batch_index: 0, records: customRecordsStaged }],
      };
      autoHaltBatchRef.current = null;
    } else {
      if (stagedScenario.key === 'interrupted') {
        autoHaltBatchRef.current = 5; // Halt automatically after batch 5 (50% progress)
      } else {
        autoHaltBatchRef.current = null;
      }

      payload = {
        mode: 'generator',
        seed: stagedScenario.seed,
        total_batches: stagedScenario.batches,
        batch_size: stagedScenario.size,
        update_ratio: stagedScenario.updateRatio / 100,
        duplicate_ratio: stagedScenario.dupRatio / 100,
        out_of_order_ratio: stagedScenario.oooRatio / 100,
        withdrawn_ratio: stagedScenario.withdrawnRatio / 100,
        invalid_ratio: stagedScenario.invalidRatio / 100,
      };
    }

    try {
      const res = await startJob(payload);
      toast.success(`Synchronization started: ${res.job_id}`);
    } catch (err: any) {
      toast.error(err.message || 'Failed to start job');
    }
  };

  const handleCancelSync = async () => {
    if (!activeJob?.job_id) return;
    try {
      await cancelJob(activeJob.job_id);
      toast.info('Stopping job safely at current batch checkpoint...');
    } catch (err: any) {
      toast.error(err.message || 'Failed to stop job');
    }
  };

  const handleSelectScenario = (scenario: TestScenario, customRecords?: any[]) => {
    setStagedScenario(scenario);
    setCustomRecordsStaged(customRecords);
    toast.info(`Staged "${scenario.name}". Click "Start Sync" to begin.`);
  };

  const handleStartImmediately = async (scenario: TestScenario, customRecords?: any[]) => {
    setStagedScenario(scenario);
    setCustomRecordsStaged(customRecords);

    let payload: any;
    if (scenario.key === 'custom') {
      if (!customRecords || customRecords.length === 0) {
        toast.error('No custom records provided');
        return;
      }
      payload = {
        mode: 'batches',
        batches: [{ batch_index: 0, records: customRecords }],
      };
      autoHaltBatchRef.current = null;
    } else {
      if (scenario.key === 'interrupted') {
        autoHaltBatchRef.current = 5;
      } else {
        autoHaltBatchRef.current = null;
      }

      payload = {
        mode: 'generator',
        seed: scenario.seed,
        total_batches: scenario.batches,
        batch_size: scenario.size,
        update_ratio: scenario.updateRatio / 100,
        duplicate_ratio: scenario.dupRatio / 100,
        out_of_order_ratio: scenario.oooRatio / 100,
        withdrawn_ratio: scenario.withdrawnRatio / 100,
        invalid_ratio: scenario.invalidRatio / 100,
      };
    }

    try {
      const res = await startJob(payload);
      toast.success(`Synchronization started: ${res.job_id}`);
    } catch (err: any) {
      toast.error(err.message || 'Failed to start job');
    }
  };

  const handleResumeInterrupted = async (jobId: string) => {
    try {
      await resumeJob(jobId);
      toast.success(`Resumed job ${jobId}`);
      setInterruptedJob(null);
    } catch (err: any) {
      toast.error(err.message || 'Failed to resume job');
    }
  };

  const handleResetDb = async () => {
    if (!confirm('Are you sure you want to completely reset the catalog database?')) return;
    setIsResettingDb(true);
    try {
      await resetDatabase();
      toast.success('Database reset to clean initial state');
      setActiveJob(null);
      setInterruptedJob(null);
      setCurrentBatch(0);
      setMetrics({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
      setDeltas({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
      setSmartStatusText('Database has been completely reset to a clean state.');
      setSmartStatusType('normal');
      loadAllData();
      loadAnalytics();
    } catch (err: any) {
      toast.error(err.message || 'Failed to reset database');
    } finally {
      setIsResettingDb(false);
    }
  };

  const handleRemediateProduct = async (payload: {
    issue_id: string;
    sku: string;
    name: string;
    price: number;
    status: string;
  }) => {
    try {
      await remediateProduct(payload);
      toast.success(`Remediated product ${payload.sku} successfully imported into catalog!`);
      loadAllData();
      loadAnalytics();
    } catch (err: any) {
      toast.error(err.message || 'Failed to remediate product');
      throw err;
    }
  };

  const isSyncing = activeJob !== null && activeJob.status === 'RUNNING';
  const isInterrupted = interruptedJob !== null || (activeJob !== null && activeJob.status === 'INTERRUPTED');

  return (
    <div className="min-h-screen bg-slate-50 flex flex-col font-sans text-slate-900">
      {/* Toast provider */}
      <Toaster position="top-right" richColors />

      {/* Top Navigation */}
      <Header
        sseConnected={sseConnected}
        activeJob={activeJob}
        onOpenScenarioModal={() => setIsScenarioModalOpen(true)}
        onResetDatabase={handleResetDb}
        isResetting={isResettingDb}
      />

      {/* Main Container */}
      <main className="container max-w-7xl mx-auto flex-1 px-4 sm:px-6 py-6 space-y-5">
        {/* Active Staged Scenario Bar */}
        <StagedScenarioBar
          scenario={stagedScenario}
          isSyncing={isSyncing}
          isInterrupted={isInterrupted}
          onStartSync={handleStartSync}
          onCancelSync={handleCancelSync}
          onOpenScenarioModal={() => setIsScenarioModalOpen(true)}
        />

        {/* Live Feed Ingestion Progress */}
        <ProgressBanner
          currentBatch={currentBatch}
          totalBatches={totalBatches}
          throughput={throughput}
          eta={eta}
          elapsedTime={elapsedTime}
          smartStatusText={smartStatusText}
          smartStatusType={smartStatusType}
        />

        {/* Analytics Filter Bar */}
        <AnalyticsFilterBar
          range={analyticsRange}
          from={analyticsFrom}
          to={analyticsTo}
          selectedJobId={selectedJobId}
          syncJobs={analyticsData.sync_jobs || []}
          onRangeChange={setAnalyticsRange}
          onCustomDatesApply={(f, t) => {
            setAnalyticsFrom(f);
            setAnalyticsTo(t);
            loadAnalytics();
          }}
          onJobSelect={setSelectedJobId}
          onRefresh={loadAnalytics}
          isRefreshing={isRefreshingAnalytics}
        />

        {/* 5 Interactive Metric Summary Cards */}
        <MetricsGrid
          metrics={metrics}
          deltas={deltas}
          onCardClick={(cat) => setDrilldownCategory(cat)}
        />

        {/* Recharts Analytics Section */}
        <ChartsSection
          totals={analyticsData.totals}
          timeSeries={analyticsData.time_series}
        />

        {/* Explorer Tabs: Catalog, DLQ, History */}
        <ExplorerSection
          catalog={catalog}
          dlq={dlq}
          history={history}
          catalogPage={catalogPage}
          catalogLimit={catalogLimit}
          catalogSearch={catalogSearch}
          catalogStatus={catalogStatus}
          dlqPage={dlqPage}
          dlqLimit={dlqLimit}
          dlqReason={dlqReason}
          historyPage={historyPage}
          historyLimit={historyLimit}
          onCatalogSearchChange={(s) => {
            setCatalogSearch(s);
            setCatalogPage(1);
          }}
          onCatalogStatusChange={(st) => {
            setCatalogStatus(st);
            setCatalogPage(1);
          }}
          onCatalogPageChange={setCatalogPage}
          onDlqReasonChange={(r) => {
            setDlqReason(r);
            setDlqPage(1);
          }}
          onDlqPageChange={setDlqPage}
          onHistoryPageChange={setHistoryPage}
          onRemediateClick={(iss) => setActiveRemediationIssue(iss)}
        />
      </main>

      {/* Footer */}
      <footer className="border-t border-slate-200 bg-white py-4 text-center text-xs text-slate-500">
        Coderpush Catalog Synchronization Engine • High-Concurreny SQLite WAL Mode Pipeline
      </footer>

      {/* Modals */}
      <DemoModal
        open={isScenarioModalOpen}
        onOpenChange={setIsScenarioModalOpen}
        stagedScenarioKey={stagedScenario.key}
        interruptedJob={interruptedJob}
        onSelectScenario={handleSelectScenario}
        onStartImmediately={handleStartImmediately}
        onResumeInterrupted={handleResumeInterrupted}
      />

      <RemediationModal
        issue={activeRemediationIssue}
        onOpenChange={(op) => !op && setActiveRemediationIssue(null)}
        onSubmitRemediation={handleRemediateProduct}
      />

      <MetricDrilldownModal
        category={drilldownCategory}
        onOpenChange={(op) => !op && setDrilldownCategory(null)}
        catalogProducts={catalog.products}
        dlqIssues={dlq.issues}
      />

      <ShopModal
        open={isShopModalOpen}
        onOpenChange={setIsShopModalOpen}
        currentShopId={shopId}
        onSelectShop={handleSelectShop}
      />
    </div>
  );
};

export default App;
