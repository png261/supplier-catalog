import React, { useState, useEffect, useRef } from 'react';
import { AppSidebar, NavItemKey } from './components/layout/AppSidebar';
import { AppHeader } from './components/layout/AppHeader';
import { ThemeProvider } from './context/ThemeContext';
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
import { ShopGatekeeper } from './components/ShopGatekeeper';
import { Tabs, TabsList, TabsTrigger, TabsContent } from './components/ui/tabs';
import { Card, CardHeader, CardTitle, CardContent } from './components/ui/card';
import { Button } from './components/ui/button';
import { Badge } from './components/ui/badge';
import { Toaster, toast } from 'sonner';
import {
  Play,
  PlayCircle,
  RotateCw,
  SlidersHorizontal,
  Flame,
  Zap,
  FileCode,
  AlertTriangle,
  CheckCircle2,
  Package,
  Layers,
} from 'lucide-react';

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
  clearActiveShopId,
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

export const AppContent: React.FC = () => {
  // --- Shop Tenant Isolation ---
  const [shopId, setShopId] = useState<string>(() => getActiveShopId());
  const [isShopModalOpen, setIsShopModalOpen] = useState<boolean>(false);

  // --- Sidebar & Layout Navigation ---
  const [activeTab, setActiveTab] = useState<NavItemKey>('overview');
  const [isSidebarCollapsed, setIsSidebarCollapsed] = useState<boolean>(false);
  const [isMobileSidebarOpen, setIsMobileSidebarOpen] = useState<boolean>(false);

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
  }, [historyPage, shopId]);

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
      const res = await fetchHistory(historyLimit, offset);
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
        time_series: [],
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
    if (stagedScenario.key === 'custom') {
      if (!customRecordsStaged || customRecordsStaged.length === 0) {
        toast.error('No custom records staged. Please select scenario and provide JSON payload.');
        setIsScenarioModalOpen(true);
        return;
      }
      payload = {
        mode: 'batches',
        batches: [{ batch_index: 0, records: customRecordsStaged }],
      };
      autoHaltBatchRef.current = null;
    } else {
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
    }

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

  const handleSelectScenario = (scenario: TestScenario, customRecords?: any[]) => {
    setStagedScenario(scenario);
    if (customRecords) {
      setCustomRecordsStaged(customRecords);
    }
    toast.success(`Staged scenario: ${scenario.name}`);
  };

  const handleStartImmediately = async (scenario: TestScenario, customRecords?: any[]) => {
    setStagedScenario(scenario);
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
        onSelectTab={setActiveTab}
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
          sseConnected={sseConnected}
          sseEventCount={sseEventCount}
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
                {activeTab === 'scenarios' && 'Supplier Workload Scenarios'}
              </h1>
              <p className="text-xs sm:text-sm text-slate-500 dark:text-slate-400 mt-0.5">
                Resilient catalog synchronization engine with SQLite WAL atomic transactions.
              </p>
            </div>

            {/* Quick Scenario Launch Button */}
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setIsScenarioModalOpen(true)}
                className="h-8 text-xs font-semibold border-slate-300 dark:border-slate-700 text-slate-700 dark:text-slate-300 hover:bg-slate-100 dark:hover:bg-slate-800"
              >
                <SlidersHorizontal className="h-3.5 w-3.5 mr-1.5 text-slate-500 dark:text-slate-400" />
                Select Scenario
              </Button>
            </div>
          </div>

          {/* Top-level Tabs Bar (Matching satnaing/shadcn-admin) */}
          <Tabs
            value={activeTab}
            onValueChange={(val) => setActiveTab(val as NavItemKey)}
            className="w-full space-y-6"
          >
            <div className="overflow-x-auto pb-1">
              <TabsList className="bg-slate-200/60 dark:bg-slate-800/80 p-1 rounded-lg">
                <TabsTrigger value="overview" className="text-xs font-semibold px-3.5 py-1.5">
                  Overview
                </TabsTrigger>
                <TabsTrigger value="catalog" className="text-xs font-semibold px-3.5 py-1.5">
                  Live Catalog ({catalog.total})
                </TabsTrigger>
                <TabsTrigger value="attention" className="text-xs font-semibold px-3.5 py-1.5">
                  Attention (DLQ)
                  {dlq.total > 0 && (
                    <Badge variant="destructive" className="ml-1.5 text-[10px] px-1.5 py-0 h-4">
                      {dlq.total}
                    </Badge>
                  )}
                </TabsTrigger>
                <TabsTrigger value="history" className="text-xs font-semibold px-3.5 py-1.5">
                  Sync History ({history.total})
                </TabsTrigger>
                <TabsTrigger value="scenarios" className="text-xs font-semibold px-3.5 py-1.5">
                  Scenarios (6)
                </TabsTrigger>
              </TabsList>
            </div>

            {/* Tab 1: OVERVIEW DASHBOARD */}
            <TabsContent value="overview" className="space-y-6 m-0 focus-visible:outline-none">
              {/* Active Staged Scenario Bar */}
              <StagedScenarioBar
                scenario={stagedScenario}
                isSyncing={isSyncing}
                isInterrupted={isInterrupted}
                onStartSync={handleStartSync}
                onCancelSync={handleCancelSync}
                onOpenScenarioModal={() => setIsScenarioModalOpen(true)}
              />

              {/* Feed Ingestion Progress */}
              <ProgressBanner
                currentBatch={currentBatch}
                totalBatches={totalBatches}
                throughput={throughput}
                eta={eta}
                elapsedTime={elapsedTime}
                smartStatusText={smartStatusText}
                smartStatusType={smartStatusType}
              />

              {/* Operations Analytics Filter Bar */}
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

              {/* 5 Summary Metric Cards with Drilldowns */}
              <MetricsGrid
                metrics={metrics}
                deltas={deltas}
                onCardClick={(cat) => setDrilldownCategory(cat)}
              />

              {/* Recharts Donut & Velocity Charts */}
              <ChartsSection
                totals={analyticsData.totals}
                timeSeries={analyticsData.time_series}
              />

              {/* Explorer Section with Catalog, DLQ, History */}
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
                activeTab="catalog"
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
            </TabsContent>

            {/* Tab 2: LIVE CATALOG */}
            <TabsContent value="catalog" className="space-y-6 m-0 focus-visible:outline-none">
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
                activeTab="catalog"
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
            </TabsContent>

            {/* Tab 3: ATTENTION / DLQ */}
            <TabsContent value="attention" className="space-y-6 m-0 focus-visible:outline-none">
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
                activeTab="dlq"
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
            </TabsContent>

            {/* Tab 4: SYNC RUN HISTORY */}
            <TabsContent value="history" className="space-y-6 m-0 focus-visible:outline-none">
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
                activeTab="history"
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
            </TabsContent>

            {/* Tab 5: SCENARIOS GALLERY */}
            <TabsContent value="scenarios" className="space-y-6 m-0 focus-visible:outline-none">
              <StagedScenarioBar
                scenario={stagedScenario}
                isSyncing={isSyncing}
                isInterrupted={isInterrupted}
                onStartSync={handleStartSync}
                onCancelSync={handleCancelSync}
                onOpenScenarioModal={() => setIsScenarioModalOpen(true)}
              />

              <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
                {SCENARIOS.map((sc) => {
                  const isStaged = stagedScenario.key === sc.key;
                  return (
                    <Card
                      key={sc.key}
                      onClick={() => handleSelectScenario(sc)}
                      className={`p-4 border transition-all cursor-pointer flex flex-col justify-between ${
                        isStaged
                          ? 'border-indigo-600 dark:border-indigo-500 bg-indigo-50/30 dark:bg-indigo-950/20 ring-1 ring-indigo-600'
                          : 'border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 hover:border-slate-300 dark:hover:border-slate-700'
                      }`}
                    >
                      <div className="space-y-2">
                        <div className="flex items-center justify-between">
                          <span className="font-bold text-sm text-slate-900 dark:text-slate-100">
                            {sc.name}
                          </span>
                          {isStaged && (
                            <Badge variant="success" className="text-[10px]">
                              Active Staged
                            </Badge>
                          )}
                        </div>
                        <p className="text-xs text-slate-500 dark:text-slate-400 line-clamp-3 leading-relaxed">
                          {sc.desc}
                        </p>
                      </div>

                      <div className="pt-4 border-t border-slate-100 dark:border-slate-800 mt-3 flex items-center justify-between text-xs text-slate-500 dark:text-slate-400">
                        <span className="font-mono">{sc.batches} batches</span>
                        <Button
                          size="xs"
                          variant={isStaged ? 'default' : 'outline'}
                          onClick={(e) => {
                            e.stopPropagation();
                            handleStartImmediately(sc);
                          }}
                          className={isStaged ? 'bg-indigo-600 hover:bg-indigo-700 text-white' : ''}
                        >
                          <Play className="h-3 w-3 mr-1 fill-current" />
                          Run
                        </Button>
                      </div>
                    </Card>
                  );
                })}
              </div>
            </TabsContent>
          </Tabs>
        </main>

        {/* Footer */}
        <footer className="border-t border-slate-200 dark:border-slate-800 bg-white dark:bg-slate-900 py-4 text-center text-xs text-slate-500 dark:text-slate-400 transition-colors">
          Coderpush Supplier Catalog Engine • Template Inspired by shadcn-admin • SQLite WAL Mode
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
  return (
    <ThemeProvider defaultTheme="light" storageKey="catalog-engine-theme">
      <AppContent />
    </ThemeProvider>
  );
};

export default App;
