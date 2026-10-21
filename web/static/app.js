/**
 * Coderpush Catalog Sync Engine — Single-Page Dashboard
 * Zero external dependencies: pure vanilla ES6+ JavaScript.
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
    autoScrollLogs: true,
    theme: localStorage.getItem('cp_theme') || (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'),

    // Live Metrics
    metrics: { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 },
    lastDeltas: { added: 0, updated: 0, withdrawn: 0, skipped: 0, deadLetter: 0 },

    // Telemetry
    jobStartTime: null,
    runtimeTimer: null,
    lastCommittedBatch: -1,
    totalBatches: 0,
    recordsProcessed: 0,

    // Explorers
    dlq: { items: [], total: 0, limit: 10, offset: 0, filterJob: '' },
    catalog: { items: [], total: 0, limit: 10, offset: 0, search: '', status: '' },
    history: { jobs: [] },
  };

  // --- Preset Configurations ---
  const PRESETS = {
    default: { seed: 42, batches: 10, size: 100, update: 20, dup: 5, ooo: 10, with: 5, inv: 5 },
    clean:   { seed: 101, batches: 10, size: 100, update: 20, dup: 0, ooo: 0, with: 0, inv: 0 },
    stress:  { seed: 999, batches: 15, size: 150, update: 15, dup: 10, ooo: 15, with: 10, inv: 15 },
    large:   { seed: 777, batches: 50, size: 200, update: 20, dup: 5, ooo: 10, with: 5, inv: 5 },
  };

  // --- DOM Element References ---
  const el = {
    themeToggle: document.getElementById('theme-toggle'),
    themeIcon: document.getElementById('theme-icon'),
    activeJobBadge: document.getElementById('active-job-badge'),
    activeJobText: document.getElementById('active-job-text'),
    sseStatus: document.getElementById('sse-status'),
    sseStatusText: document.getElementById('sse-status-text'),

    // Control Form
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
    btnStartJob: document.getElementById('btn-start-job'),
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

    // Progress & Telemetry
    progressBarFill: document.getElementById('progress-bar-fill'),
    batchProgressText: document.getElementById('batch-progress-text'),
    telemetrySpeed: document.getElementById('telemetry-speed'),
    telemetryEta: document.getElementById('telemetry-eta'),
    telemetryRuntime: document.getElementById('telemetry-runtime'),
    eventLogContainer: document.getElementById('event-log-container'),
    checkAutoscroll: document.getElementById('check-autoscroll'),
    btnClearConsole: document.getElementById('btn-clear-console'),

    // Explorers
    countDlqBadge: document.getElementById('count-dlq-badge'),
    countCatalogBadge: document.getElementById('count-catalog-badge'),
    filterDlqJob: document.getElementById('filter-dlq-job'),
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

  // --- Ratio Validator ---
  function updateRatios() {
    if (!el.inputUpdateRatio) return;
    const updateVal = parseInt(el.inputUpdateRatio.value, 10);
    const dupVal = parseInt(el.inputDupRatio.value, 10);
    const oooVal = parseInt(el.inputOooRatio.value, 10);
    const withVal = parseInt(el.inputWithdrawnRatio.value, 10);
    const invVal = parseInt(el.inputInvalidRatio.value, 10);

    el.valUpdateRatio.textContent = `${updateVal}%`;
    el.valDupRatio.textContent = `${dupVal}%`;
    el.valOooRatio.textContent = `${oooVal}%`;
    el.valWithdrawnRatio.textContent = `${withVal}%`;
    el.valInvalidRatio.textContent = `${invVal}%`;

    const total = updateVal + dupVal + oooVal + withVal + invVal;
    el.totalRatioVal.textContent = `${total}%`;

    if (total > 100) {
      el.ratioValidator.className = 'ratio-validator invalid';
      el.btnStartJob.disabled = true;
      el.totalRatioVal.textContent = `${total}% (EXCEEDS 100%)`;
    } else {
      el.ratioValidator.className = 'ratio-validator valid';
      if (!state.activeJob || state.activeJob.status !== 'RUNNING') {
        el.btnStartJob.disabled = false;
      }
    }
  }

  [el.inputUpdateRatio, el.inputDupRatio, el.inputOooRatio, el.inputWithdrawnRatio, el.inputInvalidRatio].forEach((input) => {
    if (input) input.addEventListener('input', updateRatios);
  });

  // --- Preset Applicator ---
  function applyPreset(key) {
    const p = PRESETS[key];
    if (!p) return;
    el.inputSeed.value = p.seed;
    el.inputBatches.value = p.batches;
    el.inputBatchSize.value = p.size;
    el.inputUpdateRatio.value = p.update;
    el.inputDupRatio.value = p.dup;
    el.inputOooRatio.value = p.ooo;
    el.inputWithdrawnRatio.value = p.with;
    el.inputInvalidRatio.value = p.inv;
    updateRatios();
  }

  document.querySelectorAll('.preset-btn').forEach((btn) => {
    btn.addEventListener('click', () => applyPreset(btn.getAttribute('data-preset')));
  });

  if (el.btnRandomSeed) {
    el.btnRandomSeed.addEventListener('click', () => {
      el.inputSeed.value = Math.floor(Math.random() * 1000000);
    });
  }

  if (el.btnResetForm) {
    el.btnResetForm.addEventListener('click', () => {
      applyPreset('default');
    });
  }

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

  // --- Log Stream Appender ---
  function appendLog(text, category = 'system') {
    if (!el.eventLogContainer) return;
    const now = new Date();
    const timeStr = now.toTimeString().split(' ')[0] + '.' + String(now.getMilliseconds()).padStart(3, '0');
    const entry = document.createElement('div');
    entry.className = `log-entry log-${category}`;
    entry.textContent = `[${timeStr}] ${text}`;
    el.eventLogContainer.appendChild(entry);

    // Limit log size to 150 items
    while (el.eventLogContainer.children.length > 150) {
      el.eventLogContainer.removeChild(el.eventLogContainer.firstChild);
    }

    if (state.autoScrollLogs) {
      el.eventLogContainer.scrollTop = el.eventLogContainer.scrollHeight;
    }
  }

  if (el.checkAutoscroll) {
    el.checkAutoscroll.addEventListener('change', (e) => {
      state.autoScrollLogs = e.target.checked;
    });
  }

  if (el.btnClearConsole) {
    el.btnClearConsole.addEventListener('click', () => {
      el.eventLogContainer.innerHTML = '';
      appendLog('Log cleared by user.', 'system');
    });
  }

  // --- SSE Event Stream Connection ---
  function initSSE() {
    if (state.eventSource) {
      state.eventSource.close();
    }

    if (el.sseStatus) {
      el.sseStatus.className = 'badge badge-sse sse-connecting';
      el.sseStatusText.textContent = 'Connecting SSE...';
    }

    state.eventSource = new EventSource('/api/events');

    state.eventSource.onopen = () => {
      state.sseConnected = true;
      state.lastPingTimestamp = Date.now();
      if (el.sseStatus) {
        el.sseStatus.className = 'badge badge-sse sse-connected';
        el.sseStatusText.textContent = 'Live SSE Connected';
      }
      appendLog('Connected to Server-Sent Events stream.', 'system');
    };

    state.eventSource.onerror = () => {
      state.sseConnected = false;
      if (el.sseStatus) {
        el.sseStatus.className = 'badge badge-sse sse-disconnected';
        el.sseStatusText.textContent = 'Reconnecting...';
      }
    };

    // Keepalive ping watchdog: trigger reconnect if no ping or message in 35s
    clearInterval(state.pingWatchdogTimer);
    state.pingWatchdogTimer = setInterval(() => {
      if (Date.now() - state.lastPingTimestamp > 35000 && state.sseConnected) {
        appendLog('Keepalive timeout. Forcing SSE reconnection...', 'system');
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
    }, 1000);
  }

  function stopRuntimeTimer() {
    clearInterval(state.runtimeTimer);
    state.runtimeTimer = null;
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
      el.activeJobText.textContent = `Job #${job.job_id} (RUNNING)`;
    }
    if (el.btnStartJob) el.btnStartJob.disabled = true;
    if (el.btnResumeJob) el.btnResumeJob.disabled = true;
    if (el.progressBarFill) el.progressBarFill.style.width = '0%';
    if (el.batchProgressText) el.batchProgressText.textContent = `0 / ${state.totalBatches} Batches (0.0%)`;

    appendLog(`Job #${job.job_id} started: ${job.total_batches} total batches.`, 'start');
    showToast(`Job ${job.job_id} started`, 'info');
  }

  function handleProgress(data) {
    state.lastPingTimestamp = Date.now();
    const batchIndex = data.batch_index;
    const totalBatches = data.total_batches || state.totalBatches;
    const summary = data.summary || data.cumulative_summary || {};
    const delta = data.batch_delta || {};

    state.lastCommittedBatch = batchIndex;
    state.totalBatches = totalBatches;

    // Update state metrics
    if (summary.added !== undefined) state.metrics.added = summary.added;
    if (summary.updated !== undefined) state.metrics.updated = summary.updated;
    if (summary.withdrawn !== undefined) state.metrics.withdrawn = summary.withdrawn;
    if (summary.skipped !== undefined) state.metrics.skipped = summary.skipped;
    if (summary.dead_letter !== undefined) state.metrics.deadLetter = summary.dead_letter;

    // Update deltas
    state.lastDeltas = delta;

    updateMetricCards();

    // Calculate percentage
    const pct = totalBatches > 0 ? (((batchIndex + 1) / totalBatches) * 100).toFixed(1) : '0.0';
    if (el.progressBarFill) el.progressBarFill.style.width = `${pct}%`;
    if (el.batchProgressText) el.batchProgressText.textContent = `${batchIndex + 1} / ${totalBatches} Batches (${pct}%)`;

    // Calculate throughput & ETA
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
          el.telemetryEta.textContent = '0s';
        }
      }
    }

    appendLog(`Batch ${batchIndex} committed: +${delta.added || 0} added, ~${delta.updated || 0} updated, -${delta.withdrawn || 0} withdrawn, ↷${delta.skipped || 0} skipped, ⚠${delta.dead_letter || 0} DLQ.`, 'progress');
  }

  function handleDeadLetter(issue) {
    appendLog(`[DLQ QUARANTINE] Job #${issue.job_id} Batch ${issue.batch_index} SKU='${issue.sku || 'EMPTY'}': ${issue.reason}`, 'deadletter');
    loadDLQ();
  }

  function handleJobCompleted(data) {
    stopRuntimeTimer();
    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-completed';
      el.activeJobText.textContent = `Job #${data.job_id} (COMPLETED)`;
    }
    if (el.progressBarFill) el.progressBarFill.style.width = '100%';
    if (el.batchProgressText) el.batchProgressText.textContent = `${state.totalBatches} / ${state.totalBatches} Batches (100%)`;
    if (el.btnStartJob) el.btnStartJob.disabled = false;
    if (el.telemetryEta) el.telemetryEta.textContent = 'Done';

    appendLog(`Job #${data.job_id} COMPLETED successfully.`, 'completed');
    showToast(`Job ${data.job_id} completed successfully!`, 'success');
    loadCatalog();
    loadHistory();
    loadInterruptedJobs();
  }

  function handleJobInterrupted(data) {
    stopRuntimeTimer();
    if (el.activeJobBadge) {
      el.activeJobBadge.className = 'badge badge-job status-interrupted';
      el.activeJobText.textContent = `Job #${data.job_id} (INTERRUPTED)`;
    }
    if (el.btnStartJob) el.btnStartJob.disabled = false;
    if (el.telemetryEta) el.telemetryEta.textContent = 'Interrupted';

    appendLog(`Job #${data.job_id} INTERRUPTED at batch index ${data.committed_batch_index}. Reason: ${data.reason || 'interrupted'}`, 'interrupted');
    showToast(`Job ${data.job_id} was interrupted!`, 'error');
    loadInterruptedJobs();
    loadHistory();
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
      el.countDlqBadge.textContent = state.metrics.deadLetter;
    }
  }

  // --- REST Actions: Start Sync Job ---
  if (el.generatorForm) {
    el.generatorForm.addEventListener('submit', async (e) => {
      e.preventDefault();
      const payload = {
        mode: 'generator',
        seed: parseInt(el.inputSeed.value, 10),
        total_batches: parseInt(el.inputBatches.value, 10),
        batch_size: parseInt(el.inputBatchSize.value, 10),
        update_ratio: parseFloat(el.inputUpdateRatio.value) / 100.0,
        duplicate_ratio: parseFloat(el.inputDupRatio.value) / 100.0,
        out_of_order_ratio: parseFloat(el.inputOooRatio.value) / 100.0,
        withdrawn_ratio: parseFloat(el.inputWithdrawnRatio.value) / 100.0,
        invalid_ratio: parseFloat(el.inputInvalidRatio.value) / 100.0,
      };

      el.btnStartJob.disabled = true;
      const spinner = el.btnStartJob.querySelector('.btn-spinner');
      if (spinner) spinner.classList.remove('hidden');

      try {
        const resp = await fetch('/api/jobs', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });

        const body = await resp.json();
        if (resp.status === 202 || resp.status === 200) {
          showToast(`Job started: ${body.job_id}`, 'success');
        } else if (resp.status === 409) {
          showToast(body.error || 'A job is already running!', 'error');
          el.btnStartJob.disabled = false;
        } else {
          showToast(body.error || 'Failed to start job', 'error');
          el.btnStartJob.disabled = false;
        }
      } catch (err) {
        showToast(`Network error: ${err.message}`, 'error');
        el.btnStartJob.disabled = false;
      } finally {
        if (spinner) spinner.classList.add('hidden');
      }
    });
  }

  // --- REST Actions: Resume Interrupted Job ---
  async function loadInterruptedJobs() {
    if (!el.selectInterruptedJob) return;
    try {
      const resp = await fetch('/api/jobs?status=INTERRUPTED&limit=50');
      if (!resp.ok) return;
      const data = await resp.json();
      const jobs = data.jobs || [];

      el.selectInterruptedJob.innerHTML = '';
      if (jobs.length === 0) {
        el.selectInterruptedJob.innerHTML = '<option value="">No interrupted jobs found</option>';
        if (el.btnResumeJob) el.btnResumeJob.disabled = true;
        if (el.resumeJobDetails) el.resumeJobDetails.classList.add('hidden');
        return;
      }

      el.selectInterruptedJob.innerHTML = '<option value="">-- Select an interrupted job --</option>';
      jobs.forEach((j) => {
        const opt = document.createElement('option');
        opt.value = j.job_id;
        opt.textContent = `${j.job_id} | Committed: ${j.committed_batch_index + 1}/${j.total_batches} | ${new Date(j.created_at).toLocaleTimeString()}`;
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
          <div class="job-meta-box">
            <strong>Target Job:</strong> ${job.job_id} | 
            <strong>Last Committed Batch:</strong> ${job.committed_batch_index} | 
            <strong>Total Batches:</strong> ${job.total_batches}
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

  // --- REST Actions: Manual Custom Batch JSON ---
  if (el.btnLoadSampleJson) {
    el.btnLoadSampleJson.addEventListener('click', () => {
      const sample = [
        {
          sku: 'CUSTOM-KEYBOARD-01',
          name: 'Wireless Mechanical Keyboard',
          price: 99.50,
          status: 'ACTIVE',
          timestamp: new Date().toISOString(),
        },
        {
          sku: 'CUSTOM-MOUSE-02',
          name: 'Ergonomic Precision Mouse',
          price: 49.00,
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
      el.jsonParseStatus.textContent = '';
    });
  }

  if (el.btnSubmitManual) {
    el.btnSubmitManual.addEventListener('click', async () => {
      const text = el.manualJsonInput.value.trim();
      if (!text) {
        showToast('Please enter a valid JSON array of records', 'error');
        return;
      }
      let records;
      try {
        records = JSON.parse(text);
        if (!Array.isArray(records)) {
          throw new Error('Payload must be a JSON array of records');
        }
      } catch (err) {
        el.jsonParseStatus.textContent = `Invalid JSON: ${err.message}`;
        el.jsonParseStatus.style.color = 'var(--color-deadletter)';
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
          showToast(`Custom batch job started: ${body.job_id}`, 'success');
          el.manualJsonInput.value = '';
          el.jsonParseStatus.textContent = '';
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
    const jobID = el.filterDlqJob ? el.filterDlqJob.value.trim() : '';
    const reason = el.filterDlqReason ? el.filterDlqReason.value.trim() : '';
    let url = `/api/dead-letter?limit=${state.dlq.limit}&offset=${state.dlq.offset}`;
    if (jobID) url += `&job_id=${encodeURIComponent(jobID)}`;
    if (reason) url += `&reason=${encodeURIComponent(reason)}`;

    try {
      const resp = await fetch(url);
      if (!resp.ok) return;
      const data = await resp.json();
      const issues = data.issues || [];
      state.dlq.total = data.total || 0;
      if (el.countDlqBadge) el.countDlqBadge.textContent = state.dlq.total;

      el.dlqTableBody.innerHTML = '';
      if (issues.length === 0) {
        el.dlqTableBody.innerHTML = '<tr><td colspan="7" class="table-empty">No corrupt records in Dead-Letter Queue.</td></tr>';
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
          <td><span class="code-tag" title="${issue.issue_id}">${shortIssueId}</span></td>
          <td>${issue.job_id}</td>
          <td>${issue.batch_index}</td>
          <td class="sku-cell">${issue.sku ? issue.sku : '<span class="defect-badge">[EMPTY]</span>'}</td>
          <td><span class="defect-badge">${issue.reason}</span></td>
          <td>${timeStr}</td>
          <td><button class="btn btn-xs btn-outline btn-inspect" data-issue='${JSON.stringify(issue).replace(/'/g, '&apos;')}'>Inspect</button></td>
        `;
        el.dlqTableBody.appendChild(tr);
      });

      // Pagination
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
  if (el.filterDlqJob) {
    el.filterDlqJob.addEventListener('change', () => {
      state.dlq.offset = 0;
      loadDLQ();
    });
  }
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
        showToast('Payload copied to clipboard!', 'info');
      });
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
        el.historyTableBody.innerHTML = '<tr><td colspan="9" class="table-empty">No sync jobs recorded.</td></tr>';
        return;
      }

      jobs.forEach((j) => {
        const tr = document.createElement('tr');
        const s = j.summary || {};
        const timeStr = new Date(j.created_at).toLocaleString();

        tr.innerHTML = `
          <td><span class="code-tag">${j.job_id}</span></td>
          <td><span class="status-badge ${j.status}">${j.status}</span></td>
          <td>${j.committed_batch_index + 1} / ${j.total_batches}</td>
          <td style="color:var(--color-added)">+${s.added || 0}</td>
          <td style="color:var(--color-updated)">~${s.updated || 0}</td>
          <td style="color:var(--color-withdrawn)">-${s.withdrawn || 0}</td>
          <td style="color:var(--color-skipped)">↷${s.skipped || 0}</td>
          <td style="color:var(--color-deadletter)">⚠${s.dead_letter || 0}</td>
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
    updateRatios();
    initSSE();
    loadDLQ();
    loadCatalog();
    loadInterruptedJobs();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }
})();
