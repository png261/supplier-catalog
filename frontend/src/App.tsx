import React, { useState, useEffect, useRef } from 'react';
import { AppSidebar, NavItemKey } from './components/layout/AppSidebar';
import { AppHeader } from './components/layout/AppHeader';
import { OverviewView } from './components/views/OverviewView';
import { CatalogView } from './components/views/CatalogView';
import { AttentionView } from './components/views/AttentionView';
import { HistoryView } from './components/views/HistoryView';
import { DemoModal, SCENARIOS } from './components/DemoModal';
import { RemediationModal } from './components/RemediationModal';
import { MetricDrilldownModal } from './components/MetricDrilldownModal';
import { ShopModal } from './components/ShopModal';
import { ShopGatekeeper } from './components/ShopGatekeeper';
import { Button } from './components/ui/button';
import { Toaster, toast } from 'sonner';
import { Play, RefreshCw, RotateCw, Square } from 'lucide-react';

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
  TimeSeriesPoint,
} from './types';

export const AppContent: React.FC = () => {
  // --- Shop Tenant Isolation ---
  const [shopId, setShopId] = useState<string>(() => getActiveShopId());
  const [isShopModalOpen, setIsShopModalOpen] = useState<boolean>(false);

  // --- Sidebar & Layout Navigation ---
  const [activeTab, setActiveTab] = useState<NavItemKey>(() => {
    const params = new URLSearchParams(window.location.search);
    const p = params.get('page');
    if (p && ['overview', 'catalog', 'attention', 'history'].includes(p)) {
      return p as NavItemKey;
    }
    return 'overview';
  });
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState<boolean>(false);
  const [isMobileSidebarOpen, setIsMobileSidebarOpen] = useState<boolean>(false);

  const navigateToTab = (tab: NavItemKey) => {
    setActiveTab(tab);
    const url = new URL(window.location.href);
    if (tab === 'overview') {
      url.searchParams.delete('page');
    } else {
      url.searchParams.set('page', tab);
    }
    window.history.pushState({ page: tab }, '', url.toString());
  };

  useEffect(() => {
    const handlePopState = () => {
      const params = new URLSearchParams(window.location.search);
      const p = params.get('page');
      if (p && ['overview', 'catalog', 'attention', 'history'].includes(p)) {
        setActiveTab(p as NavItemKey);
      } else {
        setActiveTab('overview');
      }
    };
    window.addEventListener('popstate', handlePopState);
    return () => window.removeEventListener('popstate', handlePopState);
  }, []);

  // --- Server Sent Events & Connection State ---
  const [sseConnected, setSseConnected] = useState<boolean>(false);
  const [sseEventCount, setSseEventCount] = useState<number>(0);
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
    summary: {
      added: 0,
      updated: 0,
      withdrawn: 0,
      skipped: 0,
      dead_letter: 0,
    },
    sync_jobs: [],
    time_series: [],
  });

  // --- Explorer Tables State ---
  const [catalog, setCatalog] = useState<{ products: CatalogProduct[]; total: number }>({
    products: [],
    total: 0,
  });
  const [catalogPage, setCatalogPage] = useState<number>(1);
  const [catalogLimit] = useState<number>(10);
  const [catalogSearch, setCatalogSearch] = useState<string>('');
  const [catalogStatus, setCatalogStatus] = useState<string>('');

  const [dlq, setDlq] = useState<{ issues: DeadLetterIssue[]; total: number }>({
    issues: [],
    total: 0,
  });
  const [dlqPage, setDlqPage] = useState<number>(1);
  const [dlqLimit] = useState<number>(10);
  const [dlqReason, setDlqReason] = useState<string>('');

  const [history, setHistory] = useState<{ jobs: SyncJob[]; total: number }>({
    jobs: [],
    total: 0,
  });
  const [historyPage, setHistoryPage] = useState<number>(1);
  const [historyLimit] = useState<number>(10);
  const [historyStatus, setHistoryStatus] = useState<string>('');

  // --- Modal Open States ---
  const [isScenarioModalOpen, setIsScenarioModalOpen] = useState<boolean>(false);
  const [activeRemediationIssue, setActiveRemediationIssue] = useState<DeadLetterIssue | null>(null);
  const [drilldownCategory, setDrilldownCategory] = useState<
    'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter' | null
  >(null);
  const [isResettingDb, setIsResettingDb] = useState<boolean>(false);

  // --- Setup SSE Connection ---
  useEffect(() => {
    if (!shopId) return;

    if (eventSourceRef.current) {
      eventSourceRef.current.close();
    }

    const sseUrl = getSSEUrl();
    const es = new EventSource(sseUrl);
    eventSourceRef.current = es;

    es.onopen = () => {
      setSseConnected(true);
    };

    es.onerror = () => {
      setSseConnected(false);
    };

    es.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        setSseEventCount((prev) => prev + 1);
        handleSSEEvent(data);
      } catch (err) {
        console.error('Failed to parse SSE message:', err);
      }
    };

    return () => {
      es.close();
    };
  }, [shopId]);

  // --- SSE Event Dispatcher ---
  const handleSSEEvent = (data: any) => {
    switch (data.type) {
      case 'JOB_STARTED':
        setActiveJob({ job_id: data.job_id, status: 'RUNNING' });
        setCurrentBatch(0);
        setTotalBatches(data.total_batches || 10);
        setDeltas({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
        setAnalyticsData((prev) => ({ ...prev, time_series: [] }));
        jobStartTimeRef.current = Date.now();
        startRuntimeTimer();
        setSmartStatusText(`Sync job ${data.job_id} started. Streaming batches from supplier...`);
        setSmartStatusType('running');
        break;

      case 'BATCH_COMMITTED':
        setCurrentBatch(data.batch_index + 1);
        setTotalBatches(data.total_batches || totalBatches);

        if (data.cumulative_summary) {
          setMetrics({
            added: data.cumulative_summary.added || 0,
            updated: data.cumulative_summary.updated || 0,
            withdrawn: data.cumulative_summary.withdrawn || 0,
            skipped: data.cumulative_summary.skipped || 0,
            dead_letter: data.cumulative_summary.dead_letter || 0,
          });
        }

        if (data.batch_summary) {
          setDeltas({
            added: data.batch_summary.added || 0,
            updated: data.batch_summary.updated || 0,
            withdrawn: data.batch_summary.withdrawn || 0,
            skipped: data.batch_summary.skipped || 0,
            dead_letter: data.batch_summary.dead_letter || 0,
          });

          // Append batch product metrics to time_series for real-time chart progression
          const pt: TimeSeriesPoint = {
            timestamp: new Date().toISOString(),
            batch_index: data.batch_index,
            added: data.batch_summary.added || 0,
            updated: data.batch_summary.updated || 0,
            withdrawn: data.batch_summary.withdrawn || 0,
            skipped: data.batch_summary.skipped || 0,
            dead_letter: data.batch_summary.dead_letter || 0,
            processing_speed: 0,
          };
          setAnalyticsData((prev) => {
            const series = [...prev.time_series];
            const idx = series.findIndex((p) => p.batch_index === data.batch_index);
            if (idx >= 0) {
              series[idx] = pt;
            } else {
              series.push(pt);
            }
            return { ...prev, time_series: series };
          });
        }

        // Calculate throughput & ETA
        if (jobStartTimeRef.current) {
          const elapsedSec = Math.max(1, (Date.now() - jobStartTimeRef.current) / 1000);
          const totalProcessed =
            (data.cumulative_summary?.added || 0) +
            (data.cumulative_summary?.updated || 0) +
            (data.cumulative_summary?.withdrawn || 0) +
            (data.cumulative_summary?.skipped || 0) +
            (data.cumulative_summary?.dead_letter || 0);

          const speed = Math.round(totalProcessed / elapsedSec);
          setThroughput(speed);

          const remainingBatches = Math.max(0, (data.total_batches || totalBatches) - (data.batch_index + 1));
          if (speed > 0 && remainingBatches > 0) {
            const avgBatchSize = stagedScenario.size || 100;
            const remainingRecords = remainingBatches * avgBatchSize;
            const estSeconds = Math.ceil(remainingRecords / speed);
            setEta(`${estSeconds}s`);
          } else {
            setEta('—');
          }
        }

        setSmartStatusText(
          `Batch ${data.batch_index + 1}/${data.total_batches || totalBatches} committed atomically into SQLite WAL.`
        );
        setSmartStatusType('running');

        // Mid-Feed Crash Simulation Check
        if (
          autoHaltBatchRef.current !== null &&
          data.batch_index + 1 >= autoHaltBatchRef.current &&
          activeJob &&
          activeJob.status === 'RUNNING'
        ) {
          autoHaltBatchRef.current = null;
          cancelJob(activeJob.job_id).then(() => {
            toast.warning('Crash simulated! Sync abruptly halted midway.');
            setSmartStatusText('Simulated Mid-Feed Crash! Engine saved checkpoint in SQLite WAL.');
            setSmartStatusType('warning');
          });
        }
        break;

      case 'JOB_COMPLETED':
        setActiveJob(null);
        stopRuntimeTimer();
        setEta('Done');
        setSmartStatusText(`Sync job ${data.job_id} successfully completed. Catalog fully updated!`);
        setSmartStatusType('success');
        toast.success(`Job ${data.job_id} completed successfully!`);
        loadAllData();
        loadAnalytics();
        break;

      case 'JOB_INTERRUPTED':
        setActiveJob(null);
        stopRuntimeTimer();
        setSmartStatusText(`Job ${data.job_id} was interrupted. Checkpoint preserved for resumption.`);
        setSmartStatusType('warning');
        toast.warning(`Job ${data.job_id} interrupted!`);
        loadInterruptedJob();
        loadAllData();
        loadAnalytics();
        break;

      case 'JOB_FAILED':
        setActiveJob(null);
        stopRuntimeTimer();
        setSmartStatusText(`Job ${data.job_id} failed: ${data.error || 'Unknown error'}`);
        setSmartStatusType('warning');
        toast.error(`Job failed: ${data.error || 'Unknown error'}`);
        loadAllData();
        loadAnalytics();
        break;

      default:
        break;
    }
  };

  // --- Runtime Timer ---
  const startRuntimeTimer = () => {
    if (runtimeTimerRef.current) clearInterval(runtimeTimerRef.current);
    const start = Date.now();
    runtimeTimerRef.current = setInterval(() => {
      const elapsed = Math.floor((Date.now() - start) / 1000);
      const mins = String(Math.floor(elapsed / 60)).padStart(2, '0');
      const secs = String(elapsed % 60).padStart(2, '0');
      setElapsedTime(`${mins}:${secs}`);
    }, 1000);
  };

  const stopRuntimeTimer = () => {
    if (runtimeTimerRef.current) {
      clearInterval(runtimeTimerRef.current);
      runtimeTimerRef.current = null;
    }
  };

  // --- Initial Data Load & Refresh ---
  useEffect(() => {
    if (shopId) {
      loadAllData();
      loadAnalytics();
      loadInterruptedJob();
    }
  }, [shopId]);

  useEffect(() => {
    if (shopId) {
      loadCatalog();
    }
  }, [catalogPage, catalogSearch, catalogStatus, shopId]);

  useEffect(() => {
    if (shopId) {
      loadDLQ();
    }
  }, [dlqPage, dlqReason, shopId]);

  useEffect(() => {
    if (shopId) {
      loadHistory();
    }
  }, [historyPage, historyStatus, shopId]);

  useEffect(() => {
    if (shopId) {
      loadAnalytics();
    }
  }, [analyticsRange, selectedJobId, shopId]);

  const loadAllData = () => {
    loadCatalog();
    loadDLQ();
    loadHistory();
  };

  const loadCatalog = async () => {
    try {
      const offset = (catalogPage - 1) * catalogLimit;
      const res = await fetchCatalog(catalogLimit, offset, catalogSearch, catalogStatus);
      setCatalog({ products: res.products || [], total: res.total || 0 });
    } catch (err) {
      console.error('Failed to load catalog:', err);
    }
  };

  const loadDLQ = async () => {
    try {
      const offset = (dlqPage - 1) * dlqLimit;
      const res = await fetchDLQ(dlqLimit, offset, dlqReason);
      setDlq({ issues: res.issues || [], total: res.total || 0 });
    } catch (err) {
      console.error('Failed to load DLQ:', err);
    }
  };

  const loadHistory = async () => {
    try {
      const offset = (historyPage - 1) * historyLimit;
      const res = await fetchHistory(historyLimit, offset, historyStatus);
      setHistory({ jobs: res.jobs || [], total: res.total || 0 });
    } catch (err) {
      console.error('Failed to load history:', err);
    }
  };

  const loadInterruptedJob = async () => {
    try {
      const res = await fetchInterruptedJobs();
      if (res.jobs && res.jobs.length > 0) {
        setInterruptedJob(res.jobs[0]);
      } else {
        setInterruptedJob(null);
      }
    } catch (err) {
      console.error('Failed to fetch interrupted jobs:', err);
    }
  };

  const loadAnalytics = async () => {
    setIsRefreshingAnalytics(true);
    try {
      const res = await fetchAnalytics({
        range: analyticsRange,
        from: analyticsRange === 'custom' ? analyticsFrom : undefined,
        to: analyticsRange === 'custom' ? analyticsTo : undefined,
        jobId: selectedJobId !== 'all' ? selectedJobId : undefined,
      });
      const summary = res.metrics?.summary || {
        added: 0,
        updated: 0,
        withdrawn: 0,
        skipped: 0,
        dead_letter: 0,
      };
      setMetrics({
        added: summary.added || 0,
        updated: summary.updated || 0,
        withdrawn: summary.withdrawn || 0,
        skipped: summary.skipped || 0,
        dead_letter: summary.dead_letter || 0,
      });
      setAnalyticsData({
        period: res.filter?.time_range || 'today',
        job_id: selectedJobId,
        time_range: { start: res.filter?.from || '', end: res.filter?.to || '' },
        totals: {
          total_jobs: res.metrics?.total_syncs || 0,
          active_products: summary.added || 0,
          withdrawn_products: summary.withdrawn || 0,
          quarantined_issues: summary.dead_letter || 0,
          total_events_processed: res.metrics?.total_processed || 0,
          total_events_succeeded:
            (res.metrics?.total_processed || 0) - (summary.dead_letter || 0),
          defect_rate: 100 - (res.metrics?.quality_score || 100),
        },
        summary: summary,
        time_series: (res.sync_jobs || []).slice(0, 10).reverse().map((j: any, idx: number) => ({
          timestamp: j.created_at || new Date().toISOString(),
          batch_index: idx,
          added: j.summary?.added || 0,
          updated: j.summary?.updated || 0,
          withdrawn: j.summary?.withdrawn || 0,
          skipped: j.summary?.skipped || 0,
          dead_letter: j.summary?.dead_letter || 0,
          processing_speed: 0,
        })),
        sync_jobs: res.sync_jobs || [],
      });
    } catch (err) {
      console.error('Failed to load analytics:', err);
    } finally {
      setIsRefreshingAnalytics(false);
    }
  };

  // --- Handlers ---
  const handleSelectShop = (newShopId: string) => {
    setActiveShopId(newShopId);
    setShopId(newShopId);
    toast.success(`Switched to shop: ${newShopId}`);
  };

  const handleStartSync = async () => {
    if (interruptedJob) {
      handleResumeInterrupted(interruptedJob.job_id);
      return;
    }

    let payload: any;
    if (stagedScenario.key === 'interrupted') {
      autoHaltBatchRef.current = 5;
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

    try {
      const res = await startJob(payload);
      toast.success(`Synchronization started: ${res.job_id}`);
    } catch (err: any) {
      toast.error(err.message || 'Failed to start job');
    }
  };

  const handleCancelSync = async () => {
    if (!activeJob) return;
    try {
      await cancelJob(activeJob.job_id);
      toast.info('Cancellation signal sent to engine.');
    } catch (err: any) {
      toast.error(err.message || 'Failed to cancel job');
    }
  };

  const handleSelectScenario = (scenario: TestScenario) => {
    setStagedScenario(scenario);
    toast.success(`Staged scenario: ${scenario.name}`);
  };

  const handleStartImmediately = async (scenario: TestScenario) => {
    setStagedScenario(scenario);
    let payload: any;
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

  // If no shop is active, show the shop selection / gatekeeper screen
  if (!shopId) {
    return (
      <>
        <Toaster position="top-right" richColors />
        <ShopGatekeeper onSelectShop={handleSelectShop} />
      </>
    );
  }

  return (
    <div className="min-h-screen bg-slate-50 dark:bg-slate-950 flex font-sans text-slate-900 dark:text-slate-100 transition-colors">
      {/* Toast notifications */}
      <Toaster position="top-right" richColors />

      {/* App Sidebar (shadcn-admin architecture) */}
      <AppSidebar
        activeTab={activeTab}
        onSelectTab={navigateToTab}
        openScenarioModal={(pre) => {
          setIsScenarioModalOpen(true);
        }}
        isCollapsed={isSidebarCollapsed}
        onToggleCollapse={() => setIsSidebarCollapsed(!isSidebarCollapsed)}
        isMobileOpen={isMobileSidebarOpen}
        onCloseMobile={() => setIsMobileSidebarOpen(false)}
        catalogCount={catalog.total}
        quarantinedCount={dlq.total}
        historyCount={history.total}
        activeShopId={shopId}
        onOpenShopModal={() => setIsShopModalOpen(true)}
      />

      {/* Main Layout Area */}
      <div
        className={`flex-1 flex flex-col min-w-0 transition-all duration-300 ${
          isSidebarCollapsed ? 'lg:pl-16' : 'lg:pl-64'
        }`}
      >
        {/* Top Header */}
        <AppHeader
          activeTab={activeTab}
          onOpenMobileSidebar={() => setIsMobileSidebarOpen(true)}
          openScenarioModal={() => setIsScenarioModalOpen(true)}
          onResetDatabase={handleResetDb}
          activeJobId={activeJob ? activeJob.job_id : null}
        />

        {/* Content Body */}
        <main className="flex-1 p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1600px] w-full mx-auto">
          {/* Top Page Title & Nav Tabs Bar (satnaing/shadcn-admin style) */}
          <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4 pb-2 border-b border-slate-200/80 dark:border-slate-800">
            <div>
              <h1 className="text-xl sm:text-2xl font-bold tracking-tight text-slate-900 dark:text-slate-100">
                {activeTab === 'overview' && 'Dashboard Overview'}
                {activeTab === 'catalog' && 'Live Catalog Management'}
                {activeTab === 'attention' && 'Quarantined Attention (DLQ)'}
                {activeTab === 'history' && 'Sync Run Audit Trail'}
              </h1>
              <p className="text-xs sm:text-sm text-slate-500 dark:text-slate-400 mt-0.5">
                {activeTab === 'overview' && 'Comprehensive operations metrics, live ingestion telemetry, and SQLite WAL health.'}
                {activeTab === 'catalog' && 'Search, filter, and inspect product catalog state synchronized from supplier feeds.'}
                {activeTab === 'attention' && 'Invariant D dead-letter isolation: review and remediate defective supplier records.'}
                {activeTab === 'history' && 'Immutable audit log of historical sync sessions and batch transaction checkpoints.'}
              </p>
            </div>

            {/* Quick Action for Dashboard Overview */}
            {activeTab === 'overview' && (
              <div className="flex items-center gap-2">
                {isSyncing ? (
                  <div className="flex items-center gap-1.5">
                    <Button
                      size="sm"
                      disabled
                      className="h-8 text-xs font-semibold bg-blue-600 text-white opacity-90 cursor-not-allowed"
                    >
                      <RefreshCw className="h-3.5 w-3.5 mr-1.5 animate-spin" />
                      Syncing...
                    </Button>
                    <Button
                      variant="outline"
                      size="sm"
                      onClick={handleCancelSync}
                      className="h-8 text-xs font-semibold border-rose-300 text-rose-600 hover:bg-rose-50 hover:text-rose-700 dark:border-rose-900/60 dark:text-rose-400"
                    >
                      <Square className="h-3 w-3 mr-1 fill-current" />
                      Halt
                    </Button>
                  </div>
                ) : isInterrupted ? (
                  <Button
                    size="sm"
                    onClick={handleStartSync}
                    className="h-8 text-xs font-semibold bg-amber-600 hover:bg-amber-700 text-white shadow-xs"
                  >
                    <RotateCw className="h-3.5 w-3.5 mr-1.5" />
                    Resume Sync
                  </Button>
                ) : (
                  <Button
                    size="sm"
                    onClick={handleStartSync}
                    className="h-8 text-xs font-semibold bg-blue-600 hover:bg-blue-700 text-white shadow-xs"
                  >
                    <Play className="h-3.5 w-3.5 mr-1.5 fill-current" />
                    Start Sync
                  </Button>
                )}
              </div>
            )}
          </div>

          {/* Dedicated Page View Content (Navigated via Sidebar) */}
          <div className="w-full space-y-6">
            {/* 1. DEDICATED OVERVIEW DASHBOARD VIEW */}
            {activeTab === 'overview' && (
              <OverviewView
                currentBatch={currentBatch}
                totalBatches={totalBatches}
                throughput={throughput}
                eta={eta}
                elapsedTime={elapsedTime}
                smartStatusText={smartStatusText}
                smartStatusType={smartStatusType}
                isSyncing={isSyncing}
                isInterrupted={isInterrupted}
                onStartSync={handleStartSync}
                onCancelSync={handleCancelSync}
                analyticsRange={analyticsRange}
                analyticsFrom={analyticsFrom}
                analyticsTo={analyticsTo}
                selectedJobId={selectedJobId}
                analyticsData={analyticsData}
                metrics={metrics}
                deltas={deltas}
                isRefreshingAnalytics={isRefreshingAnalytics}
                onRangeChange={setAnalyticsRange}
                onCustomDatesApply={(f, t) => {
                  setAnalyticsFrom(f);
                  setAnalyticsTo(t);
                  loadAnalytics();
                }}
                onJobSelect={setSelectedJobId}
                onRefreshAnalytics={loadAnalytics}
                onMetricCardClick={(cat) => setDrilldownCategory(cat)}
                onNavigateToTab={(tab) => navigateToTab(tab as NavItemKey)}
              />
            )}

            {/* 2. DEDICATED LIVE CATALOG VIEW */}
            {activeTab === 'catalog' && (
              <CatalogView
                products={catalog.products}
                total={catalog.total}
                page={catalogPage}
                limit={catalogLimit}
                search={catalogSearch}
                status={catalogStatus}
                onSearchChange={(s) => {
                  setCatalogSearch(s);
                  setCatalogPage(1);
                }}
                onStatusChange={(st) => {
                  setCatalogStatus(st);
                  setCatalogPage(1);
                }}
                onPageChange={setCatalogPage}
                onResetFilters={() => {
                  setCatalogSearch('');
                  setCatalogStatus('');
                  setCatalogPage(1);
                }}
              />
            )}

            {/* 3. DEDICATED ATTENTION / DLQ REMEDIATION VIEW */}
            {activeTab === 'attention' && (
              <AttentionView
                issues={dlq.issues}
                total={dlq.total}
                page={dlqPage}
                limit={dlqLimit}
                reason={dlqReason}
                onReasonChange={(r) => {
                  setDlqReason(r);
                  setDlqPage(1);
                }}
                onPageChange={setDlqPage}
                onRemediateClick={(iss) => setActiveRemediationIssue(iss)}
              />
            )}

            {/* 4. DEDICATED SYNC RUN AUDIT HISTORY VIEW */}
            {activeTab === 'history' && (
              <HistoryView
                jobs={history.jobs}
                total={history.total}
                page={historyPage}
                limit={historyLimit}
                status={historyStatus}
                onStatusChange={(st) => {
                  setHistoryStatus(st);
                  setHistoryPage(1);
                }}
                onPageChange={setHistoryPage}
                onResumeJob={handleResumeInterrupted}
                isSyncing={isSyncing}
              />
            )}
          </div>
        </main>

        {/* Footer */}
        <footer className="border-t border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 py-4 text-center text-xs text-slate-500 dark:text-slate-400 transition-colors">
          Coderpush Supplier Catalog Engine • Single-Responsibility Modular Architecture • SQLite WAL Mode
        </footer>
      </div>

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

export const App: React.FC = () => {
  return <AppContent />;
};

export default App;
