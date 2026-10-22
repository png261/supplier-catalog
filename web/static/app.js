/**
 * Coderpush Catalog Sync Engine — High-Reliability Operations Dashboard
 * Pure vanilla ES6+ JavaScript, SVG charts & Canvas. Zero external runtime dependencies.
 */

(function () {
  'use strict';

  // --- Central Application State ---
  const state = {
    activeJob: null,
    eventSource: null,
    sseConnected: false,
    sseReconnectTimer: null,
    lastPingTimestamp: Date.now(),
    pingWatchdogTimer: null,

    // Live Metrics (5 Core Invariants)
    metrics: { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 },
    lastDeltas: { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 },

    // Telemetry
    jobStartTime: null,
    runtimeTimer: null,
    lastCommittedBatch: -1,
    totalBatches: 0,
    recordsProcessed: 0,

    // Explorers
    dlq: { items: [], total: 0, limit: 10, offset: 0, filterReason: '' },
    catalog: { items: [], total: 0, limit: 10, offset: 0, search: '', status: '' },
    history: { jobs: [] },

    // Metric Drill-Down Modal
    metricModal: {
      type: null,
      search: '',
      limit: 10,
      offset: 0,
      total: 0,
      searchTimer: null,
    },

    // Analytics Filtering
    analytics: {
      timeRange: 'today',
      from: null,
      to: null,
      selectedJobId: '',
      syncJobs: [],
    },

    // Active Staged Scenario
    stagedScenario: {
      key: 'default',
      name: 'Standard Ingestion Profile',
      desc: 'Balanced workload for regular operations (60% new items, 20% price updates, 5% duplicates, 10% out-of-order, 5% withdrawn, 5% invalid records).',
      batches: 10,
      size: 100,
      volume: 1000,
      quality: '5% anomalies (DLQ test)',
      status: 'Ready to Sync',
    },

    // Active Remediation Issue
    currentRemedyIssue: null,
  };

  // --- Workload Presets ---
  const PRESETS = {
    default: { seed: 42, batches: 10, size: 100, update: 20, dup: 5, ooo: 10, with: 5, inv: 5, name: 'Standard Profile' },
    clean:   { seed: 101, batches: 10, size: 100, update: 20, dup: 0, ooo: 0, with: 0, inv: 0, name: 'Clean Ingestion' },
    stress:  { seed: 999, batches: 15, size: 150, update: 15, dup: 10, ooo: 15, with: 10, inv: 15, name: 'Fault Tolerance' },
    large:   { seed: 777, batches: 50, size: 200, update: 20, dup: 5, ooo: 10, with: 5, inv: 5, name: 'High Volume' },
  };

  // --- DOM Element References ---
  const el = {
    activeJobBadge: document.getElementById('active-job-badge'),
    activeJobText: document.getElementById('active-job-text'),
    sseStatus: document.getElementById('sse-status'),
    sseStatusText: document.getElementById('sse-status-text'),

    // Demo Simulation Modal & Controls
    btnOpenDemo: document.getElementById('btn-open-demo'),
    demoModal: document.getElementById('demo-modal'),
    btnCloseDemoModal: document.getElementById('btn-close-demo-modal'),
    presetCards: document.querySelectorAll('.preset-card'),
    btnStartJob: document.getElementById('btn-start-job'),
    btnToggleAdvanced: document.getElementById('btn-toggle-advanced'),
    advancedToggleLabel: document.getElementById('advanced-toggle-label'),
    advancedSettingsPanel: document.getElementById('advanced-settings-panel'),
    btnResetDb: document.getElementById('btn-reset-db'),
    btnCancelJob: document.getElementById('btn-cancel-job'),
    btnApplyScenario: document.getElementById('btn-apply-scenario'),

    // Staged Scenario & Sync on Main Page
    stagedScenarioTitle: document.getElementById('staged-scenario-title'),
    stagedScenarioVolumeTag: document.getElementById('staged-scenario-volume-tag'),
    stagedScenarioDescription: document.getElementById('staged-scenario-description'),
    stagedStatVolume: document.getElementById('staged-stat-volume'),
    stagedStatBatches: document.getElementById('staged-stat-batches'),
    stagedStatQuality: document.getElementById('staged-stat-quality'),
    stagedStatStatus: document.getElementById('staged-stat-status'),
    btnReopenScenarios: document.getElementById('btn-reopen-scenarios'),
    btnStartSyncMain: document.getElementById('btn-start-sync-main'),
    mainSyncSpinner: document.getElementById('main-sync-spinner'),
    mainSyncText: document.getElementById('main-sync-text'),
    btnCancelSyncMain: document.getElementById('btn-cancel-sync-main'),

    // Control Form (Advanced)
    generatorForm: document.getElementById('generator-form'),
    inputSeed: document.getElementById('input-seed'),
    btnRandomSeed: document.getElementById('btn-random-seed'),
    inputBatches: document.getElementById('input-batches'),
    inputBatchSize: document.getElementById('input-batch-size'),
    inputUpdateRatio: document.getElementById('input-update-ratio'),
    inputDupRatio: document.getElementById('input-dup-ratio'),
    inputOooRatio: document.getElementById('input-ooo-ratio'),
    inputWithdrawnRatio: document.getElementById('input-withdrawn-ratio'),
    inputInvalidRatio: document.getElementById('input-invalid-ratio'),
    valUpdateRatio: document.getElementById('val-update-ratio'),
    valDupRatio: document.getElementById('val-dup-ratio'),
    valOooRatio: document.getElementById('val-ooo-ratio'),
    valWithdrawnRatio: document.getElementById('val-withdrawn-ratio'),
    valInvalidRatio: document.getElementById('val-invalid-ratio'),
    ratioValidator: document.getElementById('ratio-validator'),
    totalRatioVal: document.getElementById('total-ratio-val'),
    btnResetForm: document.getElementById('btn-reset-form'),

    // Resume Tab
    selectInterruptedJob: document.getElementById('select-interrupted-job'),
    btnRefreshInterrupted: document.getElementById('btn-refresh-interrupted'),
    btnResumeJob: document.getElementById('btn-resume-job'),
    resumeJobDetails: document.getElementById('resume-job-details'),

    // Manual Tab
    manualJsonInput: document.getElementById('manual-json-input'),
    btnLoadSampleJson: document.getElementById('btn-load-sample-json'),
    btnClearJson: document.getElementById('btn-clear-json'),
    btnSubmitManual: document.getElementById('btn-submit-manual'),
    jsonParseStatus: document.getElementById('json-parse-status'),

    // Analytics Filter Controls
    analyticsFilterBadge: document.getElementById('analytics-filter-badge'),
    analyticsFilterSubtitle: document.getElementById('analytics-filter-subtitle'),
    rangePills: document.querySelectorAll('#analytics-range-pills .btn-pill'),
    selectSyncRun: document.getElementById('select-sync-run'),
    btnRefreshAnalytics: document.getElementById('btn-refresh-analytics'),
    customRangeRow: document.getElementById('custom-range-row'),
    inputRangeFrom: document.getElementById('input-range-from'),
    inputRangeTo: document.getElementById('input-range-to'),
    btnApplyCustomRange: document.getElementById('btn-apply-custom-range'),

    // Metric Values
    metricAdded: document.getElementById('metric-added'),
    metricUpdated: document.getElementById('metric-updated'),
    metricWithdrawn: document.getElementById('metric-withdrawn'),
    metricSkipped: document.getElementById('metric-skipped'),
    metricDeadLetter: document.getElementById('metric-deadletter'),
    deltaAdded: document.getElementById('delta-added'),
    deltaUpdated: document.getElementById('delta-updated'),
    deltaWithdrawn: document.getElementById('delta-withdrawn'),
    deltaSkipped: document.getElementById('delta-skipped'),
    deltaDeadLetter: document.getElementById('delta-deadletter'),

    // Progress & Smart Banner
    progressBarFill: document.getElementById('progress-bar-fill'),
    batchProgressText: document.getElementById('batch-progress-text'),
    telemetrySpeed: document.getElementById('telemetry-speed'),
    telemetryEta: document.getElementById('telemetry-eta'),
    telemetryRuntime: document.getElementById('telemetry-runtime'),
    smartStatusText: document.getElementById('smart-status-text'),
    pulseIndicator: document.querySelector('.pulse-indicator'),

    // Charts: Donut
    donutSvg: document.getElementById('donut-svg'),
    donutTotalCount: document.getElementById('donut-total-count'),
    legValAdded: document.getElementById('leg-val-added'),
    legValUpdated: document.getElementById('leg-val-updated'),
    legValWithdrawn: document.getElementById('leg-val-withdrawn'),
    legValSkipped: document.getElementById('leg-val-skipped'),
    legValDeadletter: document.getElementById('leg-val-deadletter'),

    // Charts: Health Gauge
    gaugeFill: document.getElementById('gauge-fill'),
    healthPctValue: document.getElementById('health-pct-value'),
    healthStatusBadge: document.getElementById('health-status-badge'),
    healthStatusTitle: document.getElementById('health-status-title'),
    healthStatusDesc: document.getElementById('health-status-desc'),
    statElapsedTime: document.getElementById('stat-elapsed-time'),
    statAvgSpeed: document.getElementById('stat-avg-speed'),

    // Explorers
    countDlqBadge: document.getElementById('count-dlq-badge'),
    countCatalogBadge: document.getElementById('count-catalog-badge'),
    filterDlqReason: document.getElementById('filter-dlq-reason'),
    btnRefreshDlq: document.getElementById('btn-refresh-dlq'),
    dlqTableBody: document.getElementById('dlq-table-body'),
    dlqPaginationInfo: document.getElementById('dlq-pagination-info'),
    dlqPageNum: document.getElementById('dlq-page-num'),
    btnDlqPrev: document.getElementById('btn-dlq-prev'),
    btnDlqNext: document.getElementById('btn-dlq-next'),

    filterProductSearch: document.getElementById('filter-product-search'),
    filterProductStatus: document.getElementById('filter-product-status'),
    btnRefreshCatalog: document.getElementById('btn-refresh-catalog'),
    catalogTableBody: document.getElementById('catalog-table-body'),
    catalogPaginationInfo: document.getElementById('catalog-pagination-info'),
    catalogPageNum: document.getElementById('catalog-page-num'),
    btnCatalogPrev: document.getElementById('btn-catalog-prev'),
    btnCatalogNext: document.getElementById('btn-catalog-next'),

    btnRefreshHistory: document.getElementById('btn-refresh-history'),
    historyTableBody: document.getElementById('history-table-body'),

    // Remediation Modal Elements
    payloadModal: document.getElementById('payload-modal'),
    modalIssueId: document.getElementById('modal-issue-id'),
    modalIssueBatch: document.getElementById('modal-issue-batch'),
    modalDefectReason: document.getElementById('modal-defect-reason'),
    remedySku: document.getElementById('remedy-sku'),
    remedyName: document.getElementById('remedy-name'),
    remedyPrice: document.getElementById('remedy-price'),
    remedyStatus: document.getElementById('remedy-status'),
    remedyTimestamp: document.getElementById('remedy-timestamp'),
    btnToggleRawPayload: document.getElementById('btn-toggle-raw-payload'),
    modalPayloadCode: document.getElementById('modal-payload-code'),
    btnCloseModal: document.getElementById('btn-close-modal'),
    btnDismissModal: document.getElementById('btn-dismiss-modal'),
    btnResolveAndImport: document.getElementById('btn-resolve-and-import'),
    resolveSpinner: document.getElementById('resolve-spinner'),
    btnResolveText: document.getElementById('btn-resolve-text'),
    formRemediateProduct: document.getElementById('form-remediate-product'),

    // Metric Drill-Down Modal
    metricCards: document.querySelectorAll('.metric-card[data-metric]'),
    metricDetailModal: document.getElementById('metric-detail-modal'),
    metricModalTitle: document.getElementById('metric-modal-title'),
    metricModalSubtitle: document.getElementById('metric-modal-subtitle'),
    metricModalSearch: document.getElementById('metric-modal-search'),
    metricModalCountBadge: document.getElementById('metric-modal-count-badge'),
    metricModalThead: document.getElementById('metric-modal-thead'),
    metricModalTbody: document.getElementById('metric-modal-tbody'),
    metricModalPagination: document.getElementById('metric-modal-pagination'),
    metricModalPaginationInfo: document.getElementById('metric-modal-pagination-info'),
    metricModalPageNum: document.getElementById('metric-modal-page-num'),
    btnMetricModalPrev: document.getElementById('btn-metric-modal-prev'),
    btnMetricModalNext: document.getElementById('btn-metric-modal-next'),
    btnCloseMetricModal: document.getElementById('btn-close-metric-modal'),
    btnDismissMetricModal: document.getElementById('btn-dismiss-metric-modal'),

    // Toasts
    toastContainer: document.getElementById('toast-container'),
  };

  // --- Tab Navigation ---
  document.querySelectorAll('.tab-group').forEach((group) => {
    group.addEventListener('click', (e) => {
      const btn = e.target.closest('.tab-btn');
      if (!btn) return;
      const targetTabId = btn.getAttribute('data-tab');
      const parentCard = group.closest('.card');

      group.querySelectorAll('.tab-btn').forEach((b) => b.classList.remove('active'));
      parentCard.querySelectorAll('.tab-content').forEach((c) => c.classList.remove('active'));

      btn.classList.add('active');
      const content = document.getElementById(targetTabId);
      if (content) content.classList.add('active');

      if (targetTabId === 'tab-dlq') loadDLQ();
      if (targetTabId === 'tab-catalog') loadCatalog();
      if (targetTabId === 'tab-history') loadHistory();
      if (targetTabId === 'tab-resume') loadInterruptedJobs();
    });
  });

  // --- Toast Notifications ---
  function showToast(message, type = 'info') {
    if (!el.toastContainer) return;
    const toast = document.createElement('div');
    toast.className = `toast toast-${type}`;
    toast.textContent = message;
    el.toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      toast.style.transition = 'all 200ms ease';
      setTimeout(() => toast.remove(), 200);
    }, 4000);
  }

  // --- Smart Status Banner ---
  function updateSmartStatus(message, statusType = 'normal') {
    if (el.smartStatusText) {
      el.smartStatusText.textContent = message;
    }
    if (el.pulseIndicator) {
      if (statusType === 'running') {
        el.pulseIndicator.style.backgroundColor = 'var(--color-running)';
        el.pulseIndicator.style.boxShadow = '0 0 8px var(--color-running)';
      } else if (statusType === 'success') {
        el.pulseIndicator.style.backgroundColor = 'var(--color-added)';
        el.pulseIndicator.style.boxShadow = '0 0 8px var(--color-added)';
      } else if (statusType === 'warning') {
        el.pulseIndicator.style.backgroundColor = 'var(--color-withdrawn)';
        el.pulseIndicator.style.boxShadow = '0 0 8px var(--color-withdrawn)';
      } else if (statusType === 'error') {
        el.pulseIndicator.style.backgroundColor = 'var(--color-deadletter)';
        el.pulseIndicator.style.boxShadow = '0 0 8px var(--color-deadletter)';
      } else {
        el.pulseIndicator.style.backgroundColor = 'var(--color-idle)';
        el.pulseIndicator.style.boxShadow = 'none';
      }
    }
  }

  // --- Chart 1: Donut Chart Rendering ---
  function renderDonutChart() {
    if (!el.donutSvg) return;

    const added = state.metrics.added || 0;
    const updated = state.metrics.updated || 0;
    const withdrawn = state.metrics.withdrawn || 0;
    const skipped = state.metrics.skipped || 0;
    const deadLetter = state.metrics.deadLetter || 0;
    const total = added + updated + withdrawn + skipped + deadLetter;

    if (el.donutTotalCount) {
      el.donutTotalCount.textContent = total.toLocaleString();
    }

    const calcPct = (cnt) => (total > 0 ? ((cnt / total) * 100).toFixed(1) : '0.0');

    if (el.legValAdded) el.legValAdded.textContent = `${added.toLocaleString()} (${calcPct(added)}%)`;
    if (el.legValUpdated) el.legValUpdated.textContent = `${updated.toLocaleString()} (${calcPct(updated)}%)`;
    if (el.legValWithdrawn) el.legValWithdrawn.textContent = `${withdrawn.toLocaleString()} (${calcPct(withdrawn)}%)`;
    if (el.legValSkipped) el.legValSkipped.textContent = `${skipped.toLocaleString()} (${calcPct(skipped)}%)`;
    if (el.legValDeadletter) el.legValDeadletter.textContent = `${deadLetter.toLocaleString()} (${calcPct(deadLetter)}%)`;

    // SVG Circle circumferences: r = 70 => C = 2 * PI * 70 ≈ 439.82
    const C = 439.82;
    el.donutSvg.innerHTML = '';

    if (total === 0) {
      const emptyCircle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
      emptyCircle.setAttribute('cx', '100');
      emptyCircle.setAttribute('cy', '100');
      emptyCircle.setAttribute('r', '70');
      emptyCircle.setAttribute('class', 'donut-slice donut-slice-empty');
      el.donutSvg.appendChild(emptyCircle);
      return;
    }

    const slices = [
      { key: 'added', count: added },
      { key: 'updated', count: updated },
      { key: 'withdrawn', count: withdrawn },
      { key: 'skipped', count: skipped },
      { key: 'deadletter', count: deadLetter },
    ];

    let accumulatedOffset = 0;
    slices.forEach((slice) => {
      if (slice.count <= 0) return;
      const sliceLength = (slice.count / total) * C;
      const circle = document.createElementNS('http://www.w3.org/2000/svg', 'circle');
      circle.setAttribute('cx', '100');
      circle.setAttribute('cy', '100');
      circle.setAttribute('r', '70');
      circle.setAttribute('class', `donut-slice donut-slice-${slice.key}`);
      circle.setAttribute('stroke-dasharray', `${sliceLength} ${C - sliceLength}`);
      circle.setAttribute('stroke-dashoffset', `${-accumulatedOffset}`);
      el.donutSvg.appendChild(circle);
      accumulatedOffset += sliceLength;
    });
  }

  // --- Chart 2: System Health Gauge Rendering ---
  function updateHealthGauge() {
    if (!el.gaugeFill) return;

    const added = state.metrics.added || 0;
    const updated = state.metrics.updated || 0;
    const withdrawn = state.metrics.withdrawn || 0;
    const skipped = state.metrics.skipped || 0;
    const deadLetter = state.metrics.deadLetter || 0;
    const total = added + updated + withdrawn + skipped + deadLetter;

    const successRate = total > 0 ? Math.max(0, ((total - deadLetter) / total) * 100) : 100;
    const C = 314.16; // 2 * PI * 50
    const offset = C * (1.0 - successRate / 100.0);

    el.gaugeFill.style.strokeDashoffset = offset.toFixed(2);
    if (el.healthPctValue) {
      el.healthPctValue.textContent = `${successRate.toFixed(1)}%`;
    }

    if (successRate >= 98.0) {
      el.gaugeFill.style.stroke = 'var(--color-added)';
      if (el.healthStatusBadge) {
        el.healthStatusBadge.className = 'health-status-badge badge-success';
      }
      if (el.healthStatusTitle) {
        el.healthStatusTitle.textContent = 'Optimal Integrity (100%)';
      }
      if (el.healthStatusDesc) {
        el.healthStatusDesc.textContent = 'All committed records strictly satisfy mathematical invariants with zero database corruption.';
      }
    } else if (successRate >= 90.0) {
      el.gaugeFill.style.stroke = 'var(--color-withdrawn)';
      if (el.healthStatusBadge) {
        el.healthStatusBadge.className = 'health-status-badge badge-warning';
      }
      if (el.healthStatusTitle) {
        el.healthStatusTitle.textContent = `Isolated Defects (${deadLetter.toLocaleString()} records)`;
      }
      if (el.healthStatusDesc) {
        el.healthStatusDesc.textContent = 'Non-compliant records (negative prices or missing SKUs) were safely quarantined. Core catalog remains clean.';
      }
    } else {
      el.gaugeFill.style.stroke = 'var(--color-deadletter)';
      if (el.healthStatusBadge) {
        el.healthStatusBadge.className = 'health-status-badge badge-danger';
      }
      if (el.healthStatusTitle) {
        el.healthStatusTitle.textContent = `High Defect Rate (${(100 - successRate).toFixed(1)}%)`;
      }
      if (el.healthStatusDesc) {
        el.healthStatusDesc.textContent = 'Abnormal supplier defect rate detected. Review individual items in Quarantined Records.';
      }
    }

    // Update Telemetry in Health Card
    if (state.jobStartTime && el.statElapsedTime && el.statAvgSpeed) {
      const elapsedSec = (Date.now() - state.jobStartTime) / 1000;
      if (elapsedSec > 0.5) {
        const mm = String(Math.floor(elapsedSec / 60)).padStart(2, '0');
        const ss = String(elapsedSec % 60).padStart(2, '0');
        el.statElapsedTime.textContent = `${mm}:${ss}`;
        const avg = Math.round(total / elapsedSec);
        el.statAvgSpeed.textContent = `${avg} rec/s`;
      }
    }
  }

  // --- Workload Presets & Scenario Management ---
  const SCENARIO_DETAILS = {
    default: {
      name: 'Standard Ingestion Profile',
      desc: 'Balanced workload for regular operations (60% new items, 20% price updates, 5% duplicates, 10% out-of-order, 5% withdrawn, 5% invalid records).',
      batches: 10,
      size: 100,
      volume: 1000,
      quality: '5% anomalies (DLQ test)',
    },
    clean: {
      name: 'Clean Ingestion (Zero Defects)',
      desc: '100% compliant supplier feed with zero schema errors or timestamp anomalies. Perfect baseline for error-free catalog additions and updates.',
      batches: 10,
      size: 100,
      volume: 1000,
      quality: '0% anomalies (100% compliant)',
    },
    stress: {
      name: 'Fault Tolerance & DLQ Stress',
      desc: 'High anomaly rate with negative prices, empty SKUs, and zero timestamps. Thoroughly tests dead-letter quarantine isolation without aborting batches.',
      batches: 15,
      size: 150,
      volume: 2250,
      quality: '15% anomalies (Heavy DLQ test)',
    },
    large: {
      name: 'High-Volume Stress Load',
      desc: 'High-throughput streaming feed with rapid interleaved batches to stress-test SQLite WAL concurrency, throughput, and memory stability.',
      batches: 50,
      size: 200,
      volume: 10000,
      quality: '5% anomalies (High throughput)',
    },
  };

  function updateStagedScenarioUI() {
    const s = state.stagedScenario;
    if (el.stagedScenarioTitle) el.stagedScenarioTitle.textContent = s.name;
    if (el.stagedScenarioVolumeTag) el.stagedScenarioVolumeTag.textContent = `${s.batches} Batches • ${s.volume.toLocaleString()} Records`;
    if (el.stagedScenarioDescription) el.stagedScenarioDescription.textContent = s.desc;
    if (el.stagedStatVolume) el.stagedStatVolume.textContent = `${s.volume.toLocaleString()} items`;
    if (el.stagedStatBatches) el.stagedStatBatches.textContent = `${s.batches} batches (${s.size} / batch)`;
    if (el.stagedStatQuality) el.stagedStatQuality.textContent = s.quality;
    if (el.stagedStatStatus) {
      el.stagedStatStatus.textContent = s.status;
      el.stagedStatStatus.className = s.status === 'Ready to Sync' ? 'status-ready' : (s.status.includes('Sync') ? 'status-syncing' : 'status-completed');
    }
  }

  function applyPreset(key) {
    const p = PRESETS[key];
    if (!p) return;
    if (el.inputSeed) el.inputSeed.value = p.seed;
    if (el.inputBatches) el.inputBatches.value = p.batches;
    if (el.inputBatchSize) el.inputBatchSize.value = p.size;
    if (el.inputUpdateRatio) el.inputUpdateRatio.value = p.update;
    if (el.inputDupRatio) el.inputDupRatio.value = p.dup;
    if (el.inputOooRatio) el.inputOooRatio.value = p.ooo;
    if (el.inputWithdrawnRatio) el.inputWithdrawnRatio.value = p.with;
    if (el.inputInvalidRatio) el.inputInvalidRatio.value = p.inv;
    updateRatios();

    const detail = SCENARIO_DETAILS[key] || {
      name: p.name,
      desc: 'Configured synthetic workload profile.',
      batches: p.batches,
      size: p.size,
      volume: p.batches * p.size,
      quality: `${p.inv}% anomalies`,
    };

    state.stagedScenario = {
      key: key,
      name: detail.name,
      desc: detail.desc,
      batches: detail.batches,
      size: detail.size,
      volume: detail.volume,
      quality: detail.quality,
      status: 'Ready to Sync',
    };
    updateStagedScenarioUI();
  }

  el.presetCards.forEach((card) => {
    card.addEventListener('click', () => {
      el.presetCards.forEach((c) => c.classList.remove('active'));
      card.classList.add('active');
      const presetKey = card.getAttribute('data-preset');
      applyPreset(presetKey);
    });
  });

  // --- Advanced Panel Toggle ---
  if (el.btnToggleAdvanced && el.advancedSettingsPanel) {
    el.btnToggleAdvanced.addEventListener('click', () => {
      const isHidden = el.advancedSettingsPanel.classList.toggle('hidden');
      if (el.advancedToggleLabel) {
        el.advancedToggleLabel.textContent = isHidden ? 'Advanced Settings' : 'Hide Advanced';
      }
    });
  }

  // --- Ratio Validator ---
  function updateRatios() {
    if (!el.inputUpdateRatio) return;
    const updateVal = parseInt(el.inputUpdateRatio.value, 10);
    const dupVal = parseInt(el.inputDupRatio.value, 10);
    const oooVal = parseInt(el.inputOooRatio.value, 10);
    const withVal = parseInt(el.inputWithdrawnRatio.value, 10);
    const invVal = parseInt(el.inputInvalidRatio.value, 10);

    if (el.valUpdateRatio) el.valUpdateRatio.textContent = `${updateVal}%`;
    if (el.valDupRatio) el.valDupRatio.textContent = `${dupVal}%`;
    if (el.valOooRatio) el.valOooRatio.textContent = `${oooVal}%`;
    if (el.valWithdrawnRatio) el.valWithdrawnRatio.textContent = `${withVal}%`;
    if (el.valInvalidRatio) el.valInvalidRatio.textContent = `${invVal}%`;

    const total = updateVal + dupVal + oooVal + withVal + invVal;
    if (el.totalRatioVal) el.totalRatioVal.textContent = `${total}%`;

    if (total > 100) {
      if (el.ratioValidator) {
        el.ratioValidator.className = 'ratio-validator invalid';
        el.totalRatioVal.textContent = `${total}% (Exceeds 100%)`;
      }
      if (el.btnStartJob) el.btnStartJob.disabled = true;
    } else {
      if (el.ratioValidator) el.ratioValidator.className = 'ratio-validator valid';
      if (!state.activeJob || state.activeJob.status !== 'RUNNING') {
        if (el.btnStartJob) el.btnStartJob.disabled = false;
      }
    }
  }

  [el.inputUpdateRatio, el.inputDupRatio, el.inputOooRatio, el.inputWithdrawnRatio, el.inputInvalidRatio].forEach((input) => {
    if (input) input.addEventListener('input', updateRatios);
  });

  if (el.btnRandomSeed) {
    el.btnRandomSeed.addEventListener('click', () => {
      if (el.inputSeed) el.inputSeed.value = Math.floor(Math.random() * 1000000);
    });
  }

  if (el.btnResetForm) {
    el.btnResetForm.addEventListener('click', () => {
      applyPreset('default');
    });
  }

  // --- Metric Cards Animation ---
  function updateMetricCards() {
    if (!el.metricAdded) return;
    el.metricAdded.textContent = state.metrics.added.toLocaleString();
    el.metricUpdated.textContent = state.metrics.updated.toLocaleString();
    el.metricWithdrawn.textContent = state.metrics.withdrawn.toLocaleString();
    el.metricSkipped.textContent = state.metrics.skipped.toLocaleString();
    el.metricDeadLetter.textContent = state.metrics.deadLetter.toLocaleString();

    el.deltaAdded.textContent = `+${state.lastDeltas.added || 0}`;
    el.deltaUpdated.textContent = `+${state.lastDeltas.updated || 0}`;
    el.deltaWithdrawn.textContent = `+${state.lastDeltas.withdrawn || 0}`;
    el.deltaSkipped.textContent = `+${state.lastDeltas.skipped || 0}`;
    el.deltaDeadLetter.textContent = `+${state.lastDeltas.dead_letter || 0}`;

    if (el.countDlqBadge) {
      el.countDlqBadge.textContent = state.metrics.deadLetter.toLocaleString();
    }

    renderDonutChart();
    updateHealthGauge();
  }

  // --- Analytics & Filter Engine ---
  async function loadAnalyticsMetrics() {
    let url = `/api/metrics?time_range=${encodeURIComponent(state.analytics.timeRange)}`;
    if (state.analytics.selectedJobId) {
      url += `&job_id=${encodeURIComponent(state.analytics.selectedJobId)}`;
    }
    if (state.analytics.from) {
      url += `&from=${encodeURIComponent(state.analytics.from)}`;
    }
    if (state.analytics.to) {
      url += `&to=${encodeURIComponent(state.analytics.to)}`;
    }

    try {
      const resp = await fetch(url);
      if (!resp.ok) throw new Error('HTTP ' + resp.status);
      const data = await resp.json();
      const m = data.metrics || {};
      const summary = m.summary || {};
      const jobs = data.sync_jobs || [];
      state.analytics.syncJobs = jobs;

      // Update state metrics with aggregated values
      state.metrics.added = summary.added || 0;
      state.metrics.updated = summary.updated || 0;
      state.metrics.withdrawn = summary.withdrawn || 0;
      state.metrics.skipped = summary.skipped || 0;
      state.metrics.deadLetter = summary.dead_letter || 0;

      updateMetricCards();

      // If no job is currently running, populate telemetry with aggregated performance
      if (!state.activeJob || state.activeJob.status !== 'RUNNING') {
        if (el.statElapsedTime) {
          const sec = Math.round(m.elapsed_seconds || 0);
          const mm = String(Math.floor(sec / 60)).padStart(2, '0');
          const ss = String(sec % 60).padStart(2, '0');
          el.statElapsedTime.textContent = `${mm}:${ss}`;
        }
        if (el.statAvgSpeed) {
          el.statAvgSpeed.textContent = `${Math.round(m.avg_throughput || 0)} rec/s`;
        }
      }

      // Update Subtitle & Badges
      if (state.analytics.selectedJobId) {
        const shortId = state.analytics.selectedJobId.slice(0, 8);
        if (el.analyticsFilterBadge) el.analyticsFilterBadge.textContent = `Sync Run #${shortId}`;
        if (el.analyticsFilterSubtitle) {
          el.analyticsFilterSubtitle.textContent = `Showing Sync Run: #${state.analytics.selectedJobId.slice(0, 16)}... (${(m.total_processed || 0).toLocaleString()} items • ${summary.dead_letter || 0} defects)`;
        }
      } else {
        const labels = {
          today: 'Today',
          yesterday: 'Yesterday',
          '7d': 'Last 7 Days',
          all: 'All Time',
          custom: 'Custom Range',
        };
        const curLabel = labels[state.analytics.timeRange] || 'Filtered';
        if (el.analyticsFilterBadge) el.analyticsFilterBadge.textContent = curLabel;
        if (el.analyticsFilterSubtitle) {
          el.analyticsFilterSubtitle.textContent = `Showing: ${curLabel} (${m.total_syncs || 0} sync runs, ${(m.total_processed || 0).toLocaleString()} total records processed)`;
        }
      }

      // Populate Sync Run Selector Dropdown
      if (el.selectSyncRun) {
        const currentSelected = state.analytics.selectedJobId;
        el.selectSyncRun.innerHTML = '';

        const optAll = document.createElement('option');
        optAll.value = 'all';
        optAll.textContent = `All Sync Runs in period (${m.total_syncs || 0} runs)`;
        el.selectSyncRun.appendChild(optAll);

        jobs.forEach((j) => {
          const opt = document.createElement('option');
          opt.value = j.job_id;
          const shortId = j.job_id.slice(0, 8);
          const timeStr = j.created_at ? new Date(j.created_at).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }) : '';
          const s = j.summary || {};
          const totalItems = (s.added || 0) + (s.updated || 0) + (s.withdrawn || 0) + (s.skipped || 0) + (s.dead_letter || 0);
          opt.textContent = `Run #${shortId} (${timeStr} • ${totalItems.toLocaleString()} items • ${j.status})`;
          if (j.job_id === currentSelected) {
            opt.selected = true;
          }
          el.selectSyncRun.appendChild(opt);
        });

        if (!currentSelected) {
          optAll.selected = true;
        }
      }
    } catch (err) {
      console.error('Failed to load analytics metrics', err);
    }
  }

  // Wire Range Pills
  if (el.rangePills) {
    el.rangePills.forEach((pill) => {
      pill.addEventListener('click', () => {
        el.rangePills.forEach((p) => p.classList.remove('active'));
        pill.classList.add('active');
        const range = pill.getAttribute('data-range');
        state.analytics.timeRange = range;
        state.analytics.selectedJobId = '';

        if (range === 'custom') {
          if (el.customRangeRow) el.customRangeRow.classList.remove('hidden');
        } else {
          if (el.customRangeRow) el.customRangeRow.classList.add('hidden');
          state.analytics.from = null;
          state.analytics.to = null;
          loadAnalyticsMetrics();
        }
      });
    });
  }

  // Wire Custom Range Apply
  if (el.btnApplyCustomRange) {
    el.btnApplyCustomRange.addEventListener('click', () => {
      const fromVal = el.inputRangeFrom ? el.inputRangeFrom.value : '';
      const toVal = el.inputRangeTo ? el.inputRangeTo.value : '';
      if (!fromVal && !toVal) {
        showToast('Please specify a date to filter', 'warning');
        return;
      }
      state.analytics.timeRange = 'custom';
      state.analytics.from = fromVal || null;
      state.analytics.to = toVal || null;
      state.analytics.selectedJobId = '';
      loadAnalyticsMetrics();
    });
  }

  // Wire Sync Run Selector
  if (el.selectSyncRun) {
    el.selectSyncRun.addEventListener('change', () => {
      const val = el.selectSyncRun.value;
      if (val === 'all') {
        state.analytics.selectedJobId = '';
      } else {
        state.analytics.selectedJobId = val;
      }
      loadAnalyticsMetrics();
    });
  }

  // Wire Refresh Button
  if (el.btnRefreshAnalytics) {
    el.btnRefreshAnalytics.addEventListener('click', () => {
      loadAnalyticsMetrics();
      showToast('Analytics metrics updated', 'info');
    });
  }

  // --- Runtime Timer ---
  function startRuntimeTimer() {
    state.jobStartTime = Date.now();
    clearInterval(state.runtimeTimer);
    state.runtimeTimer = setInterval(() => {
      if (!state.jobStartTime || !el.telemetryRuntime) return;
      const elapsedSec = Math.floor((Date.now() - state.jobStartTime) / 1000);
      const mm = String(Math.floor(elapsedSec / 60)).padStart(2, '0');
      const ss = String(elapsedSec % 60).padStart(2, '0');
      el.telemetryRuntime.textContent = `${mm}:${ss}`;
      if (el.statElapsedTime) el.statElapsedTime.textContent = `${mm}:${ss}`;
    }, 1000);
  }

  function stopRuntimeTimer() {
    clearInterval(state.runtimeTimer);
    state.runtimeTimer = null;
  }

  // --- SSE Event Stream Connection ---
  function initSSE() {
    if (state.eventSource) {
      state.eventSource.close();
    }

    if (el.sseStatus) {
      el.sseStatus.className = 'badge badge-sse sse-connecting';
      el.sseStatusText.textContent = 'Connecting...';
    }

    state.eventSource = new EventSource('/api/events');

    state.eventSource.onopen = () => {
      state.sseConnected = true;
      state.lastPingTimestamp = Date.now();
      if (el.sseStatus) {
        el.sseStatus.className = 'badge badge-sse sse-connected';
        el.sseStatusText.textContent = 'Live';
      }
    };

    state.eventSource.onerror = () => {
      state.sseConnected = false;
      if (el.sseStatus) {
        el.sseStatus.className = 'badge badge-sse sse-disconnected';
        el.sseStatusText.textContent = 'Reconnecting...';
      }
    };

    clearInterval(state.pingWatchdogTimer);
    state.pingWatchdogTimer = setInterval(() => {
      if (Date.now() - state.lastPingTimestamp > 35000 && state.sseConnected) {
        initSSE();
      }
    }, 5000);

    state.eventSource.addEventListener('job_started', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleJobStarted(data);
      } catch (err) {
        console.error('Invalid job_started payload', err);
      }
    });

    state.eventSource.addEventListener('progress', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleProgress(data);
      } catch (err) {
        console.error('Invalid progress payload', err);
      }
    });

    state.eventSource.addEventListener('dead_letter', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleDeadLetter(data);
      } catch (err) {
        console.error('Invalid dead_letter payload', err);
      }
    });

    state.eventSource.addEventListener('job_completed', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleJobCompleted(data);
      } catch (err) {
        console.error('Invalid job_completed payload', err);
      }
    });

    state.eventSource.addEventListener('job_interrupted', (e) => {
      state.lastPingTimestamp = Date.now();
      try {
        const data = JSON.parse(e.data);
        handleJobInterrupted(data);
      } catch (err) {
        console.error('Invalid job_interrupted payload', err);
      }
    });
  }

  // --- SSE Event Handlers ---
  function handleJobStarted(job) {
    state.activeJob = job;
    state.totalBatches = job.total_batches || 10;
    state.recordsProcessed = 0;
    state.metrics = { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 };
    updateMetricCards();
    startRuntimeTimer();

    state.stagedScenario.status = 'Syncing...';
    updateStagedScenarioUI();

    if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = true;
    if (el.mainSyncSpinner) el.mainSyncSpinner.classList.remove('hidden');
    if (el.mainSyncText) el.mainSyncText.textContent = 'Syncing...';
    if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.remove('hidden');

    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-running';
      el.activeJobText.textContent = `Running (${job.job_id.substring(0, 12)}...)`;
    }
    if (el.btnStartJob) el.btnStartJob.disabled = true;
    if (el.btnResumeJob) el.btnResumeJob.disabled = true;
    if (el.btnCancelJob) el.btnCancelJob.classList.remove('hidden');

    if (el.progressBarFill) el.progressBarFill.style.width = '0%';
    if (el.batchProgressText) el.batchProgressText.textContent = `0 / ${state.totalBatches} Batches (0.0%)`;

    updateSmartStatus(`Started synchronization job #${job.job_id.substring(0, 8)}. Ingesting supplier batches...`, 'running');
    showToast(`Started synchronization (${state.totalBatches} batches)`, 'info');
  }

  function handleProgress(data) {
    state.lastPingTimestamp = Date.now();
    const batchIndex = data.batch_index;
    const totalBatches = data.total_batches || state.totalBatches;
    const summary = data.summary || data.cumulative_summary || {};
    const delta = data.batch_delta || {};

    state.lastCommittedBatch = batchIndex;
    state.totalBatches = totalBatches;

    if (summary.added !== undefined) state.metrics.added = summary.added;
    if (summary.updated !== undefined) state.metrics.updated = summary.updated;
    if (summary.withdrawn !== undefined) state.metrics.withdrawn = summary.withdrawn;
    if (summary.skipped !== undefined) state.metrics.skipped = summary.skipped;
    if (summary.dead_letter !== undefined) state.metrics.deadLetter = summary.dead_letter;

    state.lastDeltas = delta;
    updateMetricCards();

    const pct = totalBatches > 0 ? (((batchIndex + 1) / totalBatches) * 100).toFixed(1) : '0.0';
    if (el.progressBarFill) el.progressBarFill.style.width = `${pct}%`;
    if (el.batchProgressText) el.batchProgressText.textContent = `${batchIndex + 1} / ${totalBatches} Batches (${pct}%)`;

    if (state.jobStartTime && el.telemetrySpeed && el.telemetryEta) {
      const elapsedSec = (Date.now() - state.jobStartTime) / 1000;
      const totalRecs = state.metrics.added + state.metrics.updated + state.metrics.withdrawn + state.metrics.skipped + state.metrics.deadLetter;
      if (elapsedSec > 0.5) {
        const speed = Math.round(totalRecs / elapsedSec);
        el.telemetrySpeed.textContent = `${speed} rec/s`;
        const batchesRemaining = totalBatches - (batchIndex + 1);
        if (batchesRemaining > 0 && batchIndex > 0) {
          const avgBatchTime = elapsedSec / (batchIndex + 1);
          const etaSec = Math.round(batchesRemaining * avgBatchTime);
          el.telemetryEta.textContent = `${etaSec}s`;
        } else {
          el.telemetryEta.textContent = 'Finishing';
        }
      }
    }

    updateSmartStatus(`Batch ${batchIndex + 1}/${totalBatches} committed: Added +${delta.added || 0}, Updated ~${delta.updated || 0}, Withdrawn -${delta.withdrawn || 0}, Duplicates skipped ${delta.skipped || 0}.`, 'running');
  }

  function handleDeadLetter(issue) {
    loadDLQ();
    updateSmartStatus(`Defect detected in batch ${issue.batch_index + 1}. Safely quarantined to Dead-Letter Queue.`, 'warning');
  }

  function handleJobCompleted(data) {
    stopRuntimeTimer();
    state.stagedScenario.status = 'Synchronized';
    updateStagedScenarioUI();

    if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = false;
    if (el.mainSyncSpinner) el.mainSyncSpinner.classList.add('hidden');
    if (el.mainSyncText) el.mainSyncText.textContent = 'Sync Again';
    if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.add('hidden');

    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-completed';
      el.activeJobText.textContent = 'Completed';
    }
    if (el.progressBarFill) el.progressBarFill.style.width = '100%';
    if (el.batchProgressText) el.batchProgressText.textContent = `${state.totalBatches} / ${state.totalBatches} Batches (100%)`;
    if (el.btnStartJob) el.btnStartJob.disabled = false;
    if (el.btnCancelJob) el.btnCancelJob.classList.add('hidden');
    if (el.telemetryEta) el.telemetryEta.textContent = 'Done';

    updateSmartStatus('All supplier batches synchronized and committed successfully to storage.', 'success');
    showToast('Synchronization completed successfully', 'success');
    loadCatalog();
    loadHistory();
    loadInterruptedJobs();
    loadAnalyticsMetrics();
  }

  function handleJobInterrupted(data) {
    stopRuntimeTimer();
    state.stagedScenario.status = 'Interrupted';
    updateStagedScenarioUI();

    if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = false;
    if (el.mainSyncSpinner) el.mainSyncSpinner.classList.add('hidden');
    if (el.mainSyncText) el.mainSyncText.textContent = 'Resume Sync';
    if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.add('hidden');

    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-interrupted';
      el.activeJobText.textContent = 'Interrupted';
    }
    if (el.btnStartJob) el.btnStartJob.disabled = false;
    if (el.btnCancelJob) el.btnCancelJob.classList.add('hidden');
    if (el.telemetryEta) el.telemetryEta.textContent = 'Paused';

    updateSmartStatus(`Synchronization paused safely at batch ${data.committed_batch_index + 1}. Checkpoint saved.`, 'warning');
    showToast('Job paused safely at last committed batch.', 'info');
    loadInterruptedJobs();
    loadHistory();
  }

  // --- Demo Simulation Modal Open/Close ---
  function openDemoModal() {
    if (el.demoModal) el.demoModal.classList.remove('hidden');
  }

  function closeDemoModal() {
    if (el.demoModal) el.demoModal.classList.add('hidden');
  }

  if (el.btnOpenDemo) el.btnOpenDemo.addEventListener('click', openDemoModal);
  if (el.btnCloseDemoModal) el.btnCloseDemoModal.addEventListener('click', closeDemoModal);
  if (el.demoModal) {
    el.demoModal.addEventListener('click', (e) => {
      if (e.target === el.demoModal) closeDemoModal();
    });
  }

  // --- Start Sync Job ---
  async function triggerStartJob() {
    if (el.btnStartJob) el.btnStartJob.disabled = true;
    if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = true;
    const spinner = el.btnStartJob ? el.btnStartJob.querySelector('.btn-spinner') : null;
    if (spinner) spinner.classList.remove('hidden');
    if (el.mainSyncSpinner) el.mainSyncSpinner.classList.remove('hidden');
    if (el.mainSyncText) el.mainSyncText.textContent = 'Syncing...';
    if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.remove('hidden');

    state.stagedScenario.status = 'Syncing...';
    updateStagedScenarioUI();

    const payload = {
      mode: 'generator',
      seed: parseInt(el.inputSeed.value || '42', 10),
      total_batches: parseInt(el.inputBatches.value || '10', 10),
      batch_size: parseInt(el.inputBatchSize.value || '100', 10),
      update_ratio: parseFloat(el.inputUpdateRatio.value || '20') / 100.0,
      duplicate_ratio: parseFloat(el.inputDupRatio.value || '5') / 100.0,
      out_of_order_ratio: parseFloat(el.inputOooRatio.value || '10') / 100.0,
      withdrawn_ratio: parseFloat(el.inputWithdrawnRatio.value || '5') / 100.0,
      invalid_ratio: parseFloat(el.inputInvalidRatio.value || '5') / 100.0,
    };

    try {
      const resp = await fetch('/api/jobs', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      });

      const body = await resp.json();
      if (resp.status === 202 || resp.status === 200) {
        showToast(`Synchronization started: ${body.job_id}`, 'success');
        if (el.demoModal) el.demoModal.classList.add('hidden');
      } else if (resp.status === 409) {
        showToast(body.error || 'A synchronization job is already running!', 'error');
        if (el.btnStartJob) el.btnStartJob.disabled = false;
        if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = false;
        if (el.mainSyncSpinner) el.mainSyncSpinner.classList.add('hidden');
        if (el.mainSyncText) el.mainSyncText.textContent = 'Start Sync';
        if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.add('hidden');
        state.stagedScenario.status = 'Ready to Sync';
        updateStagedScenarioUI();
      } else {
        showToast(body.error || 'Failed to start synchronization', 'error');
        if (el.btnStartJob) el.btnStartJob.disabled = false;
        if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = false;
        if (el.mainSyncSpinner) el.mainSyncSpinner.classList.add('hidden');
        if (el.mainSyncText) el.mainSyncText.textContent = 'Start Sync';
        if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.add('hidden');
        state.stagedScenario.status = 'Ready to Sync';
        updateStagedScenarioUI();
      }
    } catch (err) {
      showToast(`Network error: ${err.message}`, 'error');
      if (el.btnStartJob) el.btnStartJob.disabled = false;
      if (el.btnStartSyncMain) el.btnStartSyncMain.disabled = false;
      if (el.mainSyncSpinner) el.mainSyncSpinner.classList.add('hidden');
      if (el.mainSyncText) el.mainSyncText.textContent = 'Start Sync';
      if (el.btnCancelSyncMain) el.btnCancelSyncMain.classList.add('hidden');
      state.stagedScenario.status = 'Ready to Sync';
      updateStagedScenarioUI();
    } finally {
      if (spinner) spinner.classList.add('hidden');
    }
  }

  if (el.btnStartJob) {
    el.btnStartJob.addEventListener('click', triggerStartJob);
  }

  // Staged Scenario Execution Buttons on Main Page
  if (el.btnStartSyncMain) {
    el.btnStartSyncMain.addEventListener('click', triggerStartJob);
  }
  if (el.btnCancelSyncMain) {
    el.btnCancelSyncMain.addEventListener('click', () => {
      if (el.btnCancelJob) el.btnCancelJob.click();
    });
  }
  if (el.btnReopenScenarios) {
    el.btnReopenScenarios.addEventListener('click', openDemoModal);
  }
  if (el.btnApplyScenario) {
    el.btnApplyScenario.addEventListener('click', () => {
      closeDemoModal();
      updateSmartStatus(`Scenario "${state.stagedScenario.name}" staged. Review feed parameters and click "Start Sync".`, 'normal');
      showToast(`Staged "${state.stagedScenario.name}". Click Start Sync on main page.`, 'info');
      if (el.batchProgressText) el.batchProgressText.textContent = `0 / ${state.stagedScenario.batches} Batches (Ready)`;
      if (el.progressBarFill) el.progressBarFill.style.width = '0%';
    });
  }

  if (el.generatorForm) {
    el.generatorForm.addEventListener('submit', (e) => {
      e.preventDefault();
      triggerStartJob();
    });
  }

  // --- Cancel Running Job ---
  if (el.btnCancelJob) {
    el.btnCancelJob.addEventListener('click', async () => {
      if (!state.activeJob || !state.activeJob.job_id) return;
      try {
        const resp = await fetch(`/api/jobs/${encodeURIComponent(state.activeJob.job_id)}/cancel`, {
          method: 'POST',
        });
        if (resp.ok) {
          showToast('Cancellation request sent. Stopping safely...', 'info');
        } else {
          showToast('Failed to cancel job', 'error');
        }
      } catch (err) {
        showToast(`Error: ${err.message}`, 'error');
      }
    });
  }

  // --- Reset Entire Database ---
  if (el.btnResetDb) {
    el.btnResetDb.addEventListener('click', async () => {
      if (!confirm('Are you sure you want to reset the entire database? All products, synchronization history, and quarantined defect records will be permanently erased.')) {
        return;
      }

      el.btnResetDb.disabled = true;
      try {
        const resp = await fetch('/api/database/reset', { method: 'POST' });
        const body = await resp.json();
        if (resp.ok) {
          state.activeJob = null;
          state.metrics = { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 };
          state.lastDeltas = { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 };
          state.totalBatches = 0;
          state.recordsProcessed = 0;
          stopRuntimeTimer();
          updateMetricCards();

          if (el.progressBarFill) el.progressBarFill.style.width = '0%';
          if (el.batchProgressText) el.batchProgressText.textContent = '0 / 0 Batches (0.0%)';
          if (el.telemetrySpeed) el.telemetrySpeed.textContent = '0 rec/s';
          if (el.telemetryEta) el.telemetryEta.textContent = '—';
          if (el.telemetryRuntime) el.telemetryRuntime.textContent = '00:00';
          if (el.activeJobBadge) {
            el.activeJobBadge.className = 'badge badge-job status-idle';
            el.activeJobText.textContent = 'Ready';
          }
          if (el.btnStartJob) el.btnStartJob.disabled = false;
          if (el.btnCancelJob) el.btnCancelJob.classList.add('hidden');

          showToast('Database reset successfully', 'success');
          updateSmartStatus('Database has been completely reset to a clean state.', 'normal');

          loadCatalog();
          loadDLQ();
          loadHistory();
          loadInterruptedJobs();
          loadAnalyticsMetrics();
        } else {
          showToast(body.error || 'Failed to reset database', 'error');
        }
      } catch (err) {
        showToast(`Network error: ${err.message}`, 'error');
      } finally {
        el.btnResetDb.disabled = false;
      }
    });
  }

  // --- Resume Interrupted Job ---
  async function loadInterruptedJobs() {
    if (!el.selectInterruptedJob) return;
    try {
      const resp = await fetch('/api/jobs?status=INTERRUPTED&limit=50');
      if (!resp.ok) return;
      const data = await resp.json();
      const jobs = data.jobs || [];

      el.selectInterruptedJob.innerHTML = '';
      if (jobs.length === 0) {
        el.selectInterruptedJob.innerHTML = '<option value="">No interrupted jobs found (Clean state)</option>';
        if (el.btnResumeJob) el.btnResumeJob.disabled = true;
        if (el.resumeJobDetails) el.resumeJobDetails.classList.add('hidden');
        return;
      }

      el.selectInterruptedJob.innerHTML = '<option value="">-- Select an interrupted job --</option>';
      jobs.forEach((j) => {
        const opt = document.createElement('option');
        opt.value = j.job_id;
        opt.textContent = `Job: ${j.job_id.substring(0, 16)}... | Committed: ${j.committed_batch_index + 1}/${j.total_batches} batches | ${new Date(j.created_at).toLocaleTimeString()}`;
        opt.dataset.job = JSON.stringify(j);
        el.selectInterruptedJob.appendChild(opt);
      });
      if (el.btnResumeJob) el.btnResumeJob.disabled = true;
    } catch (err) {
      console.error('Error fetching interrupted jobs', err);
    }
  }

  if (el.btnRefreshInterrupted) {
    el.btnRefreshInterrupted.addEventListener('click', loadInterruptedJobs);
  }

  if (el.selectInterruptedJob) {
    el.selectInterruptedJob.addEventListener('change', () => {
      const selectedOpt = el.selectInterruptedJob.selectedOptions[0];
      if (!selectedOpt || !selectedOpt.value) {
        if (el.btnResumeJob) el.btnResumeJob.disabled = true;
        if (el.resumeJobDetails) el.resumeJobDetails.classList.add('hidden');
        return;
      }
      if (el.btnResumeJob) el.btnResumeJob.disabled = false;
      const job = JSON.parse(selectedOpt.dataset.job);
      if (el.resumeJobDetails) {
        el.resumeJobDetails.innerHTML = `
          <div class="code-block-wrapper">
            <strong>Job ID:</strong> ${job.job_id}<br>
            <strong>Last Checkpoint:</strong> Batch ${job.committed_batch_index + 1} / ${job.total_batches}<br>
            <strong>Storage Integrity:</strong> All batches committed prior to interruption remain atomic in WAL storage.
          </div>
        `;
        el.resumeJobDetails.classList.remove('hidden');
      }
    });
  }

  if (el.btnResumeJob) {
    el.btnResumeJob.addEventListener('click', async () => {
      const jobID = el.selectInterruptedJob.value;
      if (!jobID) return;

      el.btnResumeJob.disabled = true;
      const spinner = el.btnResumeJob.querySelector('.btn-spinner');
      if (spinner) spinner.classList.remove('hidden');

      try {
        const resp = await fetch(`/api/jobs/${encodeURIComponent(jobID)}/resume`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
        });
        const body = await resp.json();
        if (resp.status === 202 || resp.status === 200) {
          showToast(`Resumed job ${jobID}`, 'success');
          if (el.demoModal) el.demoModal.classList.add('hidden');
        } else {
          showToast(body.error || 'Failed to resume job', 'error');
          el.btnResumeJob.disabled = false;
        }
      } catch (err) {
        showToast(`Network error: ${err.message}`, 'error');
        el.btnResumeJob.disabled = false;
      } finally {
        if (spinner) spinner.classList.add('hidden');
      }
    });
  }

  // --- Custom JSON Batch Submission ---
  if (el.btnLoadSampleJson) {
    el.btnLoadSampleJson.addEventListener('click', () => {
      const sample = [
        {
          sku: 'DESK-OAK-01',
          name: 'Minimalist Oak Desk',
          price: 199.99,
          status: 'ACTIVE',
          timestamp: new Date().toISOString(),
        },
        {
          sku: 'CHAIR-ERGONOMIC-02',
          name: 'Ergonomic Precision Chair',
          price: 249.50,
          status: 'ACTIVE',
          timestamp: new Date().toISOString(),
        },
      ];
      el.manualJsonInput.value = JSON.stringify(sample, null, 2);
    });
  }

  if (el.btnClearJson) {
    el.btnClearJson.addEventListener('click', () => {
      el.manualJsonInput.value = '';
      if (el.jsonParseStatus) el.jsonParseStatus.textContent = '';
    });
  }

  if (el.btnSubmitManual) {
    el.btnSubmitManual.addEventListener('click', async () => {
      const text = el.manualJsonInput.value.trim();
      if (!text) {
        showToast('Please provide a valid JSON array of supplier records', 'error');
        return;
      }
      let records;
      try {
        records = JSON.parse(text);
        if (!Array.isArray(records)) {
          throw new Error('Payload must be a JSON array [] of records');
        }
      } catch (err) {
        if (el.jsonParseStatus) {
          el.jsonParseStatus.textContent = `Invalid JSON: ${err.message}`;
          el.jsonParseStatus.style.color = 'var(--color-deadletter)';
        }
        return;
      }

      const payload = {
        mode: 'batches',
        batches: [{ batch_index: 0, records: records }],
      };

      try {
        const resp = await fetch('/api/jobs', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const body = await resp.json();
        if (resp.status === 202 || resp.status === 200) {
          showToast(`Custom batch submitted: ${body.job_id}`, 'success');
          if (el.demoModal) el.demoModal.classList.add('hidden');
          el.manualJsonInput.value = '';
          if (el.jsonParseStatus) el.jsonParseStatus.textContent = '';
        } else {
          showToast(body.error || 'Failed to submit batch', 'error');
        }
      } catch (err) {
        showToast(`Network error: ${err.message}`, 'error');
      }
    });
  }

  // --- Explorer: Dead-Letter Queue Table ---
  async function loadDLQ() {
    if (!el.dlqTableBody) return;
    const reason = el.filterDlqReason ? el.filterDlqReason.value.trim() : '';
    let url = `/api/dead-letter?limit=${state.dlq.limit}&offset=${state.dlq.offset}`;
    if (reason) url += `&reason=${encodeURIComponent(reason)}`;

    try {
      const resp = await fetch(url);
      if (!resp.ok) return;
      const data = await resp.json();
      const issues = data.issues || [];
      state.dlq.total = data.total || 0;
      if (el.countDlqBadge) el.countDlqBadge.textContent = state.dlq.total.toLocaleString();

      el.dlqTableBody.innerHTML = '';
      if (issues.length === 0) {
        el.dlqTableBody.innerHTML = '<tr><td colspan="6" class="table-empty">Zero quarantined records. Ingestion clean.</td></tr>';
        if (el.dlqPaginationInfo) el.dlqPaginationInfo.textContent = 'Showing 0-0 of 0';
        if (el.btnDlqPrev) el.btnDlqPrev.disabled = true;
        if (el.btnDlqNext) el.btnDlqNext.disabled = true;
        return;
      }

      issues.forEach((issue) => {
        const tr = document.createElement('tr');
        const shortIssueId = issue.issue_id.length > 16 ? `${issue.issue_id.substring(0, 16)}...` : issue.issue_id;
        const timeStr = new Date(issue.created_at).toLocaleTimeString();

        tr.innerHTML = `
          <td><span class="code-badge" title="${issue.issue_id}">${shortIssueId}</span></td>
          <td>Batch ${issue.batch_index + 1}</td>
          <td class="sku-cell">${issue.sku ? issue.sku : '<span class="defect-badge">[EMPTY SKU]</span>'}</td>
          <td><span class="defect-badge">${issue.reason}</span></td>
          <td>${timeStr}</td>
          <td><button class="btn btn-xs btn-outline btn-inspect" data-issue='${JSON.stringify(issue).replace(/'/g, '&apos;')}'>Inspect</button></td>
        `;
        el.dlqTableBody.appendChild(tr);
      });

      const from = state.dlq.offset + 1;
      const to = Math.min(state.dlq.offset + state.dlq.limit, state.dlq.total);
      if (el.dlqPaginationInfo) el.dlqPaginationInfo.textContent = `Showing ${from}-${to} of ${state.dlq.total}`;
      const curPage = Math.floor(state.dlq.offset / state.dlq.limit) + 1;
      const totalPages = Math.ceil(state.dlq.total / state.dlq.limit) || 1;
      if (el.dlqPageNum) el.dlqPageNum.textContent = `Page ${curPage} of ${totalPages}`;

      if (el.btnDlqPrev) el.btnDlqPrev.disabled = state.dlq.offset <= 0;
      if (el.btnDlqNext) el.btnDlqNext.disabled = state.dlq.offset + state.dlq.limit >= state.dlq.total;
    } catch (err) {
      console.error('Failed to load DLQ', err);
    }
  }

  if (el.btnRefreshDlq) el.btnRefreshDlq.addEventListener('click', loadDLQ);
  if (el.filterDlqReason) {
    el.filterDlqReason.addEventListener('input', () => {
      state.dlq.offset = 0;
      loadDLQ();
    });
  }

  if (el.btnDlqPrev) {
    el.btnDlqPrev.addEventListener('click', () => {
      if (state.dlq.offset >= state.dlq.limit) {
        state.dlq.offset -= state.dlq.limit;
        loadDLQ();
      }
    });
  }

  if (el.btnDlqNext) {
    el.btnDlqNext.addEventListener('click', () => {
      if (state.dlq.offset + state.dlq.limit < state.dlq.total) {
        state.dlq.offset += state.dlq.limit;
        loadDLQ();
      }
    });
  }

  // Modal Payload Inspector
  if (el.dlqTableBody) {
    el.dlqTableBody.addEventListener('click', (e) => {
      const btn = e.target.closest('.btn-inspect');
      if (!btn) return;
      const issue = JSON.parse(btn.getAttribute('data-issue'));
      openPayloadModal(issue);
    });
  }

  function openPayloadModal(issue) {
    if (!el.payloadModal) return;
    state.currentRemedyIssue = issue;

    if (el.modalIssueId) el.modalIssueId.textContent = issue.issue_id || '—';
    if (el.modalIssueBatch) el.modalIssueBatch.textContent = `Batch #${(issue.batch_index ?? 0) + 1}`;
    if (el.modalDefectReason) el.modalDefectReason.textContent = issue.reason || 'Schema or invariant violation';

    let parsed = {};
    try {
      parsed = JSON.parse(issue.raw_payload || '{}');
      if (el.modalPayloadCode) el.modalPayloadCode.textContent = JSON.stringify(parsed, null, 2);
    } catch (_) {
      parsed = {};
      if (el.modalPayloadCode) el.modalPayloadCode.textContent = issue.raw_payload || '';
    }

    // Populate form fields with intelligent remediation defaults
    const sku = issue.sku || parsed.sku || '';
    if (el.remedySku) el.remedySku.value = sku;
    if (el.remedyName) el.remedyName.value = parsed.name || 'Corrected Item';

    let price = parsed.price;
    if (price === undefined || price === null || isNaN(price) || price < 0 || !isFinite(price)) {
      price = 49.99;
    }
    if (el.remedyPrice) el.remedyPrice.value = Number(price).toFixed(2);

    let status = parsed.status || 'ACTIVE';
    if (status !== 'ACTIVE' && status !== 'WITHDRAWN') {
      status = 'ACTIVE';
    }
    if (el.remedyStatus) el.remedyStatus.value = status;

    let ts = parsed.timestamp;
    if (!ts || ts.startsWith('0001-01-01')) {
      ts = new Date().toISOString();
    }
    if (el.remedyTimestamp) el.remedyTimestamp.value = ts;

    if (el.modalPayloadCode) el.modalPayloadCode.classList.add('hidden');
    if (el.btnToggleRawPayload) el.btnToggleRawPayload.textContent = 'Show raw supplier payload JSON';

    el.payloadModal.classList.remove('hidden');
  }

  function closePayloadModal() {
    if (el.payloadModal) {
      el.payloadModal.classList.add('hidden');
    }
    state.currentRemedyIssue = null;
  }

  // Toggle raw payload JSON preview
  if (el.btnToggleRawPayload && el.modalPayloadCode) {
    el.btnToggleRawPayload.addEventListener('click', () => {
      const isHidden = el.modalPayloadCode.classList.toggle('hidden');
      el.btnToggleRawPayload.textContent = isHidden ? 'Show raw supplier payload JSON' : 'Hide raw supplier payload JSON';
    });
  }

  // Resolve and import product into catalog
  async function resolveAndImportProduct() {
    if (!state.currentRemedyIssue || !el.btnResolveAndImport) return;

    const issueId = state.currentRemedyIssue.issue_id;
    const sku = el.remedySku ? el.remedySku.value.trim() : '';
    const name = el.remedyName ? el.remedyName.value.trim() : '';
    const price = el.remedyPrice ? parseFloat(el.remedyPrice.value) : NaN;
    const status = el.remedyStatus ? el.remedyStatus.value : 'ACTIVE';
    let timestamp = el.remedyTimestamp ? el.remedyTimestamp.value.trim() : '';

    if (!sku) {
      showToast('Product SKU cannot be empty', 'error');
      if (el.remedySku) el.remedySku.focus();
      return;
    }
    if (!name) {
      showToast('Product name cannot be empty', 'error');
      if (el.remedyName) el.remedyName.focus();
      return;
    }
    if (isNaN(price) || price < 0 || !isFinite(price)) {
      showToast('Please enter a valid non-negative price', 'error');
      if (el.remedyPrice) el.remedyPrice.focus();
      return;
    }

    if (!timestamp) {
      timestamp = new Date().toISOString();
    }

    el.btnResolveAndImport.disabled = true;
    if (el.resolveSpinner) el.resolveSpinner.classList.remove('hidden');
    if (el.btnResolveText) el.btnResolveText.textContent = 'Importing...';

    try {
      const res = await fetch(`/api/dead-letter/${encodeURIComponent(issueId)}/resolve`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          sku,
          name,
          price,
          status,
          timestamp,
        }),
      });

      const body = await res.json();
      if (res.ok) {
        closePayloadModal();
        showToast(`Product "${sku}" resolved and imported to catalog!`, 'success');
        updateSmartStatus(`Remediated product ${sku} imported into catalog. Quarantined defect cleared.`, 'success');

        // Optimistically update counts & charts
        state.metrics.deadLetter = Math.max(0, state.metrics.deadLetter - 1);
        if (status === 'ACTIVE') {
          state.metrics.added += 1;
        } else {
          state.metrics.withdrawn += 1;
        }
        updateMetricCards();
        renderDonutChart();
        updateHealthGauge();

        // Refresh data tables
        loadDLQ();
        loadCatalog();
        loadHistory();

        // Refresh metric modal if open
        if (state.metricModal && state.metricModal.type) {
          loadMetricModalData();
        }
      } else {
        showToast(body.error || 'Failed to remediate product', 'error');
      }
    } catch (err) {
      showToast(`Network error: ${err.message}`, 'error');
    } finally {
      el.btnResolveAndImport.disabled = false;
      if (el.resolveSpinner) el.resolveSpinner.classList.add('hidden');
      if (el.btnResolveText) el.btnResolveText.textContent = 'Save & Import to Catalog';
    }
  }

  if (el.btnResolveAndImport) {
    el.btnResolveAndImport.addEventListener('click', resolveAndImportProduct);
  }

  if (el.formRemediateProduct) {
    el.formRemediateProduct.addEventListener('submit', (e) => {
      e.preventDefault();
      resolveAndImportProduct();
    });
  }

  if (el.btnCloseModal) el.btnCloseModal.addEventListener('click', closePayloadModal);
  if (el.btnDismissModal) el.btnDismissModal.addEventListener('click', closePayloadModal);
  if (el.payloadModal) {
    el.payloadModal.addEventListener('click', (e) => {
      if (e.target === el.payloadModal) closePayloadModal();
    });
  }
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      if (el.metricDetailModal && !el.metricDetailModal.classList.contains('hidden')) {
        closeMetricModal();
        return;
      }
      if (el.payloadModal && !el.payloadModal.classList.contains('hidden')) {
        closePayloadModal();
        return;
      }
      if (el.demoModal && !el.demoModal.classList.contains('hidden')) {
        closeDemoModal();
        return;
      }
    }
  });

  // --- Helper: Safe HTML Escaping ---
  function escapeHTML(str) {
    if (!str) return '';
    return String(str)
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;')
      .replace(/'/g, '&#039;');
  }

  // --- Metric Drill-Down Modal Configuration & Handlers ---
  const METRIC_CONFIG = {
    added: {
      title: 'New Products Added',
      subtitle: 'Products currently active in the store catalog.',
      searchPlaceholder: 'Filter active products by SKU or name...',
      emptyMessage: 'No products currently recorded.',
      columns: ['SKU', 'Product Name', 'Price', 'Status', 'Supplier Timestamp', 'Last Updated'],
    },
    updated: {
      title: 'Price & Info Updates',
      subtitle: 'Catalog products that have received price or specification updates.',
      searchPlaceholder: 'Filter updated products by SKU or name...',
      emptyMessage: 'No products currently recorded.',
      columns: ['SKU', 'Product Name', 'Price', 'Status', 'Supplier Timestamp', 'Last Updated'],
    },
    withdrawn: {
      title: 'Discontinued / Withdrawn Products',
      subtitle: 'Products marked as WITHDRAWN (Tombstoned in accordance with Invariant B).',
      searchPlaceholder: 'Filter withdrawn products by SKU or name...',
      emptyMessage: 'Zero products withdrawn. All catalog items active.',
      columns: ['SKU', 'Product Name', 'Price', 'Status', 'Supplier Timestamp', 'Last Updated'],
    },
    skipped: {
      title: 'Duplicates Preserved (Unchanged)',
      subtitle: 'Products where redundant duplicate records or older timestamps were safely ignored to preserve catalog integrity.',
      searchPlaceholder: 'Filter preserved products by SKU or name...',
      emptyMessage: 'No duplicate records processed.',
      columns: ['SKU', 'Product Name', 'Price', 'Status', 'Supplier Timestamp', 'Last Updated'],
    },
    deadletter: {
      title: 'Needs Attention — Dead Letter Queue (DLQ)',
      subtitle: 'Corrupt, malformed, or invariant-violating records quarantined for review.',
      searchPlaceholder: 'Filter quarantined records by defect reason...',
      emptyMessage: 'Zero quarantined records. Ingestion is clean.',
      columns: ['Issue ID', 'Batch', 'SKU', 'Rejection Reason', 'Quarantined At', 'Action'],
    },
  };

  function openMetricModal(type) {
    if (!METRIC_CONFIG[type] || !el.metricDetailModal) return;
    const cfg = METRIC_CONFIG[type];
    state.metricModal.type = type;
    state.metricModal.search = '';
    state.metricModal.offset = 0;

    if (el.metricModalTitle) el.metricModalTitle.textContent = cfg.title;
    if (el.metricModalSubtitle) el.metricModalSubtitle.textContent = cfg.subtitle;
    if (el.metricModalSearch) {
      el.metricModalSearch.value = '';
      el.metricModalSearch.placeholder = cfg.searchPlaceholder;
    }

    if (el.metricModalThead) {
      el.metricModalThead.innerHTML = `<tr>${cfg.columns.map((col) => `<th>${col}</th>`).join('')}</tr>`;
    }

    loadMetricModalData();
    el.metricDetailModal.classList.remove('hidden');
  }

  function closeMetricModal() {
    if (el.metricDetailModal) {
      el.metricDetailModal.classList.add('hidden');
    }
  }

  async function loadMetricModalData() {
    const { type, search, limit, offset } = state.metricModal;
    const cfg = METRIC_CONFIG[type];
    if (!cfg || !el.metricModalTbody) return;

    el.metricModalTbody.innerHTML = `<tr><td colspan="${cfg.columns.length}" class="table-empty">Loading records...</td></tr>`;

    try {
      if (type === 'deadletter') {
        let url = `/api/dead-letter?reason=${encodeURIComponent(search)}&limit=${limit}&offset=${offset}`;
        if (state.analytics.selectedJobId) {
          url += `&job_id=${encodeURIComponent(state.analytics.selectedJobId)}`;
        }
        const res = await fetch(url);
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        const issues = data.issues || [];
        state.metricModal.total = data.total || 0;

        if (issues.length === 0) {
          el.metricModalTbody.innerHTML = `<tr><td colspan="${cfg.columns.length}" class="table-empty">${cfg.emptyMessage}</td></tr>`;
        } else {
          el.metricModalTbody.innerHTML = '';
          issues.forEach((issue) => {
            const tr = document.createElement('tr');
            const timeStr = issue.created_at ? new Date(issue.created_at).toLocaleTimeString() : '—';
            tr.innerHTML = `
              <td><span class="code-badge">${escapeHTML((issue.issue_id || '').slice(0, 16))}</span></td>
              <td>Batch #${issue.batch_index + 1}</td>
              <td class="sku-cell">${escapeHTML(issue.sku || '[EMPTY]')}</td>
              <td><span class="defect-badge">${escapeHTML(issue.reason)}</span></td>
              <td>${timeStr}</td>
              <td>
                <button class="btn btn-xs btn-outline btn-inspect" data-issue="${escapeHTML(JSON.stringify(issue))}">Inspect</button>
              </td>
            `;
            el.metricModalTbody.appendChild(tr);
          });
        }
      } else {
        let statusParam = '';
        if (type === 'added') statusParam = 'ACTIVE';
        if (type === 'withdrawn') statusParam = 'WITHDRAWN';

        let url = `/api/products?limit=${limit}&offset=${offset}`;
        if (statusParam) url += `&status=${encodeURIComponent(statusParam)}`;
        if (search) url += `&search=${encodeURIComponent(search)}`;

        const res = await fetch(url);
        if (!res.ok) throw new Error('HTTP ' + res.status);
        const data = await res.json();
        const products = data.products || [];
        state.metricModal.total = data.total || 0;

        if (products.length === 0) {
          el.metricModalTbody.innerHTML = `<tr><td colspan="${cfg.columns.length}" class="table-empty">${cfg.emptyMessage}</td></tr>`;
        } else {
          el.metricModalTbody.innerHTML = '';
          products.forEach((p) => {
            const tr = document.createElement('tr');
            const priceStr = `$${Number(p.price).toFixed(2)}`;
            const supTime = p.supplier_timestamp ? new Date(p.supplier_timestamp).toLocaleTimeString() : '—';
            const updTime = p.updated_at ? new Date(p.updated_at).toLocaleTimeString() : '—';
            tr.innerHTML = `
              <td class="sku-cell">${escapeHTML(p.sku)}</td>
              <td>${escapeHTML(p.name)}</td>
              <td><strong>${priceStr}</strong></td>
              <td><span class="status-badge ${p.status}">${p.status}</span></td>
              <td>${supTime}</td>
              <td>${updTime}</td>
            `;
            el.metricModalTbody.appendChild(tr);
          });
        }
      }

      // Pagination
      const total = state.metricModal.total;
      const start = total === 0 ? 0 : offset + 1;
      const end = Math.min(offset + limit, total);
      const currentPage = Math.floor(offset / limit) + 1;
      const maxPages = Math.max(1, Math.ceil(total / limit));

      if (el.metricModalCountBadge) {
        el.metricModalCountBadge.textContent = `${total.toLocaleString()} total items`;
      }
      if (el.metricModalPaginationInfo) {
        el.metricModalPaginationInfo.textContent = `Showing ${start}-${end} of ${total.toLocaleString()}`;
      }
      if (el.metricModalPageNum) {
        el.metricModalPageNum.textContent = `Page ${currentPage} of ${maxPages}`;
      }
      if (el.btnMetricModalPrev) {
        el.btnMetricModalPrev.disabled = offset <= 0;
      }
      if (el.btnMetricModalNext) {
        el.btnMetricModalNext.disabled = offset + limit >= total;
      }
    } catch (err) {
      console.error('Failed to load metric modal records', err);
      el.metricModalTbody.innerHTML = `<tr><td colspan="${cfg.columns.length}" class="table-empty text-danger">Failed to load records: ${escapeHTML(err.message)}</td></tr>`;
    }
  }

  // Wire Metric Cards Clicks
  document.querySelectorAll('.metric-card[data-metric]').forEach((card) => {
    card.addEventListener('click', () => {
      const type = card.getAttribute('data-metric');
      openMetricModal(type);
    });
    card.addEventListener('keydown', (e) => {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        const type = card.getAttribute('data-metric');
        openMetricModal(type);
      }
    });
  });

  // Metric Modal Controls
  if (el.btnCloseMetricModal) el.btnCloseMetricModal.addEventListener('click', closeMetricModal);
  if (el.btnDismissMetricModal) el.btnDismissMetricModal.addEventListener('click', closeMetricModal);
  if (el.metricDetailModal) {
    el.metricDetailModal.addEventListener('click', (e) => {
      if (e.target === el.metricDetailModal) closeMetricModal();
    });
  }

  // Metric Modal Search (Debounced 250ms)
  if (el.metricModalSearch) {
    el.metricModalSearch.addEventListener('input', () => {
      clearTimeout(state.metricModal.searchTimer);
      state.metricModal.searchTimer = setTimeout(() => {
        state.metricModal.search = el.metricModalSearch.value.trim();
        state.metricModal.offset = 0;
        loadMetricModalData();
      }, 250);
    });
  }

  // Metric Modal Pagination
  if (el.btnMetricModalPrev) {
    el.btnMetricModalPrev.addEventListener('click', () => {
      if (state.metricModal.offset >= state.metricModal.limit) {
        state.metricModal.offset -= state.metricModal.limit;
        loadMetricModalData();
      }
    });
  }
  if (el.btnMetricModalNext) {
    el.btnMetricModalNext.addEventListener('click', () => {
      if (state.metricModal.offset + state.metricModal.limit < state.metricModal.total) {
        state.metricModal.offset += state.metricModal.limit;
        loadMetricModalData();
      }
    });
  }

  // Metric Modal Table DLQ Inspect Delegate
  if (el.metricModalTbody) {
    el.metricModalTbody.addEventListener('click', (e) => {
      const btn = e.target.closest('.btn-inspect');
      if (!btn) return;
      try {
        const issue = JSON.parse(btn.getAttribute('data-issue'));
        openPayloadModal(issue);
      } catch (err) {
        console.error('Failed to parse DLQ issue for inspection', err);
      }
    });
  }

  // --- Explorer: Catalog Products Table ---
  let searchDebounceTimer = null;
  async function loadCatalog() {
    if (!el.catalogTableBody) return;
    const search = el.filterProductSearch ? el.filterProductSearch.value.trim() : '';
    const status = el.filterProductStatus ? el.filterProductStatus.value : '';
    const url = `/api/products?limit=${state.catalog.limit}&offset=${state.catalog.offset}${search ? `&search=${encodeURIComponent(search)}` : ''}${status ? `&status=${encodeURIComponent(status)}` : ''}`;

    try {
      const resp = await fetch(url);
      if (!resp.ok) return;
      const data = await resp.json();
      const products = data.products || [];
      state.catalog.total = data.total || 0;
      if (el.countCatalogBadge) el.countCatalogBadge.textContent = state.catalog.total.toLocaleString();

      el.catalogTableBody.innerHTML = '';
      if (products.length === 0) {
        el.catalogTableBody.innerHTML = '<tr><td colspan="6" class="table-empty">No products match current filters.</td></tr>';
        if (el.catalogPaginationInfo) el.catalogPaginationInfo.textContent = 'Showing 0-0 of 0';
        if (el.btnCatalogPrev) el.btnCatalogPrev.disabled = true;
        if (el.btnCatalogNext) el.btnCatalogNext.disabled = true;
        return;
      }

      products.forEach((p) => {
        const tr = document.createElement('tr');
        const priceStr = `$${Number(p.price).toFixed(2)}`;
        const supTimeStr = new Date(p.supplier_timestamp).toLocaleTimeString();
        const updTimeStr = new Date(p.updated_at).toLocaleTimeString();

        tr.innerHTML = `
          <td class="sku-cell">${p.sku}</td>
          <td>${p.name}</td>
          <td><strong>${priceStr}</strong></td>
          <td><span class="status-badge ${p.status}">${p.status}</span></td>
          <td>${supTimeStr}</td>
          <td>${updTimeStr}</td>
        `;
        el.catalogTableBody.appendChild(tr);
      });

      const from = state.catalog.offset + 1;
      const to = Math.min(state.catalog.offset + state.catalog.limit, state.catalog.total);
      if (el.catalogPaginationInfo) el.catalogPaginationInfo.textContent = `Showing ${from}-${to} of ${state.catalog.total.toLocaleString()}`;
      const curPage = Math.floor(state.catalog.offset / state.catalog.limit) + 1;
      const totalPages = Math.ceil(state.catalog.total / state.catalog.limit) || 1;
      if (el.catalogPageNum) el.catalogPageNum.textContent = `Page ${curPage} of ${totalPages}`;

      if (el.btnCatalogPrev) el.btnCatalogPrev.disabled = state.catalog.offset <= 0;
      if (el.btnCatalogNext) el.btnCatalogNext.disabled = state.catalog.offset + state.catalog.limit >= state.catalog.total;
    } catch (err) {
      console.error('Failed to load catalog products', err);
    }
  }

  if (el.btnRefreshCatalog) el.btnRefreshCatalog.addEventListener('click', loadCatalog);
  if (el.filterProductStatus) {
    el.filterProductStatus.addEventListener('change', () => {
      state.catalog.offset = 0;
      loadCatalog();
    });
  }

  if (el.filterProductSearch) {
    el.filterProductSearch.addEventListener('input', () => {
      clearTimeout(searchDebounceTimer);
      searchDebounceTimer = setTimeout(() => {
        state.catalog.offset = 0;
        loadCatalog();
      }, 250);
    });
  }

  if (el.btnCatalogPrev) {
    el.btnCatalogPrev.addEventListener('click', () => {
      if (state.catalog.offset >= state.catalog.limit) {
        state.catalog.offset -= state.catalog.limit;
        loadCatalog();
      }
    });
  }

  if (el.btnCatalogNext) {
    el.btnCatalogNext.addEventListener('click', () => {
      if (state.catalog.offset + state.catalog.limit < state.catalog.total) {
        state.catalog.offset += state.catalog.limit;
        loadCatalog();
      }
    });
  }

  // --- Explorer: History Table ---
  async function loadHistory() {
    if (!el.historyTableBody) return;
    try {
      const resp = await fetch('/api/jobs?limit=20&offset=0');
      if (!resp.ok) return;
      const data = await resp.json();
      const jobs = data.jobs || [];

      el.historyTableBody.innerHTML = '';
      if (jobs.length === 0) {
        el.historyTableBody.innerHTML = '<tr><td colspan="8" class="table-empty">No synchronization jobs recorded yet.</td></tr>';
        return;
      }

      jobs.forEach((j) => {
        const tr = document.createElement('tr');
        const s = j.summary || {};
        const timeStr = new Date(j.created_at).toLocaleString();

        tr.innerHTML = `
          <td><span class="code-badge">${j.job_id.substring(0, 16)}...</span></td>
          <td><span class="status-badge ${j.status}">${j.status}</span></td>
          <td>${j.total_batches}</td>
          <td>${j.committed_batch_index + 1}</td>
          <td style="color:var(--color-added)">+${(s.added || 0).toLocaleString()}</td>
          <td style="color:var(--color-updated)">~${(s.updated || 0).toLocaleString()}</td>
          <td style="color:var(--color-withdrawn)">-${(s.withdrawn || 0).toLocaleString()}</td>
          <td>${timeStr}</td>
        `;
        el.historyTableBody.appendChild(tr);
      });
    } catch (err) {
      console.error('Failed to load history', err);
    }
  }

  if (el.btnRefreshHistory) el.btnRefreshHistory.addEventListener('click', loadHistory);

  // --- Initial Bootstrap ---
  function init() {
    applyPreset('default');
    if (el.inputRangeFrom && el.inputRangeTo) {
      const todayISO = new Date().toISOString().slice(0, 10);
      el.inputRangeFrom.value = todayISO;
      el.inputRangeTo.value = todayISO;
    }
    loadAnalyticsMetrics();
    initSSE();
    loadDLQ();
    loadCatalog();
    loadInterruptedJobs();
    loadHistory();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
