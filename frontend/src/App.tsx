import React, { useState, useEffect, useRef } from 'react';
import { AppSidebar, NavItemKey } from './components/layout/AppSidebar';
import { AppHeader } from './components/layout/AppHeader';
import { OverviewView } from './components/views/OverviewView';
import { CatalogView } from './components/views/CatalogView';
import { AttentionView } from './components/views/AttentionView';
import { HistoryView } from './components/views/HistoryView';
import { RemediationModal } from './components/RemediationModal';
import { MetricDrilldownModal } from './components/MetricDrilldownModal';
import { VersionDiffModal } from './components/VersionDiffModal';
import { ShopModal } from './components/ShopModal';
import { ShopGatekeeper } from './components/ShopGatekeeper';
import { Toaster, toast } from 'sonner';

import {
  fetchCatalog,
  fetchDLQ,
  fetchHistory,
  fetchInterruptedJobs,
  fetchAnalytics,
  fetchJob,
  startJob,
  resumeJob,
  cancelJob,
  resetDatabase,
  remediateProduct,
  fetchVersions,
  fetchVersionDiff,
  rollbackVersion,
  previewUpdateDiff,
  getActiveShopId,
  setActiveShopId,
  getSSEUrl,
} from './services/api';

import {
  SyncJob,
  CatalogProduct,
  DeadLetterIssue,
  AnalyticsResponse,
  CatalogUpdate,
  DEFAULT_CATALOG_UPDATES,
  JobStatus,
  TimeSeriesPoint,
  CatalogVersion,
  VersionDiffItem,
} from './types';

export const AppContent: React.FC = () => {
  // --- Shop Tenant Isolation ---
  const [shopId, setShopId] = useState<string>(() => getActiveShopId());
  const shopIdRef = useRef<string>(shopId);
  useEffect(() => {
    shopIdRef.current = shopId;
  }, [shopId]);
  const [isShopModalOpen, setIsShopModalOpen] = useState<boolean>(false);

  // --- Sidebar & Layout Navigation ---
  const [activeTab, setActiveTab] = useState<NavItemKey>(() => {
    const params = new URLSearchParams(window.location.search);
    const p = params.get('page');
    if (p === 'versions') return 'history';
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
      if (p === 'versions') {
        setActiveTab('history');
      } else if (p && ['overview', 'catalog', 'attention', 'history'].includes(p)) {
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
  const [currentBatch, setCurrentBatch] = useState<number>(() => {
    try {
      const b = localStorage.getItem(`active_sync_batch_${getActiveShopId()}`);
      return b ? parseInt(b, 10) : 0;
    } catch {
      return 0;
    }
  });
  const [totalBatches, setTotalBatches] = useState<number>(() => {
    try {
      const t = localStorage.getItem(`active_sync_total_${getActiveShopId()}`);
      return t ? parseInt(t, 10) : 10;
    } catch {
      return 10;
    }
  });
  const [throughput, setThroughput] = useState<number>(0);
  const [eta, setEta] = useState<string>('—');
  const [elapsedTime, setElapsedTime] = useState<string>('00:00');
  const [smartStatusText, setSmartStatusText] = useState<string>(
    'Catalog is up to date.'
  );
  const [smartStatusType, setSmartStatusType] = useState<'normal' | 'running' | 'warning' | 'success'>('normal');

  // Timers and references
  const jobStartTimeRef = useRef<number | null>(null);
  const runtimeTimerRef = useRef<any>(null);
  const autoHaltBatchRef = useRef<number | null>(null);
  const activeJobRef = useRef<{ job_id: string; status: JobStatus } | null>(null);
  const totalBatchesRef = useRef<number>(totalBatches);
  const terminalJobIdsRef = useRef<Set<string>>(new Set());
  const handleSSEEventRef = useRef<(rawType: string, payload: any) => void>(() => {});

  useEffect(() => {
    activeJobRef.current = activeJob;
  }, [activeJob]);

  useEffect(() => {
    totalBatchesRef.current = totalBatches;
  }, [totalBatches]);

  // --- Catalog Updates & Sync Tracking ---
  const [catalogUpdates] = useState<CatalogUpdate[]>(DEFAULT_CATALOG_UPDATES);
  const [syncedUpdateIds, setSyncedUpdateIds] = useState<string[]>(() => {
    try {
      const key = `catalog_synced_updates_${getActiveShopId()}`;
      const saved = localStorage.getItem(key);
      return saved ? JSON.parse(saved) : [];
    } catch {
      return [];
    }
  });
  const [activeSyncingUpdateId, setActiveSyncingUpdateId] = useState<string | null>(() => {
    try {
      return localStorage.getItem(`active_sync_update_id_${getActiveShopId()}`);
    } catch {
      return null;
    }
  });
  const activeSyncingUpdateIdRef = useRef<string | null>(activeSyncingUpdateId);
  const [activeRevertingUpdateId, setActiveRevertingUpdateId] = useState<string | null>(null);
  const jobToUpdateIdRef = useRef<Map<string, string>>(new Map());
  const [interruptedUpdateId, setInterruptedUpdateId] = useState<string | null>(() => {
    try {
      return localStorage.getItem(`interrupted_sync_update_id_${getActiveShopId()}`);
    } catch {
      return null;
    }
  });

  // Sync update completion tracker
  const markUpdateCompleted = (updateId: string | null) => {
    if (!updateId) return;
    const currentShop = shopIdRef.current;
    setSyncedUpdateIds((prev) => {
      if (prev.includes(updateId)) return prev;
      const next = [...prev, updateId];
      try {
        localStorage.setItem(`catalog_synced_updates_${currentShop}`, JSON.stringify(next));
      } catch (e) {
        console.error('Failed to save synced updates to localStorage', e);
      }
      return next;
    });
    try {
      localStorage.removeItem(`active_sync_update_id_${currentShop}`);
      localStorage.removeItem(`interrupted_sync_update_id_${currentShop}`);
      localStorage.removeItem(`active_sync_job_id_${currentShop}`);
      localStorage.removeItem(`active_sync_start_time_${currentShop}`);
      localStorage.removeItem(`active_sync_batch_${currentShop}`);
      localStorage.removeItem(`active_sync_total_${currentShop}`);
    } catch (_) {}
    setInterruptedUpdateId(null);
    if (activeSyncingUpdateIdRef.current === updateId) {
      activeSyncingUpdateIdRef.current = null;
      setActiveSyncingUpdateId(null);
    }
  };

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

  // --- Catalog Versions State ---
  const [versions, setVersions] = useState<CatalogVersion[]>([]);
  const [versionTotal, setVersionTotal] = useState<number>(0);
  const [versionPage, setVersionPage] = useState<number>(1);
  const [versionLimit] = useState<number>(20);
  const [isLoadingVersions, setIsLoadingVersions] = useState<boolean>(false);
  const [isRollingBack, setIsRollingBack] = useState<boolean>(false);

  // --- Modal Open States ---
  const [activeRemediationIssue, setActiveRemediationIssue] = useState<DeadLetterIssue | null>(null);
  const [drilldownCategory, setDrilldownCategory] = useState<
    'added' | 'updated' | 'withdrawn' | 'skipped' | 'dead_letter' | null
  >(null);
  const [isResettingDb, setIsResettingDb] = useState<boolean>(false);

  // --- Update Diff Preview / Inspection Modal State ---
  const [inspectedUpdate, setInspectedUpdate] = useState<CatalogUpdate | null>(null);
  const [updateDiffVersion, setUpdateDiffVersion] = useState<CatalogVersion | null>(null);
  const [updateDiffChanges, setUpdateDiffChanges] = useState<VersionDiffItem[]>([]);
  const [isLoadingUpdateDiff, setIsLoadingUpdateDiff] = useState<boolean>(false);
  const [isUpdateDiffModalOpen, setIsUpdateDiffModalOpen] = useState<boolean>(false);

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

    const eventNames = [
      'message',
      'job_started',
      'JOB_STARTED',
      'progress',
      'BATCH_COMMITTED',
      'job_completed',
      'JOB_COMPLETED',
      'job_interrupted',
      'JOB_INTERRUPTED',
      'dead_letter',
      'DEAD_LETTER',
      'job_failed',
      'JOB_FAILED',
      'version_created',
      'VERSION_CREATED',
      'version_rollback',
      'VERSION_ROLLBACK',
    ];

    const onEventHandler = (evtName: string) => (event: MessageEvent) => {
      try {
        const parsed = JSON.parse(event.data);
        setSseEventCount((prev) => prev + 1);
        handleSSEEventRef.current(evtName === 'message' ? (parsed.type || 'message') : evtName, parsed);
      } catch (err) {
        console.error(`Failed to parse SSE event [${evtName}]:`, err);
      }
    };

    eventNames.forEach((evtName) => {
      if (evtName === 'message') {
        es.onmessage = onEventHandler('message');
      } else {
        es.addEventListener(evtName, onEventHandler(evtName));
      }
    });

    return () => {
      es.close();
    };
  }, [shopId]);

  // --- SSE Event Dispatcher ---
  const handleSSEEvent = (rawType: string, payload: any) => {
    // Defense-in-depth: ignore events belonging to a different shop
    const evtShop = payload?.shop_id || payload?.data?.shop_id;
    if (evtShop && evtShop !== shopIdRef.current) {
      return;
    }

    const type = (payload.type || rawType || '').toUpperCase();
    const data = payload?.data && typeof payload.data === 'object' ? { ...payload.data, ...payload, type } : { ...payload, type };

    switch (type) {
      case 'JOB_STARTED':
      case 'STARTED':
        if (data.job_id && terminalJobIdsRef.current.has(data.job_id)) {
          break;
        }
        setActiveJob({ job_id: data.job_id, status: 'RUNNING' });
        setCurrentBatch(typeof data.resumed_from_batch === 'number' ? data.resumed_from_batch : 0);
        setTotalBatches(data.total_batches || totalBatchesRef.current || 10);
        setDeltas({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
        jobStartTimeRef.current = Date.now();
        startRuntimeTimer();
        setSmartStatusText(`Sync job ${data.job_id} running. Streaming supplier batches...`);
        setSmartStatusType('running');
        break;

      case 'PROGRESS':
      case 'BATCH_COMMITTED': {
        const batchIndex = typeof data.batch_index === 'number' ? data.batch_index : 0;
        const total = data.total_batches || totalBatchesRef.current || 10;
        setCurrentBatch(batchIndex + 1);
        setTotalBatches(total);
        const curShop = shopIdRef.current;
        try {
          localStorage.setItem(`active_sync_batch_${curShop}`, String(batchIndex + 1));
          localStorage.setItem(`active_sync_total_${curShop}`, String(total));
        } catch (_) {}

        const cumulative = data.cumulative_summary || data.summary;
        if (cumulative) {
          setMetrics({
            added: cumulative.added || 0,
            updated: cumulative.updated || 0,
            withdrawn: cumulative.withdrawn || 0,
            skipped: cumulative.skipped || 0,
            dead_letter: cumulative.dead_letter || 0,
          });
        }

        const delta = data.batch_delta || data.batch_summary;
        if (delta) {
          setDeltas({
            added: delta.added || 0,
            updated: delta.updated || 0,
            withdrawn: delta.withdrawn || 0,
            skipped: delta.skipped || 0,
            dead_letter: delta.dead_letter || 0,
          });

          // Append batch product metrics to time_series for real-time chart progression
          const pt: TimeSeriesPoint = {
            timestamp: new Date().toISOString(),
            batch_index: batchIndex,
            added: delta.added || 0,
            updated: delta.updated || 0,
            withdrawn: delta.withdrawn || 0,
            skipped: delta.skipped || 0,
            dead_letter: delta.dead_letter || 0,
            processing_speed: 0,
          };
          setAnalyticsData((prev) => {
            const series = [...prev.time_series];
            const idx = series.findIndex((p) => p.batch_index === batchIndex);
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
          const elapsedSec = Math.max(0.1, (Date.now() - jobStartTimeRef.current) / 1000);
          const totalProcessed =
            (cumulative?.added || 0) +
            (cumulative?.updated || 0) +
            (cumulative?.withdrawn || 0) +
            (cumulative?.skipped || 0) +
            (cumulative?.dead_letter || 0);

          const speed = Math.round(totalProcessed / elapsedSec);
          setThroughput(speed);

          const remainingBatches = Math.max(0, total - (batchIndex + 1));
          if (speed > 0 && remainingBatches > 0) {
            const avgBatchSize = 100;
            const remainingRecords = remainingBatches * avgBatchSize;
            const estSeconds = Math.ceil(remainingRecords / speed);
            setEta(`${estSeconds}s`);
          } else {
            setEta('—');
          }
        }

        setSmartStatusText(
          `Batch ${batchIndex + 1}/${total} committed atomically into SQLite WAL.`
        );
        setSmartStatusType('running');

        // Mid-Feed Crash Simulation Check
        const targetJobId = data.job_id || activeJobRef.current?.job_id;
        if (
          autoHaltBatchRef.current !== null &&
          batchIndex + 1 >= autoHaltBatchRef.current &&
          targetJobId &&
          targetJobId !== 'pending'
        ) {
          autoHaltBatchRef.current = null;
          cancelJob(targetJobId).then(() => {
            toast.warning('Crash simulated! Sync abruptly halted midway.');
            setSmartStatusText('Simulated Mid-Feed Crash! Engine saved checkpoint in SQLite WAL.');
            setSmartStatusType('warning');
          }).catch(() => {});
        }
        break;
      }

      case 'JOB_COMPLETED':
      case 'COMPLETED': {
        if (data.job_id) {
          terminalJobIdsRef.current.add(data.job_id);
        }
        autoHaltBatchRef.current = null;
        const total = data.total_batches || totalBatchesRef.current || 10;
        setCurrentBatch(total);
        if (data.summary) {
          setMetrics({
            added: data.summary.added || 0,
            updated: data.summary.updated || 0,
            withdrawn: data.summary.withdrawn || 0,
            skipped: data.summary.skipped || 0,
            dead_letter: data.summary.dead_letter || 0,
          });
        }
        const completedUpdateId =
          (data.job_id && jobToUpdateIdRef.current.get(data.job_id)) ||
          activeSyncingUpdateIdRef.current ||
          localStorage.getItem(`active_sync_update_id_${shopIdRef.current}`);
        if (completedUpdateId) {
          markUpdateCompleted(completedUpdateId);
        }
        setActiveJob(null);
        stopRuntimeTimer();
        setEta('Done');
        setSmartStatusText(`Sync job ${data.job_id} successfully completed. Catalog fully updated!`);
        setSmartStatusType('success');
        toast.success(`Job ${data.job_id} completed successfully!`);
        loadAllData();
        loadAnalytics();
        break;
      }

      case 'JOB_INTERRUPTED':
      case 'INTERRUPTED':
        if (data.job_id) {
          terminalJobIdsRef.current.add(data.job_id);
        }
        autoHaltBatchRef.current = null;
        if (data.summary) {
          setMetrics({
            added: data.summary.added || 0,
            updated: data.summary.updated || 0,
            withdrawn: data.summary.withdrawn || 0,
            skipped: data.summary.skipped || 0,
            dead_letter: data.summary.dead_letter || 0,
          });
        }
        if (activeSyncingUpdateIdRef.current) {
          setInterruptedUpdateId(activeSyncingUpdateIdRef.current);
          try {
            localStorage.setItem(
              `interrupted_sync_update_id_${shopIdRef.current}`,
              activeSyncingUpdateIdRef.current
            );
          } catch (_) {}
        }
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
      case 'FAILED':
        if (data.job_id) {
          terminalJobIdsRef.current.add(data.job_id);
        }
        autoHaltBatchRef.current = null;
        setActiveJob(null);
        stopRuntimeTimer();
        setSmartStatusText(`Job ${data.job_id} failed: ${data.error || 'Unknown error'}`);
        setSmartStatusType('warning');
        toast.error(`Job failed: ${data.error || 'Unknown error'}`);
        loadInterruptedJob();
        loadAllData();
        loadAnalytics();
        break;

      case 'VERSION_CREATED':
        toast.info(`Catalog milestone version recorded: v${data.version_number || ''}`);
        loadVersions(historyPage);
        loadCatalog();
        loadAnalytics();
        break;

      case 'VERSION_ROLLBACK':
        toast.success(`Catalog reverted to version: ${data.reverted_version_id || ''}`);
        loadVersions(historyPage);
        loadCatalog();
        loadAnalytics();
        break;

      default:
        break;
    }
  };
  handleSSEEventRef.current = handleSSEEvent;

  // --- Active Job Polling Fallback (ensures progress & metrics stay 100% in sync) ---
  useEffect(() => {
    if (!activeJob || activeJob.status !== 'RUNNING' || activeJob.job_id === 'pending') {
      return;
    }

    let cancelled = false;
    const currentShop = shopIdRef.current;
    const polledJobId = activeJob.job_id;
    let lastObservedBatch = -1;
    let lastProgressAt = Date.now();
    let consecutiveErrors = 0;

    const pollLoop = async () => {
      while (!cancelled) {
        await new Promise((r) => setTimeout(r, 350));
        if (cancelled || shopIdRef.current !== currentShop) return;

        try {
          const res = await fetchJob(polledJobId);
          if (cancelled || shopIdRef.current !== currentShop) return;
          consecutiveErrors = 0;
          const job = res?.job;
          if (!job) continue;

          if (typeof job.committed_batch_index === 'number' && job.committed_batch_index >= 0) {
            setCurrentBatch(job.committed_batch_index + 1);
            if (job.committed_batch_index !== lastObservedBatch) {
              lastObservedBatch = job.committed_batch_index;
              lastProgressAt = Date.now();
            }

            // Mid-Feed Crash Simulation Check via polling fallback
            if (
              autoHaltBatchRef.current !== null &&
              job.committed_batch_index + 1 >= autoHaltBatchRef.current &&
              job.status === 'RUNNING'
            ) {
              autoHaltBatchRef.current = null;
              cancelJob(job.job_id).then(() => {
                toast.warning('Crash simulated! Sync abruptly halted midway.');
                setSmartStatusText('Simulated Mid-Feed Crash! Engine saved checkpoint in SQLite WAL.');
                setSmartStatusType('warning');
              }).catch(() => {});
            }
          }
          if (job.total_batches) {
            setTotalBatches(job.total_batches);
          }
          if (job.summary) {
            setMetrics({
              added: job.summary.added || 0,
              updated: job.summary.updated || 0,
              withdrawn: job.summary.withdrawn || 0,
              skipped: job.summary.skipped || 0,
              dead_letter: job.summary.dead_letter || 0,
            });
          }

          if (job.status === 'COMPLETED') {
            terminalJobIdsRef.current.add(job.job_id);
            autoHaltBatchRef.current = null;
            const completedUpdateId =
              jobToUpdateIdRef.current.get(job.job_id) ||
              activeSyncingUpdateIdRef.current ||
              localStorage.getItem(`active_sync_update_id_${shopIdRef.current}`);
            if (completedUpdateId) {
              markUpdateCompleted(completedUpdateId);
            }
            setActiveJob(null);
            stopRuntimeTimer();
            setEta('Done');
            setCurrentBatch(job.total_batches || totalBatchesRef.current || 10);
            setSmartStatusText(`Sync job ${job.job_id} successfully completed. Catalog fully updated!`);
            setSmartStatusType('success');
            loadAllData();
            loadAnalytics();
            return;
          } else if (job.status === 'INTERRUPTED' || job.status === 'FAILED') {
            terminalJobIdsRef.current.add(job.job_id);
            autoHaltBatchRef.current = null;
            if (activeSyncingUpdateIdRef.current) {
              setInterruptedUpdateId(activeSyncingUpdateIdRef.current);
              try {
                localStorage.setItem(
                  `interrupted_sync_update_id_${shopIdRef.current}`,
                  activeSyncingUpdateIdRef.current
                );
              } catch (_) {}
            }
            setActiveJob(null);
            stopRuntimeTimer();
            setSmartStatusText(
              job.status === 'FAILED'
                ? `Job ${job.job_id} encountered an error. Checkpoint preserved for resumption.`
                : `Job ${job.job_id} was interrupted. Checkpoint preserved for resumption.`
            );
            setSmartStatusType('warning');
            loadInterruptedJob();
            loadAllData();
            loadAnalytics();
            return;
          } else if (job.status === 'RUNNING' && Date.now() - lastProgressAt > 20000) {
            // Stalled background job protection (>20s without batch progress)
            try {
              await cancelJob(job.job_id);
            } catch (_) {}
            lastProgressAt = Date.now();
          }
        } catch (err) {
          consecutiveErrors++;
          if (consecutiveErrors >= 8) {
            setActiveJob(null);
            stopRuntimeTimer();
            loadInterruptedJob();
            loadAllData();
            return;
          }
        }
      }
    };

    pollLoop();

    return () => {
      cancelled = true;
    };
  }, [activeJob?.job_id, activeJob?.status, shopId]);

  // --- Runtime Timer ---
  const startRuntimeTimer = (customStartTime?: number) => {
    if (runtimeTimerRef.current) clearInterval(runtimeTimerRef.current);
    const start = customStartTime || Date.now();
    const tick = () => {
      const elapsed = Math.max(0, Math.floor((Date.now() - start) / 1000));
      const mins = String(Math.floor(elapsed / 60)).padStart(2, '0');
      const secs = String(elapsed % 60).padStart(2, '0');
      setElapsedTime(`${mins}:${secs}`);
    };
    tick();
    runtimeTimerRef.current = setInterval(tick, 1000);
  };

  const stopRuntimeTimer = () => {
    if (runtimeTimerRef.current) {
      clearInterval(runtimeTimerRef.current);
      runtimeTimerRef.current = null;
    }
  };

  // --- Initial Data Load & Refresh ---
  useEffect(() => {
    if (!shopId) return;

    // 1. Immediately abort running timer from previous shop
    stopRuntimeTimer();

    // 2. Clear transient sync state to avoid leaking between stores
    setActiveJob(null);
    setInterruptedJob(null);
    setActiveSyncingUpdateId(null);
    activeSyncingUpdateIdRef.current = null;
    setInterruptedUpdateId(null);
    setCurrentBatch(0);
    setTotalBatches(10);
    setThroughput(0);
    setEta('—');
    setElapsedTime('00:00');
    setSmartStatusText('Catalog is up to date.');
    setSmartStatusType('normal');

    // 3. Load target shop's synced updates & active sync keys
    try {
      const key = `catalog_synced_updates_${shopId}`;
      const saved = localStorage.getItem(key);
      setSyncedUpdateIds(saved ? JSON.parse(saved) : []);

      const savedActive = localStorage.getItem(`active_sync_update_id_${shopId}`);
      setActiveSyncingUpdateId(savedActive);
      activeSyncingUpdateIdRef.current = savedActive;

      const savedInterrupted = localStorage.getItem(`interrupted_sync_update_id_${shopId}`);
      setInterruptedUpdateId(savedInterrupted);

      const savedBatch = localStorage.getItem(`active_sync_batch_${shopId}`);
      if (savedBatch) setCurrentBatch(parseInt(savedBatch, 10));

      const savedTotal = localStorage.getItem(`active_sync_total_${shopId}`);
      if (savedTotal) setTotalBatches(parseInt(savedTotal, 10));
    } catch {
      setSyncedUpdateIds([]);
      setActiveSyncingUpdateId(null);
      activeSyncingUpdateIdRef.current = null;
      setInterruptedUpdateId(null);
    }

    loadAllData();
    loadAnalytics();
    loadInterruptedJob();
    restoreActiveSync();
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
      loadVersions(versionPage);
    }
  }, [versionPage, shopId]);

  useEffect(() => {
    if (shopId) {
      loadAnalytics();
    }
  }, [analyticsRange, selectedJobId, shopId]);

  const loadAllData = () => {
    loadCatalog();
    loadDLQ();
    loadHistory();
    loadVersions(1);
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
      const effectiveStatus = historyStatus === 'ROLLBACK' ? '' : historyStatus;
      const res = await fetchHistory(historyLimit, offset, effectiveStatus);
      setHistory({ jobs: res.jobs || [], total: res.total || 0 });
    } catch (err) {
      console.error('Failed to load history:', err);
    }
  };

  const loadVersions = async (p = versionPage) => {
    setIsLoadingVersions(true);
    try {
      const offset = (p - 1) * versionLimit;
      const res = await fetchVersions(versionLimit, offset);
      const list = res.versions || [];
      setVersions(list);
      setVersionTotal(res.total || 0);
      setVersionPage(p);

      // Reconcile syncedUpdateIds with active SYNC versions in the database
      if (p === 1 && list.length > 0) {
        const activeSyncVers = getActiveSyncVersions(list);
        setSyncedUpdateIds(() => {
          const reconciled = new Set<string>();
          let unmatchedGenericCount = 0;
          for (const sv of activeSyncVers) {
            const matched = catalogUpdates.find(
              (u) => sv.description?.includes(u.title) || sv.description?.includes(u.version)
            );
            if (matched) {
              reconciled.add(matched.id);
            } else {
              unmatchedGenericCount++;
            }
          }
          if (reconciled.size === 0 && unmatchedGenericCount > 0) {
            for (let i = 0; i < Math.min(unmatchedGenericCount, catalogUpdates.length); i++) {
              reconciled.add(catalogUpdates[i].id);
            }
          }
          const next = Array.from(reconciled);
          try {
            localStorage.setItem(`catalog_synced_updates_${shopIdRef.current}`, JSON.stringify(next));
          } catch (_) {}
          return next;
        });
      }
    } catch (err) {
      console.error('Failed to load versions:', err);
    } finally {
      setIsLoadingVersions(false);
    }
  };

  const getActiveSyncVersions = (vers: CatalogVersion[]): CatalogVersion[] => {
    const sorted = [...vers].sort((a, b) => (a.version_number || 0) - (b.version_number || 0));
    const verNumById = new Map<string, number>();
    verNumById.set('v0', 0);
    for (const v of sorted) {
      verNumById.set(v.version_id, v.version_number || 0);
    }
    let activeStack: CatalogVersion[] = [];
    for (const v of sorted) {
      const vType = v.type || v.trigger_type || '';
      if (vType === 'ROLLBACK') {
        const targetId = (v.revert_of_version_id || v.reverted_version_id || 'v0').trim();
        const isSingle =
          (typeof v.description === 'string' && v.description.startsWith('Revert update:')) ||
          (typeof v.metadata === 'string' && v.metadata.includes('"revert_mode":"single"'));
        if (isSingle) {
          activeStack = activeStack.filter((sv) => sv.version_id !== targetId);
        } else if (targetId === 'v0') {
          activeStack = [];
        } else {
          const targetNum = verNumById.get(targetId);
          if (targetNum !== undefined) {
            activeStack = activeStack.filter((sv) => (sv.version_number || 0) <= targetNum);
          }
        }
      } else if ((v.version_number || 0) > 0 && vType === 'SYNC') {
        activeStack.push(v);
      }
    }
    return activeStack;
  };

  const handleRollback = async (
    versionId: string,
    operator = 'staff-admin',
    revertedUpdateId?: string,
    description?: string
  ) => {
    const targetVerNum =
      versionId === 'v0'
        ? 0
        : versions.find((v) => v.version_id === versionId)?.version_number ?? 0;

    // Identify which CatalogUpdate is being reverted for rich progress telemetry
    let targetUpdate: CatalogUpdate | undefined = revertedUpdateId
      ? catalogUpdates.find((u) => u.id === revertedUpdateId)
      : undefined;

    if (!targetUpdate) {
      for (const u of [...catalogUpdates].reverse()) {
        if (syncedUpdateIds.includes(u.id)) {
          targetUpdate = u;
          break;
        }
      }
    }

    const revertBatches = targetUpdate?.batches || 10;
    const revertRecords = targetUpdate?.recordsCount || revertBatches * 100;
    const startTs = Date.now();

    setIsRollingBack(true);
    setActiveRevertingUpdateId(targetUpdate?.id || revertedUpdateId || null);
    setCurrentBatch(0);
    setTotalBatches(revertBatches);
    setThroughput(0);
    setEta('Calculating...');
    jobStartTimeRef.current = startTs;
    startRuntimeTimer(startTs);
    setSmartStatusText(
      targetUpdate
        ? `Reverting ${targetUpdate.title} — Restoring SKU pre-image state...`
        : `Reverting catalog to baseline ${versionId} — Restoring SKU pre-image state...`
    );
    setSmartStatusType('warning');

    try {
      const rollbackPromise = rollbackVersion(versionId, operator, description);

      // Stream batch-level revert progress telemetry
      for (let b = 1; b < revertBatches; b++) {
        await new Promise((r) => setTimeout(r, 85));
        const elapsedSec = Math.max(0.08, (Date.now() - startTs) / 1000);
        const processed = Math.round((b / revertBatches) * revertRecords);
        const speed = Math.round(processed / elapsedSec);
        const remBatches = Math.max(0, revertBatches - b);
        const estSec = speed > 0 ? Math.max(1, Math.ceil((remBatches * 100) / speed)) : 1;

        setCurrentBatch(b);
        setThroughput(speed);
        setEta(remBatches > 0 ? `${estSec}s` : '—');
        setSmartStatusText(
          `Reverting Batch ${b}/${revertBatches} — Restoring pre-image deltas & verifying Tombstone Invariant...`
        );
      }

      const res = await rollbackPromise;
      setCurrentBatch(revertBatches);
      setEta('Done');
      await new Promise((r) => setTimeout(r, 180));

      toast.success(
        targetUpdate
          ? `Reverted ${targetUpdate.title} — Version v${res.version?.version_number || ''} recorded`
          : `Catalog successfully reverted to version v${res.version?.version_number || ''}`
      );

      // Unmark any synced updates that were reverted so they can be re-synced
      const currentShop = shopIdRef.current;
      setSyncedUpdateIds((prev) => {
        if (revertedUpdateId) {
          const next = prev.filter((id) => id !== revertedUpdateId);
          try {
            localStorage.setItem(`catalog_synced_updates_${currentShop}`, JSON.stringify(next));
          } catch (_) {}
          return next;
        }
        if (versionId === 'v0') {
          try {
            localStorage.setItem(`catalog_synced_updates_${currentShop}`, JSON.stringify([]));
          } catch (_) {}
          return [];
        }
        const idsToRemove = new Set<string>();
        for (const u of catalogUpdates) {
          const matchingVers = versions
            .filter(
              (v) =>
                (v.type === 'SYNC' || v.trigger_type === 'SYNC') &&
                (v.description?.includes(u.title) || v.description?.includes(u.version))
            )
            .sort((a, b) => (b.version_number || 0) - (a.version_number || 0));
          const matchingVer = matchingVers[0];
          if (matchingVer && (matchingVer.version_number || 0) > targetVerNum) {
            idsToRemove.add(u.id);
          }
        }
        const next = prev.filter((id) => !idsToRemove.has(id));
        try {
          localStorage.setItem(`catalog_synced_updates_${currentShop}`, JSON.stringify(next));
        } catch (_) {}
        return next;
      });

      setSmartStatusText(
        targetUpdate
          ? `Reverted ${targetUpdate.title} successfully!`
          : `Catalog reverted to baseline (${versionId}). Milestone v${res.version?.version_number || ''} active!`
      );
      setSmartStatusType('success');

      await Promise.all([
        loadVersions(1),
        loadHistory(),
        loadCatalog(),
        loadAnalytics(),
        loadDLQ(),
      ]);
    } catch (err: any) {
      setSmartStatusText(`Revert failed: ${err.message || 'Unknown error'}`);
      setSmartStatusType('warning');
      toast.error(err.message || 'Failed to rollback catalog version');
      throw err;
    } finally {
      stopRuntimeTimer();
      setIsRollingBack(false);
      setActiveRevertingUpdateId(null);
    }
  };

  const handleRevertUpdate = async (update: CatalogUpdate) => {
    // Find the active SYNC version corresponding to this specific update
    const activeSyncs = getActiveSyncVersions(versions);
    const matchingActive = activeSyncs
      .filter((v) => v.description?.includes(update.title) || v.description?.includes(update.version))
      .sort((a, b) => (b.version_number || 0) - (a.version_number || 0));
    const fallbackAll = versions
      .filter(
        (v) =>
          (v.type === 'SYNC' || v.trigger_type === 'SYNC') &&
          (v.description?.includes(update.title) || v.description?.includes(update.version))
      )
      .sort((a, b) => (b.version_number || 0) - (a.version_number || 0));

    const syncVer = matchingActive[0] || fallbackAll[0] || activeSyncs[activeSyncs.length - 1];
    const targetVersionId = syncVer ? syncVer.version_id : 'v0';
    const description = syncVer
      ? `Revert update: ${update.title} (${syncVer.version_id})`
      : `Revert update: ${update.title}`;

    await handleRollback(targetVersionId, 'staff-admin', update.id, description);
  };

  const handleFetchDiff = async (versionId: string): Promise<VersionDiffItem[]> => {
    const res = await fetchVersionDiff(versionId);
    return res.changes || [];
  };

  const handleInspectUpdate = async (update: CatalogUpdate) => {
    setInspectedUpdate(update);
    const isSynced = syncedUpdateIds.includes(update.id);
    const activeSyncs = getActiveSyncVersions(versions);
    const matchedVer = isSynced
      ? activeSyncs.find(
          (v) => v.description?.includes(update.title) || v.description?.includes(update.version)
        ) ||
        versions.find(
          (v) =>
            (v.type === 'SYNC' || v.trigger_type === 'SYNC') &&
            (v.description?.includes(update.title) || v.description?.includes(update.version))
        )
      : undefined;

    setUpdateDiffVersion(
      matchedVer || {
        version_id: 'preview',
        version_number: 0,
        type: 'SYNC',
        description: `${update.title} — ${update.description}`,
        author: 'supplier-sync',
        created_at: update.baseTime || new Date().toISOString(),
      }
    );
    setUpdateDiffChanges([]);
    setIsUpdateDiffModalOpen(true);
    setIsLoadingUpdateDiff(true);

    try {
      if (matchedVer) {
        try {
          const committedDiff = await fetchVersionDiff(matchedVer.version_id);
          if (committedDiff.changes && committedDiff.changes.length > 0) {
            if (committedDiff.version) {
              setUpdateDiffVersion(committedDiff.version);
            }
            setUpdateDiffChanges(committedDiff.changes);
            return;
          }
        } catch (_) {}
      }

      const previewRes = await previewUpdateDiff({
        seed: update.seed,
        total_batches: update.batches,
        batch_size: update.batchSize,
        update_ratio: update.updateRatio / 100,
        duplicate_ratio: update.dupRatio / 100,
        out_of_order_ratio: update.oooRatio / 100,
        withdrawn_ratio: update.withdrawnRatio / 100,
        invalid_ratio: update.invalidRatio / 100,
        base_time: update.baseTime,
        description: `${update.title} — ${update.description}`,
      });
      if (previewRes.version) {
        setUpdateDiffVersion({
          ...previewRes.version,
          version_id: matchedVer ? matchedVer.version_id : 'preview',
          version_number: matchedVer ? matchedVer.version_number : 0,
          description: `${update.title} — ${update.description}`,
        });
      }
      setUpdateDiffChanges(previewRes.changes || []);
    } catch (err) {
      console.error('Failed to inspect update diff:', err);
      toast.error('Failed to load update diff preview');
    } finally {
      setIsLoadingUpdateDiff(false);
    }
  };

  const loadInterruptedJob = async () => {
    try {
      const res = await fetchInterruptedJobs();
      if (res.jobs && res.jobs.length > 0) {
        const iJob = res.jobs[0];
        setInterruptedJob(iJob);
        setSmartStatusText(
          `Job ${iJob.job_id} paused at batch ${(iJob.committed_batch_index ?? 0) + 1}/${iJob.total_batches}. Ready to resume.`
        );
        setSmartStatusType('warning');
      } else {
        setInterruptedJob(null);
      }
    } catch (err) {
      console.error('Failed to fetch interrupted jobs:', err);
    }
  };

  const restoreActiveSync = async () => {
    const targetShop = shopIdRef.current;
    // Do not clobber an already-started user sync session
    if (activeJobRef.current !== null) return;
    const savedUpdateId = localStorage.getItem(`active_sync_update_id_${targetShop}`);
    const savedJobId = localStorage.getItem(`active_sync_job_id_${targetShop}`);
    const savedStartTime = localStorage.getItem(`active_sync_start_time_${targetShop}`);
    const savedBatch = localStorage.getItem(`active_sync_batch_${targetShop}`);
    const savedTotal = localStorage.getItem(`active_sync_total_${targetShop}`);

    try {
      let jobToRestore: any = null;

      // 1. Try checking the savedJobId if present
      if (savedJobId && savedJobId !== 'pending') {
        try {
          const res = await fetchJob(savedJobId);
          if (shopIdRef.current !== targetShop || activeJobRef.current !== null) return;
          if (res?.job) {
            jobToRestore = res.job;
          }
        } catch (_) {}
      }

      // 2. Query server for any currently running job for this shop
      if (!jobToRestore || jobToRestore.status !== 'RUNNING') {
        try {
          const historyRes = await fetchHistory(5, 0, 'RUNNING');
          if (shopIdRef.current !== targetShop || activeJobRef.current !== null) return;
          if (historyRes.jobs && historyRes.jobs.length > 0) {
            jobToRestore = historyRes.jobs[0];
          }
        } catch (_) {}
      }

      if (shopIdRef.current !== targetShop || activeJobRef.current !== null) return;

      // 3. If there is a running job, restore state immediately
      if (jobToRestore && jobToRestore.status === 'RUNNING') {
        const jobId = jobToRestore.job_id;
        const total = jobToRestore.total_batches || (savedTotal ? parseInt(savedTotal, 10) : 10);
        const batch =
          typeof jobToRestore.committed_batch_index === 'number' && jobToRestore.committed_batch_index >= 0
            ? jobToRestore.committed_batch_index + 1
            : (savedBatch ? parseInt(savedBatch, 10) : 0);

        let updateId = savedUpdateId;
        if (!updateId) {
          const savedSynced = localStorage.getItem(`catalog_synced_updates_${targetShop}`);
          const parsedSynced = savedSynced ? JSON.parse(savedSynced) : [];
          const pending = DEFAULT_CATALOG_UPDATES.find((u) => !parsedSynced.includes(u.id));
          updateId = pending ? pending.id : DEFAULT_CATALOG_UPDATES[0].id;
        }

        setActiveSyncingUpdateId(updateId);
        activeSyncingUpdateIdRef.current = updateId;
        try {
          localStorage.setItem(`active_sync_update_id_${targetShop}`, updateId);
          localStorage.setItem(`active_sync_job_id_${targetShop}`, jobId);
        } catch (_) {}

        setActiveJob({ job_id: jobId, status: 'RUNNING' });
        setCurrentBatch(batch);
        setTotalBatches(total);

        if (jobToRestore.summary) {
          setMetrics({
            added: jobToRestore.summary.added || 0,
            updated: jobToRestore.summary.updated || 0,
            withdrawn: jobToRestore.summary.withdrawn || 0,
            skipped: jobToRestore.summary.skipped || 0,
            dead_letter: jobToRestore.summary.dead_letter || 0,
          });
        }

        const startTs = savedStartTime
          ? parseInt(savedStartTime, 10)
          : (jobToRestore.created_at ? new Date(jobToRestore.created_at).getTime() : Date.now());
        jobStartTimeRef.current = startTs;
        startRuntimeTimer(startTs);

        setSmartStatusText(`Sync job ${jobId} running. Streaming batches...`);
        setSmartStatusType('running');
        return;
      }

      // 4. If the saved job has already COMPLETED while reloaded
      if (jobToRestore && jobToRestore.status === 'COMPLETED' && savedUpdateId) {
        markUpdateCompleted(savedUpdateId);
      } else if (
        (jobToRestore && (jobToRestore.status === 'INTERRUPTED' || jobToRestore.status === 'FAILED')) ||
        (!jobToRestore && savedJobId)
      ) {
        if (jobToRestore?.status === 'INTERRUPTED' && savedUpdateId) {
          setInterruptedUpdateId(savedUpdateId);
          try {
            localStorage.setItem(`interrupted_sync_update_id_${targetShop}`, savedUpdateId);
          } catch (_) {}
        }
        // Clean up stale active keys if the job is no longer running
        try {
          localStorage.removeItem(`active_sync_update_id_${targetShop}`);
          localStorage.removeItem(`active_sync_job_id_${targetShop}`);
          localStorage.removeItem(`active_sync_start_time_${targetShop}`);
          localStorage.removeItem(`active_sync_batch_${targetShop}`);
          localStorage.removeItem(`active_sync_total_${targetShop}`);
        } catch (_) {}
        setActiveSyncingUpdateId(null);
        activeSyncingUpdateIdRef.current = null;
      }
    } catch (err) {
      console.error('Failed to restore active sync:', err);
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
      // Prepare time series: if a specific job or single run is selected, fetch batch checkpoints for rich timeline
      let timeSeriesData: TimeSeriesPoint[] = [];

      if (selectedJobId !== 'all') {
        try {
          const jobRes = await fetchJob(selectedJobId);
          if (jobRes?.checkpoints && jobRes.checkpoints.length > 0) {
            timeSeriesData = jobRes.checkpoints.map((cp: any) => ({
              timestamp: cp.committed_at || new Date().toISOString(),
              batch_index: cp.batch_index,
              added: cp.summary?.added || 0,
              updated: cp.summary?.updated || 0,
              withdrawn: cp.summary?.withdrawn || 0,
              skipped: cp.summary?.skipped || 0,
              dead_letter: cp.summary?.dead_letter || 0,
              processing_speed: 0,
            }));
          }
        } catch (_) {}
      } else if (res.sync_jobs && res.sync_jobs.length === 1) {
        try {
          const jobRes = await fetchJob(res.sync_jobs[0].job_id);
          if (jobRes?.checkpoints && jobRes.checkpoints.length > 0) {
            timeSeriesData = jobRes.checkpoints.map((cp: any) => ({
              timestamp: cp.committed_at || new Date().toISOString(),
              batch_index: cp.batch_index,
              added: cp.summary?.added || 0,
              updated: cp.summary?.updated || 0,
              withdrawn: cp.summary?.withdrawn || 0,
              skipped: cp.summary?.skipped || 0,
              dead_letter: cp.summary?.dead_letter || 0,
              processing_speed: 0,
            }));
          }
        } catch (_) {}
      }

      if (timeSeriesData.length === 0) {
        timeSeriesData = (res.sync_jobs || []).slice().reverse().map((j: any, idx: number) => ({
          timestamp: j.completed_at || j.created_at || new Date().toISOString(),
          batch_index: idx,
          added: j.summary?.added || 0,
          updated: j.summary?.updated || 0,
          withdrawn: j.summary?.withdrawn || 0,
          skipped: j.summary?.skipped || 0,
          dead_letter: j.summary?.dead_letter || 0,
          processing_speed: 0,
        }));
      }

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
        time_series: timeSeriesData,
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
    if (newShopId === shopId) return;
    setActiveShopId(newShopId);
    shopIdRef.current = newShopId;
    setShopId(newShopId);
    toast.success(`Switched to shop: ${newShopId}`);
  };

  const handleSyncUpdate = async (update: CatalogUpdate) => {
    if (syncedUpdateIds.includes(update.id)) {
      toast.info(`${update.title} has already been synced.`);
      return;
    }
    if (activeJob && activeJob.status === 'RUNNING') {
      toast.warning('A sync job is currently running.');
      return;
    }

    const currentShop = shopIdRef.current;
    activeSyncingUpdateIdRef.current = update.id;
    setActiveSyncingUpdateId(update.id);

    if (update.isInterrupted) {
      autoHaltBatchRef.current = 5;
    } else {
      autoHaltBatchRef.current = null;
    }

    const payload = {
      mode: 'generator',
      seed: update.seed,
      total_batches: update.batches,
      batch_size: update.batchSize,
      update_ratio: update.updateRatio / 100,
      duplicate_ratio: update.dupRatio / 100,
      out_of_order_ratio: update.oooRatio / 100,
      withdrawn_ratio: update.withdrawnRatio / 100,
      invalid_ratio: update.invalidRatio / 100,
      base_time: update.baseTime,
      description: `${update.title} — ${update.description}`,
    };

    const now = Date.now();
    jobStartTimeRef.current = now;
    startRuntimeTimer(now);
    setCurrentBatch(0);
    setTotalBatches(update.batches || 10);
    setSmartStatusText(`Starting sync: ${update.title}...`);
    setSmartStatusType('running');
    setActiveJob({
      job_id: 'pending',
      status: 'RUNNING',
    });
    try {
      localStorage.setItem(`active_sync_update_id_${currentShop}`, update.id);
      localStorage.setItem(`active_sync_start_time_${currentShop}`, String(now));
      localStorage.setItem(`active_sync_total_${currentShop}`, String(update.batches || 10));
      localStorage.setItem(`active_sync_batch_${currentShop}`, '0');
    } catch (_) {}

    try {
      let res: { job_id: string; status: string };
      try {
        res = await startJob(payload);
      } catch (firstErr: any) {
        // Self-heal if a zombie job was left in RUNNING state on the server
        if (firstErr?.message && firstErr.message.toLowerCase().includes('already running')) {
          const runningList = await fetchHistory(5, 0, 'RUNNING');
          for (const rj of runningList.jobs || []) {
            try {
              await cancelJob(rj.job_id);
            } catch (_) {}
          }
          res = await startJob(payload);
        } else {
          throw firstErr;
        }
      }

      if (shopIdRef.current !== currentShop) return;
      jobToUpdateIdRef.current.set(res.job_id, update.id);
      if (!terminalJobIdsRef.current.has(res.job_id)) {
        setActiveJob({
          job_id: res.job_id,
          status: 'RUNNING',
        });
      } else {
        markUpdateCompleted(update.id);
      }
      try {
        localStorage.setItem(`active_sync_job_id_${currentShop}`, res.job_id);
      } catch (_) {}
      toast.success(`Sync started: ${update.title} (${res.job_id})`);

      try {
        const jobRes = await fetchJob(res.job_id);
        if (shopIdRef.current !== currentShop) return;
        const job = jobRes?.job;
        if (!job) return;

        if (job.summary) {
          setMetrics({
            added: job.summary.added || 0,
            updated: job.summary.updated || 0,
            withdrawn: job.summary.withdrawn || 0,
            skipped: job.summary.skipped || 0,
            dead_letter: job.summary.dead_letter || 0,
          });
        }

        if (
          job.status === 'RUNNING' &&
          autoHaltBatchRef.current !== null &&
          typeof job.committed_batch_index === 'number' &&
          job.committed_batch_index + 1 >= autoHaltBatchRef.current
        ) {
          autoHaltBatchRef.current = null;
          await cancelJob(res.job_id).catch(() => {});
          const afterCancel = await fetchJob(res.job_id).catch(() => null);
          if (afterCancel?.job) {
            Object.assign(job, afterCancel.job);
          } else {
            job.status = 'INTERRUPTED';
          }
        }

        if (job.status === 'COMPLETED') {
          terminalJobIdsRef.current.add(res.job_id);
          autoHaltBatchRef.current = null;
          markUpdateCompleted(update.id);
          setActiveJob(null);
          stopRuntimeTimer();
          setEta('Done');
          setCurrentBatch(job.total_batches || update.batches || 10);
          setSmartStatusText(`Sync job ${res.job_id} successfully completed. Catalog fully updated!`);
          setSmartStatusType('success');
          loadAllData();
          loadAnalytics();
        } else if (job.status === 'INTERRUPTED' || job.status === 'FAILED') {
          terminalJobIdsRef.current.add(res.job_id);
          autoHaltBatchRef.current = null;
          setInterruptedUpdateId(update.id);
          try {
            localStorage.setItem(`interrupted_sync_update_id_${currentShop}`, update.id);
          } catch (_) {}
          setActiveJob(null);
          stopRuntimeTimer();
          setSmartStatusText(`Job ${res.job_id} was interrupted. Checkpoint preserved for resumption.`);
          setSmartStatusType('warning');
          loadInterruptedJob();
          loadAllData();
          loadAnalytics();
        }
      } catch (_) {}
    } catch (err: any) {
      if (shopIdRef.current === currentShop) {
        autoHaltBatchRef.current = null;
        activeSyncingUpdateIdRef.current = null;
        setActiveSyncingUpdateId(null);
        setActiveJob(null);
        stopRuntimeTimer();
        setSmartStatusText(`Failed to start sync: ${err.message || 'Unknown error'}`);
        setSmartStatusType('warning');
      }
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

  const handleResumeInterrupted = async (jobId: string) => {
    const currentShop = shopIdRef.current;
    const restoredUpdateId =
      interruptedUpdateId ||
      activeSyncingUpdateIdRef.current ||
      localStorage.getItem(`interrupted_sync_update_id_${currentShop}`) ||
      'update-5-recovery';

    autoHaltBatchRef.current = null;
    terminalJobIdsRef.current.delete(jobId);
    activeSyncingUpdateIdRef.current = restoredUpdateId;
    setActiveSyncingUpdateId(restoredUpdateId);
    try {
      localStorage.setItem(`active_sync_update_id_${currentShop}`, restoredUpdateId);
      localStorage.setItem(`active_sync_job_id_${currentShop}`, jobId);
    } catch (_) {}

    const matchingUpdate = catalogUpdates.find((u) => u.id === restoredUpdateId);
    const resumePayload = matchingUpdate
      ? {
          seed: matchingUpdate.seed,
          total_batches: matchingUpdate.batches,
          batch_size: matchingUpdate.batchSize,
          update_ratio: matchingUpdate.updateRatio / 100,
          duplicate_ratio: matchingUpdate.dupRatio / 100,
          out_of_order_ratio: matchingUpdate.oooRatio / 100,
          withdrawn_ratio: matchingUpdate.withdrawnRatio / 100,
          invalid_ratio: matchingUpdate.invalidRatio / 100,
          base_time: matchingUpdate.baseTime,
        }
      : undefined;

    jobStartTimeRef.current = Date.now();
    startRuntimeTimer();
    setSmartStatusText(`Resuming interrupted sync ${jobId} from committed WAL checkpoint...`);
    setSmartStatusType('running');
    setActiveJob({
      job_id: jobId,
      status: 'RUNNING',
    });
    try {
      await resumeJob(jobId, resumePayload);
      toast.success(`Resumed job ${jobId}`);
      setInterruptedJob(null);
      setInterruptedUpdateId(null);
      try {
        localStorage.removeItem(`interrupted_sync_update_id_${currentShop}`);
      } catch (_) {}

      try {
        const jobRes = await fetchJob(jobId);
        if (shopIdRef.current !== currentShop) return;
        const job = jobRes?.job;
        if (job?.status === 'COMPLETED') {
          terminalJobIdsRef.current.add(jobId);
          markUpdateCompleted(restoredUpdateId);
          setActiveJob(null);
          stopRuntimeTimer();
          setEta('Done');
          setCurrentBatch(job.total_batches || totalBatchesRef.current || 10);
          if (job.summary) {
            setMetrics({
              added: job.summary.added || 0,
              updated: job.summary.updated || 0,
              withdrawn: job.summary.withdrawn || 0,
              skipped: job.summary.skipped || 0,
              dead_letter: job.summary.dead_letter || 0,
            });
          }
          setSmartStatusText(`Sync job ${jobId} successfully completed. Catalog fully updated!`);
          setSmartStatusType('success');
          loadAllData();
          loadAnalytics();
        }
      } catch (_) {}
    } catch (err: any) {
      setActiveJob(null);
      stopRuntimeTimer();
      setSmartStatusText(`Failed to resume job: ${err.message || 'Unknown error'}`);
      setSmartStatusType('warning');
      toast.error(err.message || 'Failed to resume job');
    }
  };

  const handleResetDb = async () => {
    setIsResettingDb(true);

    const currentShop = shopIdRef.current;

    // 1. Immediately abort local timers
    stopRuntimeTimer();

    // 2. Terminate all active syncing jobs on the server
    const currentActiveJob = activeJob;
    const savedJobId = localStorage.getItem(`active_sync_job_id_${currentShop}`);
    const jobsToCancel = new Set<string>();

    if (currentActiveJob?.job_id && currentActiveJob.job_id !== 'pending') {
      jobsToCancel.add(currentActiveJob.job_id);
    }
    if (savedJobId && savedJobId !== 'pending') {
      jobsToCancel.add(savedJobId);
    }

    try {
      const runningJobs = await fetchHistory(5, 0, 'RUNNING');
      if (runningJobs?.jobs) {
        for (const rj of runningJobs.jobs) {
          jobsToCancel.add(rj.job_id);
        }
      }
    } catch (_) {}

    // Send cancellation signal to all running jobs
    for (const jid of jobsToCancel) {
      try {
        await cancelJob(jid);
      } catch (_) {}
    }

    // 3. Clear all active sync keys and localStorage
    try {
      localStorage.removeItem(`active_sync_update_id_${currentShop}`);
      localStorage.removeItem(`interrupted_sync_update_id_${currentShop}`);
      localStorage.removeItem(`active_sync_job_id_${currentShop}`);
      localStorage.removeItem(`active_sync_start_time_${currentShop}`);
      localStorage.removeItem(`active_sync_batch_${currentShop}`);
      localStorage.removeItem(`active_sync_total_${currentShop}`);
      localStorage.removeItem(`catalog_synced_updates_${currentShop}`);
    } catch (_) {}

    setActiveJob(null);
    setInterruptedJob(null);
    setInterruptedUpdateId(null);
    setActiveSyncingUpdateId(null);
    activeSyncingUpdateIdRef.current = null;
    setSyncedUpdateIds([]);
    setVersions([]);
    setVersionTotal(0);
    setCatalog({ products: [], total: 0 });
    setDlq({ issues: [], total: 0 });
    setHistory({ jobs: [], total: 0 });
    setCurrentBatch(0);
    setTotalBatches(10);
    setThroughput(0);
    setEta('—');
    setElapsedTime('00:00');
    setMetrics({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
    setDeltas({ added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });

    // 4. Perform database reset
    try {
      await resetDatabase();
      toast.success('Database reset and all active sync processes stopped');
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
    <div className="min-h-screen bg-slate-50 flex font-sans text-slate-900 transition-colors">
      {/* Toast notifications */}
      <Toaster position="top-right" richColors />

      {/* App Sidebar (shadcn-admin architecture) */}
      <AppSidebar
        activeTab={activeTab}
        onSelectTab={navigateToTab}
        isCollapsed={isSidebarCollapsed}
        onToggleCollapse={() => setIsSidebarCollapsed(!isSidebarCollapsed)}
        isMobileOpen={isMobileSidebarOpen}
        onCloseMobile={() => setIsMobileSidebarOpen(false)}
        catalogCount={catalog.total}
        quarantinedCount={dlq.total}
        historyCount={history.total}
        versionsCount={versionTotal}
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
          activeShopId={shopId}
          onOpenMobileSidebar={() => setIsMobileSidebarOpen(true)}
          onResetDatabase={handleResetDb}
          activeJobId={activeJob ? activeJob.job_id : null}
          isRunning={Boolean(activeJob)}
          isRollingBack={isRollingBack}
          isResettingDb={isResettingDb}
          sseConnected={sseConnected}
          currentBatch={currentBatch}
          totalBatches={totalBatches}
          onNavigateToOverview={() => navigateToTab('overview')}
        />

        {/* Content Body */}
        <main className="flex-1 p-4 sm:p-6 lg:p-8 space-y-6 max-w-[1600px] w-full mx-auto">
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
                interruptedJob={interruptedJob}
                interruptedUpdateId={interruptedUpdateId}
                onCancelSync={handleCancelSync}
                onResumeSync={
                  interruptedJob
                    ? () => handleResumeInterrupted(interruptedJob.job_id)
                    : undefined
                }
                updates={catalogUpdates}
                syncedUpdateIds={syncedUpdateIds}
                activeSyncingUpdateId={activeSyncingUpdateId}
                activeRevertingUpdateId={activeRevertingUpdateId}
                onSyncUpdate={handleSyncUpdate}
                onRevertUpdate={handleRevertUpdate}
                onInspectUpdate={handleInspectUpdate}
                isRollingBack={isRollingBack}
                analyticsRange={analyticsRange}
                analyticsFrom={analyticsFrom}
                analyticsTo={analyticsTo}
                selectedJobId={selectedJobId}
                analyticsData={analyticsData}
                metrics={metrics}
                deltas={deltas}
                quarantinedTotal={dlq.total}
                versionsTotal={versionTotal}
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

            {/* 4. UNIFIED SYNC HISTORY & CATALOG VERSIONS VIEW */}
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
                onPageChange={(p) => {
                  setHistoryPage(p);
                  loadVersions(p);
                }}
                onResumeJob={handleResumeInterrupted}
                isSyncing={isSyncing}
                versions={versions}
                totalVersions={versionTotal}
                isLoadingVersions={isLoadingVersions}
                onRefresh={() => {
                  loadHistory();
                  loadVersions(historyPage);
                }}
                onRollback={handleRollback}
                onFetchDiff={handleFetchDiff}
                isRollingBack={isRollingBack}
                currentBatch={currentBatch}
                totalBatches={totalBatches}
                throughput={throughput}
                eta={eta}
                elapsedTime={elapsedTime}
              />
            )}
          </div>
        </main>

        {/* Footer */}
        <footer className="border-t border-slate-200/80 bg-white py-4 text-center text-xs text-slate-500 transition-colors">
          Supplier Catalog Engine
        </footer>
      </div>

      {/* Modals */}
      <RemediationModal
        issue={activeRemediationIssue}
        onOpenChange={(op) => !op && setActiveRemediationIssue(null)}
        onSubmitRemediation={handleRemediateProduct}
      />

      <MetricDrilldownModal
        category={drilldownCategory}
        metricCount={drilldownCategory ? metrics[drilldownCategory] : 0}
        onOpenChange={(op) => !op && setDrilldownCategory(null)}
        catalogProducts={catalog.products}
        dlqIssues={dlq.issues}
        onRemediateIssue={(iss) => setActiveRemediationIssue(iss)}
        onNavigateToCatalog={(statusFilter) => {
          setCatalogStatus(statusFilter);
          setCatalogPage(1);
          navigateToTab('catalog');
        }}
        onNavigateToTab={(tab) => navigateToTab(tab as NavItemKey)}
      />

      <ShopModal
        open={isShopModalOpen}
        onOpenChange={setIsShopModalOpen}
        currentShopId={shopId}
        onSelectShop={handleSelectShop}
      />

      <VersionDiffModal
        isOpen={isUpdateDiffModalOpen}
        onOpenChange={setIsUpdateDiffModalOpen}
        version={updateDiffVersion}
        changes={updateDiffChanges}
        isLoading={isLoadingUpdateDiff}
        previewUpdate={inspectedUpdate}
        isUpdateSynced={Boolean(inspectedUpdate && syncedUpdateIds.includes(inspectedUpdate.id))}
        onSyncUpdateClick={(u) => handleSyncUpdate(u)}
      />
    </div>
  );
};

export const App: React.FC = () => {
  return <AppContent />;
};

export default App;
