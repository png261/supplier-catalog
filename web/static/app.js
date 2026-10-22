/**
 * Coderpush Catalog Sync Engine — Non-Tech Friendly Operations Dashboard
 * Zero external dependencies: pure vanilla ES6+ JavaScript, SVG charts & Canvas.
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
    theme: localStorage.getItem('cp_theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'),

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
    dlq: { items: [], total: 0, limit: 10, offset: 0, filterJob: '', filterReason: '' },
    catalog: { items: [], total: 0, limit: 10, offset: 0, search: '', status: '' },
    history: { jobs: [] },
  };

  // --- Preset Configurations ---
  const PRESETS = {
    default: { seed: 42, batches: 10, size: 100, update: 20, dup: 5, ooo: 10, with: 5, inv: 5, name: 'Đồng Bộ Tiêu Chuẩn' },
    clean:   { seed: 101, batches: 10, size: 100, update: 20, dup: 0, ooo: 0, with: 0, inv: 0, name: 'Dữ Liệu Chuẩn 100%' },
    stress:  { seed: 999, batches: 15, size: 150, update: 15, dup: 10, ooo: 15, with: 10, inv: 15, name: 'Kiểm Thử Bất Thường' },
    large:   { seed: 777, batches: 50, size: 200, update: 20, dup: 5, ooo: 10, with: 5, inv: 5, name: 'Đồng Bộ Khối Lượng Lớn' },
  };

  // --- DOM Element References ---
  const el = {
    themeToggle: document.getElementById('theme-toggle'),
    themeIcon: document.getElementById('theme-icon'),
    activeJobBadge: document.getElementById('active-job-badge'),
    activeJobText: document.getElementById('active-job-text'),
    sseStatus: document.getElementById('sse-status'),
    sseStatusText: document.getElementById('sse-status-text'),

    // Non-tech Control & Preset Cards
    presetCards: document.querySelectorAll('.preset-card'),
    btnStartJob: document.getElementById('btn-start-job'),
    btnToggleAdvanced: document.getElementById('btn-toggle-advanced'),
    advancedToggleIcon: document.getElementById('advanced-toggle-icon'),
    advancedSettingsPanel: document.getElementById('advanced-settings-panel'),
    btnCancelJob: document.getElementById('btn-cancel-job'),

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

    // Modal
    payloadModal: document.getElementById('payload-modal'),
    modalIssueId: document.getElementById('modal-issue-id'),
    modalDefectReason: document.getElementById('modal-defect-reason'),
    modalPayloadCode: document.getElementById('modal-payload-code'),
    btnCloseModal: document.getElementById('btn-close-modal'),
    btnDismissModal: document.getElementById('btn-dismiss-modal'),
    btnCopyPayload: document.getElementById('btn-copy-payload'),

    // Toasts
    toastContainer: document.getElementById('toast-container'),
  };

  // --- Theme Management ---
  function applyTheme(theme) {
    state.theme = theme;
    document.documentElement.setAttribute('data-theme', theme);
    localStorage.setItem('cp_theme', theme);
    if (el.themeIcon) {
      el.themeIcon.textContent = theme === 'dark' ? '🌙' : '☀';
    }
    renderDonutChart();
  }

  if (el.themeToggle) {
    el.themeToggle.addEventListener('click', () => {
      applyTheme(state.theme === 'dark' ? 'light' : 'dark');
    });
  }

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
    toast.innerHTML = `<span>${message}</span>`;
    el.toastContainer.appendChild(toast);
    setTimeout(() => {
      toast.style.opacity = '0';
      toast.style.transform = 'translateY(10px)';
      toast.style.transition = 'all 200ms ease';
      setTimeout(() => toast.remove(), 200);
    }, 4000);
  }

  // --- Smart Non-Tech Status Banner ---
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

  // --- Visual Chart 1: Donut Chart Rendering ---
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

    // SVG Circle circumferences: r = 70 => C = 2 * PI * 70 ≈ 439.82297
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

  // --- Visual Chart 2: System Health Gauge Rendering ---
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
        el.healthStatusTitle.textContent = 'Hệ thống an toàn tuyệt đối';
      }
      if (el.healthStatusDesc) {
        el.healthStatusDesc.textContent = 'Toàn bộ giao dịch đều tuân thủ nghiêm ngặt 5 Invariants toán học. Không có sai lệch hay xung đột dữ liệu.';
      }
    } else if (successRate >= 90.0) {
      el.gaugeFill.style.stroke = 'var(--color-withdrawn)';
      if (el.healthStatusBadge) {
        el.healthStatusBadge.className = 'health-status-badge badge-warning';
      }
      if (el.healthStatusTitle) {
        el.healthStatusTitle.textContent = `Cách ly an toàn (${deadLetter.toLocaleString()} bản ghi)`;
      }
      if (el.healthStatusDesc) {
        el.healthStatusDesc.textContent = 'Các bản ghi vi phạm quy tắc (giá âm hoặc thiếu SKU) đã được cách ly vào hàng đợi DLQ. Luồng dữ liệu chính được bảo vệ trọn vẹn.';
      }
    } else {
      el.gaugeFill.style.stroke = 'var(--color-deadletter)';
      if (el.healthStatusBadge) {
        el.healthStatusBadge.className = 'health-status-badge badge-danger';
      }
      if (el.healthStatusTitle) {
        el.healthStatusTitle.textContent = `Cần lưu ý: Tỷ lệ lỗi ${(100 - successRate).toFixed(1)}%`;
      }
      if (el.healthStatusDesc) {
        el.healthStatusDesc.textContent = 'Tỷ lệ bản ghi sai quy cách từ đối tác cao bất thường. Đề nghị kiểm tra chi tiết trong tab "Dữ Liệu Cần Kiểm Tra".';
      }
    }

    // Update Average Speed in Health Card
    if (state.jobStartTime && el.statElapsedTime && el.statAvgSpeed) {
      const elapsedSec = (Date.now() - state.jobStartTime) / 1000;
      if (elapsedSec > 0.5) {
        const mm = String(Math.floor(elapsedSec / 60)).padStart(2, '0');
        const ss = String(Math.floor(elapsedSec % 60)).padStart(2, '0');
        el.statElapsedTime.textContent = `${mm}:${ss}`;
        const avg = Math.round(total / elapsedSec);
        el.statAvgSpeed.textContent = `${avg} sp/giây`;
      }
    }
  }

  // --- Non-Tech Preset Cards Interaction ---
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
  }

  el.presetCards.forEach((card) => {
    card.addEventListener('click', () => {
      el.presetCards.forEach((c) => c.classList.remove('active'));
      card.classList.add('active');
      const presetKey = card.getAttribute('data-preset');
      applyPreset(presetKey);
      const title = card.querySelector('.preset-card-title') ? card.querySelector('.preset-card-title').textContent : presetKey;
      updateSmartStatus(`Đã chọn "${title}". Bấm "Bắt Đầu Đồng Bộ Ngay" để kích hoạt cập nhật.`, 'normal');
    });
  });

  // --- Advanced Panel Accordion Toggle ---
  if (el.btnToggleAdvanced && el.advancedSettingsPanel) {
    el.btnToggleAdvanced.addEventListener('click', () => {
      const isHidden = el.advancedSettingsPanel.classList.toggle('hidden');
      if (el.advancedToggleIcon) {
        el.advancedToggleIcon.textContent = isHidden ? '▼' : '▲';
      }
      const labelSpan = el.btnToggleAdvanced.querySelector('span');
      if (labelSpan) {
        labelSpan.textContent = isHidden ? '⚙️ Tùy Chỉnh Nâng Cao' : '⚙️ Thu Gọn Tùy Chỉnh';
      }
    });
  }

  // --- Form Ratio Validator ---
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
        el.totalRatioVal.textContent = `${total}% (VƯỢT QUÁ 100%)`;
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
      el.sseStatusText.textContent = 'Đang kết nối thời gian thực...';
    }

    state.eventSource = new EventSource('/api/events');

    state.eventSource.onopen = () => {
      state.sseConnected = true;
      state.lastPingTimestamp = Date.now();
      if (el.sseStatus) {
        el.sseStatus.className = 'badge badge-sse sse-connected';
        el.sseStatusText.textContent = 'Kết nối trực tiếp';
      }
    };

    state.eventSource.onerror = () => {
      state.sseConnected = false;
      if (el.sseStatus) {
        el.sseStatus.className = 'badge badge-sse sse-disconnected';
        el.sseStatusText.textContent = 'Đang thử kết nối lại...';
      }
    };

    // Keepalive ping watchdog: reconnect if no ping or message in 35s
    clearInterval(state.pingWatchdogTimer);
    state.pingWatchdogTimer = setInterval(() => {
      if (Date.now() - state.lastPingTimestamp > 35000 && state.sseConnected) {
        initSSE();
      }
    }, 5000);

    // Register Event Listeners
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

    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-running';
      el.activeJobText.textContent = `Đang đồng bộ (${job.job_id.substring(0, 12)}...)`;
    }
    if (el.btnStartJob) el.btnStartJob.disabled = true;
    if (el.btnResumeJob) el.btnResumeJob.disabled = true;
    if (el.btnCancelJob) el.btnCancelJob.classList.remove('hidden');

    if (el.progressBarFill) el.progressBarFill.style.width = '0%';
    if (el.batchProgressText) el.batchProgressText.textContent = `0 / ${state.totalBatches} Đợt hàng (0.0%)`;

    updateSmartStatus(`Đã bắt đầu tiến trình đồng bộ #${job.job_id.substring(0, 8)}. Hệ thống đang nạp các lô hàng...`, 'running');
    showToast(`Bắt đầu đồng bộ danh mục (${state.totalBatches} đợt hàng)`, 'info');
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
    if (el.batchProgressText) el.batchProgressText.textContent = `${batchIndex + 1} / ${totalBatches} Đợt hàng (${pct}%)`;

    if (state.jobStartTime && el.telemetrySpeed && el.telemetryEta) {
      const elapsedSec = (Date.now() - state.jobStartTime) / 1000;
      const totalRecs = state.metrics.added + state.metrics.updated + state.metrics.withdrawn + state.metrics.skipped + state.metrics.deadLetter;
      if (elapsedSec > 0.5) {
        const speed = Math.round(totalRecs / elapsedSec);
        el.telemetrySpeed.textContent = `${speed} bản ghi/giây`;
        const batchesRemaining = totalBatches - (batchIndex + 1);
        if (batchesRemaining > 0 && batchIndex > 0) {
          const avgBatchTime = elapsedSec / (batchIndex + 1);
          const etaSec = Math.round(batchesRemaining * avgBatchTime);
          el.telemetryEta.textContent = `${etaSec} giây`;
        } else {
          el.telemetryEta.textContent = 'Sắp hoàn tất';
        }
      }
    }

    updateSmartStatus(`Đã hoàn tất đợt ${batchIndex + 1}/${totalBatches}: Thêm +${delta.added || 0}, Cập nhật ~${delta.updated || 0}, Ngừng bán -${delta.withdrawn || 0}, Giữ nguyên ↷${delta.skipped || 0}.`, 'running');
  }

  function handleDeadLetter(issue) {
    loadDLQ();
    updateSmartStatus(`Phát hiện bản ghi không hợp lệ trong đợt ${issue.batch_index + 1}. Đã tự động cách ly an toàn vào hàng đợi.`, 'warning');
  }

  function handleJobCompleted(data) {
    stopRuntimeTimer();
    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-completed';
      el.activeJobText.textContent = 'Hoàn tất đồng bộ';
    }
    if (el.progressBarFill) el.progressBarFill.style.width = '100%';
    if (el.batchProgressText) el.batchProgressText.textContent = `${state.totalBatches} / ${state.totalBatches} Đợt hàng (100%)`;
    if (el.btnStartJob) el.btnStartJob.disabled = false;
    if (el.btnCancelJob) el.btnCancelJob.classList.add('hidden');
    if (el.telemetryEta) el.telemetryEta.textContent = 'Đã xong';

    updateSmartStatus('Toàn bộ các đợt hàng đã được đồng bộ thành công vào cơ sở dữ liệu!', 'success');
    showToast('Đồng bộ danh mục thành công!', 'success');
    loadCatalog();
    loadHistory();
    loadInterruptedJobs();
  }

  function handleJobInterrupted(data) {
    stopRuntimeTimer();
    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-interrupted';
      el.activeJobText.textContent = 'Tạm dừng / Gián đoạn';
    }
    if (el.btnStartJob) el.btnStartJob.disabled = false;
    if (el.btnCancelJob) el.btnCancelJob.classList.add('hidden');
    if (el.telemetryEta) el.telemetryEta.textContent = 'Tạm dừng';

    updateSmartStatus(`Tiến trình đã được lưu an toàn tại đợt ${data.committed_batch_index + 1}. Bạn có thể tiếp tục bất cứ lúc nào qua tab "Phục Hồi".`, 'warning');
    showToast('Tiến trình đồng bộ đã dừng và lưu checkpoint an toàn.', 'info');
    loadInterruptedJobs();
    loadHistory();
  }

  // --- Start Sync Job (Non-Tech 1-Click Action) ---
  async function triggerStartJob() {
    if (!el.btnStartJob) return;
    el.btnStartJob.disabled = true;
    const spinner = el.btnStartJob.querySelector('.btn-spinner');
    if (spinner) spinner.classList.remove('hidden');

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
        showToast(`Đã khởi chạy tiến trình ${body.job_id}`, 'success');
      } else if (resp.status === 409) {
        showToast(body.error || 'Một tiến trình khác đang chạy!', 'error');
        el.btnStartJob.disabled = false;
      } else {
        showToast(body.error || 'Không thể bắt đầu tiến trình', 'error');
        el.btnStartJob.disabled = false;
      }
    } catch (err) {
      showToast(`Lỗi mạng: ${err.message}`, 'error');
      el.btnStartJob.disabled = false;
    } finally {
      if (spinner) spinner.classList.add('hidden');
    }
  }

  if (el.btnStartJob) {
    el.btnStartJob.addEventListener('click', triggerStartJob);
  }

  if (el.generatorForm) {
    el.generatorForm.addEventListener('submit', (e) => {
      e.preventDefault();
      triggerStartJob();
    });
  }

  // --- Cancel Running Job Action ---
  if (el.btnCancelJob) {
    el.btnCancelJob.addEventListener('click', async () => {
      if (!state.activeJob || !state.activeJob.job_id) return;
      try {
        const resp = await fetch(`/api/jobs/${encodeURIComponent(state.activeJob.job_id)}/cancel`, {
          method: 'POST',
        });
        if (resp.ok) {
          showToast('Đã gửi yêu cầu dừng tiến trình an toàn...', 'info');
        } else {
          showToast('Không thể dừng tiến trình', 'error');
        }
      } catch (err) {
        showToast(`Lỗi: ${err.message}`, 'error');
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
        el.selectInterruptedJob.innerHTML = '<option value="">Không có đợt nào bị gián đoạn (Hệ thống sạch)</option>';
        if (el.btnResumeJob) el.btnResumeJob.disabled = true;
        if (el.resumeJobDetails) el.resumeJobDetails.classList.add('hidden');
        return;
      }

      el.selectInterruptedJob.innerHTML = '<option value="">-- Chọn đợt đồng bộ dở dang để tiếp tục --</option>';
      jobs.forEach((j) => {
        const opt = document.createElement('option');
        opt.value = j.job_id;
        opt.textContent = `Mã: ${j.job_id.substring(0, 16)}... | Đã ghi: ${j.committed_batch_index + 1}/${j.total_batches} đợt | ${new Date(j.created_at).toLocaleTimeString()}`;
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
            <strong>Mã Tiến Trình:</strong> ${job.job_id}<br>
            <strong>Điểm dừng an toàn:</strong> Đợt ${job.committed_batch_index + 1} / ${job.total_batches}<br>
            <strong>Trạng thái:</strong> Toàn bộ dữ liệu trước điểm dừng đã được commit an toàn vào WAL.
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
          showToast(`Đang tiếp tục tiến trình ${jobID}`, 'success');
        } else {
          showToast(body.error || 'Không thể phục hồi tiến trình', 'error');
          el.btnResumeJob.disabled = false;
        }
      } catch (err) {
        showToast(`Lỗi mạng: ${err.message}`, 'error');
        el.btnResumeJob.disabled = false;
      } finally {
        if (spinner) spinner.classList.add('hidden');
      }
    });
  }

  // --- Manual Custom Batch JSON ---
  if (el.btnLoadSampleJson) {
    el.btnLoadSampleJson.addEventListener('click', () => {
      const sample = [
        {
          sku: 'DESK-OAK-01',
          name: 'Bàn Làm Việc Gỗ Sồi Tối Giản',
          price: 199.99,
          status: 'ACTIVE',
          timestamp: new Date().toISOString(),
        },
        {
          sku: 'CHAIR-ERGONOMIC-02',
          name: 'Ghế Công Thái Học Cao Cấp',
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
        showToast('Vui lòng dán danh sách sản phẩm định dạng JSON', 'error');
        return;
      }
      let records;
      try {
        records = JSON.parse(text);
        if (!Array.isArray(records)) {
          throw new Error('Dữ liệu JSON phải là một mảng [] danh sách sản phẩm');
        }
      } catch (err) {
        if (el.jsonParseStatus) {
          el.jsonParseStatus.textContent = `Lỗi JSON: ${err.message}`;
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
          showToast(`Đã nạp lô sản phẩm thủ công: ${body.job_id}`, 'success');
          el.manualJsonInput.value = '';
          if (el.jsonParseStatus) el.jsonParseStatus.textContent = '';
        } else {
          showToast(body.error || 'Lỗi gửi lô sản phẩm', 'error');
        }
      } catch (err) {
        showToast(`Lỗi mạng: ${err.message}`, 'error');
      }
    });
  }

  // --- Explorer: Dead-Letter Queue (Bản ghi lỗi cần kiểm tra) ---
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
        el.dlqTableBody.innerHTML = '<tr><td colspan="6" class="table-empty">Tuyệt vời! Không có bản ghi nào bị lỗi hoặc cách ly.</td></tr>';
        if (el.dlqPaginationInfo) el.dlqPaginationInfo.textContent = 'Hiển thị 0-0 trên 0';
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
          <td>Đợt ${issue.batch_index + 1}</td>
          <td class="sku-cell">${issue.sku ? issue.sku : '<span class="defect-badge">[THIẾU SKU]</span>'}</td>
          <td><span class="defect-badge">${issue.reason}</span></td>
          <td>${timeStr}</td>
          <td><button class="btn btn-xs btn-outline btn-inspect" data-issue='${JSON.stringify(issue).replace(/'/g, '&apos;')}'>Xem Chi Tiết</button></td>
        `;
        el.dlqTableBody.appendChild(tr);
      });

      const from = state.dlq.offset + 1;
      const to = Math.min(state.dlq.offset + state.dlq.limit, state.dlq.total);
      if (el.dlqPaginationInfo) el.dlqPaginationInfo.textContent = `Hiển thị ${from}-${to} trên ${state.dlq.total}`;
      const curPage = Math.floor(state.dlq.offset / state.dlq.limit) + 1;
      const totalPages = Math.ceil(state.dlq.total / state.dlq.limit) || 1;
      if (el.dlqPageNum) el.dlqPageNum.textContent = `Trang ${curPage} / ${totalPages}`;

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
    el.modalIssueId.textContent = issue.issue_id;
    el.modalDefectReason.textContent = issue.reason;
    try {
      const parsed = JSON.parse(issue.raw_payload);
      el.modalPayloadCode.textContent = JSON.stringify(parsed, null, 2);
    } catch (_) {
      el.modalPayloadCode.textContent = issue.raw_payload;
    }
    el.payloadModal.classList.remove('hidden');
  }

  function closePayloadModal() {
    if (el.payloadModal) {
      el.payloadModal.classList.add('hidden');
    }
  }

  if (el.btnCloseModal) el.btnCloseModal.addEventListener('click', closePayloadModal);
  if (el.btnDismissModal) el.btnDismissModal.addEventListener('click', closePayloadModal);
  if (el.payloadModal) {
    el.payloadModal.addEventListener('click', (e) => {
      if (e.target === el.payloadModal) closePayloadModal();
    });
  }
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && el.payloadModal && !el.payloadModal.classList.contains('hidden')) {
      closePayloadModal();
    }
  });

  if (el.btnCopyPayload) {
    el.btnCopyPayload.addEventListener('click', () => {
      const code = el.modalPayloadCode.textContent;
      navigator.clipboard.writeText(code).then(() => {
        showToast('Đã sao chép nội dung JSON vào khay nhớ tạm!', 'info');
      });
    });
  }

  // --- Explorer: Catalog Products Table (Danh Mục Sản Phẩm) ---
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
        el.catalogTableBody.innerHTML = '<tr><td colspan="6" class="table-empty">Chưa có sản phẩm nào phù hợp với bộ lọc.</td></tr>';
        if (el.catalogPaginationInfo) el.catalogPaginationInfo.textContent = 'Hiển thị 0-0 trên 0';
        if (el.btnCatalogPrev) el.btnCatalogPrev.disabled = true;
        if (el.btnCatalogNext) el.btnCatalogNext.disabled = true;
        return;
      }

      products.forEach((p) => {
        const tr = document.createElement('tr');
        const priceStr = `$${Number(p.price).toFixed(2)}`;
        const supTimeStr = new Date(p.supplier_timestamp).toLocaleTimeString();
        const updTimeStr = new Date(p.updated_at).toLocaleTimeString();
        const statusLabel = p.status === 'ACTIVE' ? 'ĐANG BÁN' : 'NGỪNG BÁN';

        tr.innerHTML = `
          <td class="sku-cell">${p.sku}</td>
          <td>${p.name}</td>
          <td><strong>${priceStr}</strong></td>
          <td><span class="status-badge ${p.status}">${statusLabel}</span></td>
          <td>${supTimeStr}</td>
          <td>${updTimeStr}</td>
        `;
        el.catalogTableBody.appendChild(tr);
      });

      const from = state.catalog.offset + 1;
      const to = Math.min(state.catalog.offset + state.catalog.limit, state.catalog.total);
      if (el.catalogPaginationInfo) el.catalogPaginationInfo.textContent = `Hiển thị ${from}-${to} trên ${state.catalog.total.toLocaleString()} sản phẩm`;
      const curPage = Math.floor(state.catalog.offset / state.catalog.limit) + 1;
      const totalPages = Math.ceil(state.catalog.total / state.catalog.limit) || 1;
      if (el.catalogPageNum) el.catalogPageNum.textContent = `Trang ${curPage} / ${totalPages}`;

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

  // --- Explorer: History Table (Lịch Sử Các Đợt Đồng Bộ) ---
  async function loadHistory() {
    if (!el.historyTableBody) return;
    try {
      const resp = await fetch('/api/jobs?limit=20&offset=0');
      if (!resp.ok) return;
      const data = await resp.json();
      const jobs = data.jobs || [];

      el.historyTableBody.innerHTML = '';
      if (jobs.length === 0) {
        el.historyTableBody.innerHTML = '<tr><td colspan="8" class="table-empty">Chưa có lịch sử đồng bộ nào.</td></tr>';
        return;
      }

      jobs.forEach((j) => {
        const tr = document.createElement('tr');
        const s = j.summary || {};
        const timeStr = new Date(j.created_at).toLocaleString();
        let statusBadge = j.status;
        if (j.status === 'COMPLETED') statusBadge = 'HOÀN THÀNH';
        else if (j.status === 'RUNNING') statusBadge = 'ĐANG CHẠY';
        else if (j.status === 'INTERRUPTED') statusBadge = 'GIÁN ĐOẠN';

        tr.innerHTML = `
          <td><span class="code-badge">${j.job_id.substring(0, 16)}...</span></td>
          <td><span class="status-badge ${j.status}">${statusBadge}</span></td>
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
    applyTheme(state.theme);
    applyPreset('default');
    renderDonutChart();
    updateHealthGauge();
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
