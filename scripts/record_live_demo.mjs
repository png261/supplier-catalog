import { chromium } from '@playwright/test';
import { spawn, execSync } from 'child_process';
import fs from 'fs';
import path from 'path';

const PORT = 8096;
const BASE_URL = `http://localhost:${PORT}`;
const ROOT_DIR = process.cwd();
const MOSS_DIR = path.resolve(ROOT_DIR, 'demo_artifacts/moss_tts');
const FRAMES_DIR = path.resolve(ROOT_DIR, 'demo_artifacts/live_frames');
const CONCAT_FILE = path.resolve(ROOT_DIR, 'demo_artifacts/frames_concat.txt');
const DB_PATH = '/tmp/demo_live_recording.db';

function cleanDb() {
  const bases = [
    DB_PATH,
    '/tmp/shops/store-flagship.db',
    '/tmp/shops/store-boutique-2.db',
    path.resolve(ROOT_DIR, 'shops/store-flagship.db'),
    path.resolve(ROOT_DIR, 'shops/store-boutique-2.db'),
    path.resolve(ROOT_DIR, 'data/shops/store-flagship.db'),
    path.resolve(ROOT_DIR, 'data/shops/store-boutique-2.db'),
  ];
  for (const base of bases) {
    for (const ext of ['', '-wal', '-shm']) {
      try {
        if (fs.existsSync(base + ext)) fs.unlinkSync(base + ext);
      } catch {}
    }
  }
}

async function sleep(ms) {
  if (ms <= 0) return;
  return new Promise((r) => setTimeout(r, ms));
}

async function main() {
  const manifestPath = path.join(MOSS_DIR, 'manifest.json');
  if (!fs.existsSync(manifestPath)) {
    throw new Error(`Missing MOSS-TTS manifest at ${manifestPath}`);
  }
  const manifest = JSON.parse(fs.readFileSync(manifestPath, 'utf-8'));

  fs.rmSync(FRAMES_DIR, { recursive: true, force: true });
  fs.mkdirSync(FRAMES_DIR, { recursive: true });
  try {
    execSync(`lsof -ti :${PORT} | xargs kill -9 2>/dev/null || true`);
  } catch {}
  await sleep(300);
  cleanDb();

  console.log('Starting Go server with SYNC_BATCH_DELAY_MS=500 on port', PORT);
  const srv = spawn('./bin/server', ['-port', String(PORT), '-db', DB_PATH], {
    stdio: 'ignore',
    env: {
      ...process.env,
      SYNC_BATCH_DELAY_MS: '500',
    },
  });

  await sleep(1500);
  // Pre-flight reset both tenants so the demo always starts from a 100% clean database
  for (const shop of ['store-flagship', 'store-boutique-2']) {
    try {
      await fetch(`${BASE_URL}/api/database/reset`, {
        method: 'POST',
        headers: { 'X-Shop-Id': shop },
      });
    } catch {}
  }

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1920, height: 1080 },
    deviceScaleFactor: 1,
  });

  // Inject visible animated DOM cursor + click ripple
  await context.addInitScript(() => {
    const installCursor = () => {
      if (document.getElementById('__demo_cursor')) return;
      const style = document.createElement('style');
      style.textContent = `
        #__demo_cursor {
          position: fixed;
          top: 0;
          left: 0;
          width: 24px;
          height: 24px;
          pointer-events: none;
          z-index: 2147483647;
          transform: translate(-2px, -2px);
        }
        .demo-click-ripple {
          position: fixed;
          width: 34px;
          height: 34px;
          border-radius: 9999px;
          border: 2.5px solid rgba(79, 70, 229, 0.85);
          background: rgba(99, 102, 241, 0.18);
          pointer-events: none;
          z-index: 2147483646;
          transform: translate(-50%, -50%) scale(0.25);
          animation: demoRippleAnim 0.48s cubic-bezier(0.16, 1, 0.3, 1) forwards;
        }
        @keyframes demoRippleAnim {
          to {
            transform: translate(-50%, -50%) scale(1.75);
            opacity: 0;
          }
        }
      `;
      document.head.appendChild(style);
      const cursor = document.createElement('div');
      cursor.id = '__demo_cursor';
      cursor.innerHTML = `
        <svg width="24" height="24" viewBox="0 0 24 24" fill="none">
          <path d="M4 3L11.5 20L14.2 13.2L21 10.5L4 3Z" fill="#4F46E5" stroke="#FFFFFF" stroke-width="2" stroke-linejoin="round"/>
        </svg>
      `;
      document.body.appendChild(cursor);

      window.addEventListener(
        'mousemove',
        (e) => {
          cursor.style.left = e.clientX + 'px';
          cursor.style.top = e.clientY + 'px';
        },
        true
      );
      window.addEventListener(
        'mousedown',
        (e) => {
          const r = document.createElement('div');
          r.className = 'demo-click-ripple';
          r.style.left = e.clientX + 'px';
          r.style.top = e.clientY + 'px';
          document.body.appendChild(r);
          setTimeout(() => r.remove(), 500);
        },
        true
      );
    };
    if (document.readyState === 'loading') {
      document.addEventListener('DOMContentLoaded', installCursor);
    } else {
      installCursor();
    }
    setInterval(installCursor, 250);
  });

  const page = await context.newPage();
  const cdp = await context.newCDPSession(page);

  // Track wall-clock screencast frames per segment so there is 0.00ms audio/video drift
  const allFrames = [];
  const segWindows = {};
  let currentSegId = manifest[0].id;
  let frameCounter = 0;
  let lastSavedMs = 0;
  let lastBase64 = '';

  function saveFrameBuffer(buf, timeMs, segId, force = false) {
    if (!force && timeMs - lastSavedMs < 65) return;
    lastSavedMs = timeMs;
    frameCounter++;
    const fileName = `frame_${String(frameCounter).padStart(6, '0')}.jpg`;
    const filePath = path.join(FRAMES_DIR, fileName);
    try {
      fs.writeFileSync(filePath, buf);
      allFrames.push({
        filePath,
        timeMs,
        segId,
      });
    } catch {}
  }

  cdp.on('Page.screencastFrame', async (event) => {
    const now = Date.now();
    try {
      await cdp.send('Page.screencastFrameAck', { sessionId: event.sessionId });
    } catch {}
    if (event.data === lastBase64) return;
    lastBase64 = event.data;
    saveFrameBuffer(Buffer.from(event.data, 'base64'), now, currentSegId, false);
  });

  await cdp.send('Page.startScreencast', {
    format: 'jpeg',
    quality: 82,
    maxWidth: 1920,
    maxHeight: 1080,
    everyNthFrame: 1,
  });

  let curX = 960;
  let curY = 540;
  let segStartMs = Date.now();

  async function captureBoundaryFrame(segId) {
    try {
      const buf = await page.screenshot({ type: 'jpeg', quality: 85 });
      saveFrameBuffer(buf, Date.now(), segId, true);
    } catch {}
  }

  async function beginSegment(segId) {
    const now = Date.now();
    currentSegId = segId;
    segStartMs = now;
    segWindows[segId] = { startMs: now, endMs: now };
    console.log(`\n=== [${segId}] started ===`);
    await captureBoundaryFrame(segId);
  }

  async function waitUntilSegElapsed(targetElapsedMs) {
    const left = targetElapsedMs - (Date.now() - segStartMs);
    if (left > 0) {
      await sleep(left);
    }
  }

  async function finishSegment(segId) {
    const segInfo = manifest.find((m) => m.id === segId);
    const targetDurMs = segInfo ? segInfo.duration_ms : 12000;
    await waitUntilSegElapsed(targetDurMs);
    await captureBoundaryFrame(segId);
    const endNow = Date.now();
    segWindows[segId].endMs = endNow;
    console.log(`=== [${segId}] completed in ${((endNow - segWindows[segId].startMs) / 1000).toFixed(2)}s (target ${(targetDurMs / 1000).toFixed(2)}s) ===`);
  }

  async function moveTo(locator, durationMs = 450) {
    try {
      const box = await locator.boundingBox();
      if (!box) return;
      const targetX = box.x + box.width / 2;
      const targetY = box.y + box.height / 2;
      const steps = Math.max(8, Math.round(durationMs / 22));
      for (let i = 1; i <= steps; i++) {
        const u = i / steps;
        const ease = u < 0.5 ? 4 * u * u * u : 1 - Math.pow(-2 * u + 2, 3) / 2;
        const nx = curX + (targetX - curX) * ease;
        const ny = curY + (targetY - curY) * ease;
        await page.mouse.move(nx, ny);
        await sleep(durationMs / steps);
      }
      curX = targetX;
      curY = targetY;
    } catch {}
  }

  async function smoothClick(locator, moveMs = 420) {
    await locator.waitFor({ state: 'visible', timeout: 15000 });
    await moveTo(locator, moveMs);
    await sleep(80);
    await locator.click();
  }

  async function typeSlowly(locator, text, charDelayMs = 55) {
    await smoothClick(locator, 350);
    await locator.fill('');
    await sleep(100);
    for (const ch of text) {
      await locator.type(ch, { delay: charDelayMs });
    }
  }

  try {
    // =========================================================================
    // SEGMENT 01: Multi-Tenant Gatekeeper (13.60s)
    // =========================================================================
    await beginSegment('seg_01_gatekeeper');
    await page.goto(BASE_URL, { waitUntil: 'networkidle' });
    await page.mouse.move(curX, curY);

    const shopInput = page.getByPlaceholder('Tenant ID (e.g. store-1)');
    const submitBtn = page.getByRole('button', { name: 'Enter', exact: true });

    await waitUntilSegElapsed(3800);
    await typeSlowly(shopInput, 'invalid shop!@#', 50);
    await smoothClick(submitBtn, 350);

    await waitUntilSegElapsed(8800);
    await typeSlowly(shopInput, 'store-flagship', 55);
    await sleep(300);
    await smoothClick(submitBtn, 380);
    await page.getByRole('heading', { name: 'Dashboard & Sync Control' }).waitFor({ timeout: 10000 });
    await finishSegment('seg_01_gatekeeper');

    // =========================================================================
    // SEGMENT 02: Pre-Sync Diff Preview (Update 1: Baseline Catalog Feed) (13.76s)
    // =========================================================================
    await beginSegment('seg_02_update1_preview');
    const syncBaselineBtn = page.getByTitle('Sync Baseline Catalog Feed');
    await moveTo(syncBaselineBtn, 900);
    await waitUntilSegElapsed(3200);
    await smoothClick(syncBaselineBtn, 300);

    const diffDialog = page.getByRole('dialog');
    const confirmSyncBtn = diffDialog.getByRole('button', { name: /Confirm & Sync/i });
    await confirmSyncBtn.waitFor({ timeout: 15000 });
    await diffDialog.locator('tbody tr').first().waitFor({ timeout: 15000 });

    await waitUntilSegElapsed(6800);
    const addedFilterBtn = diffDialog.getByRole('button', { name: /Added/i }).first();
    if (await addedFilterBtn.isVisible().catch(() => false)) {
      await smoothClick(addedFilterBtn, 380);
    }
    await waitUntilSegElapsed(8800);
    const updatedFilterBtn = diffDialog.getByRole('button', { name: /Updated/i }).first();
    if (await updatedFilterBtn.isVisible().catch(() => false)) {
      await smoothClick(updatedFilterBtn, 380);
    }
    await waitUntilSegElapsed(10800);
    const dlqFilterBtn = diffDialog.getByRole('button', { name: /DLQ/i }).first();
    if (await dlqFilterBtn.isVisible().catch(() => false)) {
      await smoothClick(dlqFilterBtn, 380);
    }
    await waitUntilSegElapsed(12300);
    const allFilterBtn = diffDialog.getByRole('button', { name: /^All/i }).first();
    if (await allFilterBtn.isVisible().catch(() => false)) {
      await smoothClick(allFilterBtn, 350);
    }
    await moveTo(confirmSyncBtn, 400);
    await finishSegment('seg_02_update1_preview');

    // =========================================================================
    // SEGMENT 03: Live Batch Sync & SSE Progress (Update 1) (13.60s)
    // =========================================================================
    await beginSegment('seg_03_update1_sync');
    await smoothClick(confirmSyncBtn, 250);
    await page.getByTitle('Revert Baseline Catalog Feed').waitFor({ timeout: 25000 });
    await sleep(600);
    await moveTo(page.getByText('Products Added', { exact: true }).first(), 500);
    await sleep(900);
    await moveTo(page.getByText('Duplicates Skipped', { exact: true }).first(), 500);
    await sleep(900);
    await moveTo(page.getByText('Quarantined (DLQ)', { exact: true }).first(), 500);
    await finishSegment('seg_03_update1_sync');

    // =========================================================================
    // SEGMENT 04: Mid-Feed Crash & Checkpoint Pause (Update 2) (15.68s)
    // =========================================================================
    await beginSegment('seg_04_update2_crash');
    const syncCrashBtn = page.getByTitle('Sync Mid-Feed Crash & Checkpoint Recovery');
    await moveTo(syncCrashBtn, 700);
    await waitUntilSegElapsed(2600);
    await smoothClick(syncCrashBtn, 300);

    await confirmSyncBtn.waitFor({ timeout: 15000 });
    await diffDialog.locator('tbody tr').first().waitFor({ timeout: 15000 });
    await waitUntilSegElapsed(5400);
    await smoothClick(confirmSyncBtn, 380);

    const resumeDashboardBtn = page.getByRole('button', { name: /Resume Checkpoint/i }).first();
    await resumeDashboardBtn.waitFor({ timeout: 25000 });
    await moveTo(resumeDashboardBtn, 500);
    await finishSegment('seg_04_update2_crash');

    // =========================================================================
    // SEGMENT 05: Resuming Interrupted Update from Checkpoint (12.56s)
    // =========================================================================
    await beginSegment('seg_05_update2_resume');
    await waitUntilSegElapsed(1800);
    await smoothClick(resumeDashboardBtn, 380);
    const revertCrashBtn = page.getByTitle('Revert Mid-Feed Crash & Checkpoint Recovery');
    await revertCrashBtn.waitFor({ timeout: 25000 });
    await moveTo(revertCrashBtn, 500);
    await finishSegment('seg_05_update2_resume');

    // =========================================================================
    // SEGMENT 06: Interactive KPI Drilldowns & Analytics (14.24s)
    // =========================================================================
    await beginSegment('seg_06_drilldown_analytics');
    const updatedCard = page.getByText('Price / Name Updated', { exact: true }).first();
    await waitUntilSegElapsed(1200);
    await smoothClick(updatedCard, 450);
    await waitUntilSegElapsed(5200);
    await smoothClick(page.getByRole('dialog').getByRole('button', { name: 'Close' }).first(), 350);

    const withdrawnCard = page.getByText('Withdrawn Products', { exact: true }).first();
    await waitUntilSegElapsed(6400);
    await smoothClick(withdrawnCard, 450);
    await waitUntilSegElapsed(10400);
    await smoothClick(page.getByRole('dialog').getByRole('button', { name: 'Close' }).first(), 350);

    const allTimeBtn = page.getByRole('button', { name: 'All Time', exact: true });
    if (await allTimeBtn.isVisible().catch(() => false)) {
      await waitUntilSegElapsed(11600);
      await smoothClick(allTimeBtn, 400);
    }
    await finishSegment('seg_06_drilldown_analytics');

    // =========================================================================
    // SEGMENT 07: Live Catalog & Product Modification History (12.16s)
    // =========================================================================
    await beginSegment('seg_07_catalog_history');
    await smoothClick(page.getByRole('button', { name: /^Catalog/i }).first(), 450);
    await page.getByRole('heading', { name: 'Catalog', exact: true }).waitFor();

    const statusSelect = page.locator('select').first();
    await waitUntilSegElapsed(2400);
    await moveTo(statusSelect, 350);
    await statusSelect.selectOption('WITHDRAWN');
    await waitUntilSegElapsed(4200);
    await statusSelect.selectOption('ACTIVE');
    await sleep(400);

    const firstSku = (await page.locator('tbody tr').first().locator('td').first().innerText()).trim();
    const searchInput = page.getByPlaceholder('Search SKU or Name...');
    await waitUntilSegElapsed(5200);
    await typeSlowly(searchInput, firstSku, 40);

    await waitUntilSegElapsed(7600);
    const historyRowBtn = page.locator('tbody tr').first().getByRole('button', { name: /History/i });
    await smoothClick(historyRowBtn, 400);
    await page.getByRole('dialog').getByText('Product Modification History', { exact: true }).waitFor({ timeout: 10000 });
    await waitUntilSegElapsed(11300);
    await smoothClick(page.getByRole('dialog').getByRole('button', { name: 'Close' }).first(), 320);
    await finishSegment('seg_07_catalog_history');

    // =========================================================================
    // SEGMENT 08: Quarantine Inspection & Staff Remediation (15.36s)
    // =========================================================================
    await beginSegment('seg_08_quarantine_remediation');
    await smoothClick(page.getByRole('button', { name: /^Quarantine/i }).first(), 450);
    await page.getByRole('heading', { name: 'Quarantine (Dead-Letter Queue)' }).waitFor();

    const reasonSelect = page.locator('select').first();
    await waitUntilSegElapsed(2500);
    await moveTo(reasonSelect, 350);
    await reasonSelect.selectOption('NEGATIVE_OR_ZERO_PRICE');

    await waitUntilSegElapsed(4200);
    const reviewFixBtn = page.locator('tbody tr').first().getByRole('button', { name: /Review & Fix/i });
    await smoothClick(reviewFixBtn, 420);

    const remModal = page.getByRole('dialog');
    await remModal.getByText('Review & Remediate Quarantined Product').waitFor();

    await waitUntilSegElapsed(7200);
    await typeSlowly(remModal.locator('#rem-name'), 'Enterprise Server Rack Pro', 38);
    await waitUntilSegElapsed(9800);
    await typeSlowly(remModal.locator('#rem-price'), '249.99', 50);

    await waitUntilSegElapsed(12000);
    const importRemBtn = remModal.getByRole('button', { name: /Import Remediated Product/i });
    await smoothClick(importRemBtn, 380);
    await finishSegment('seg_08_quarantine_remediation');

    // =========================================================================
    // SEGMENT 09: Version Diffs & Independent Update Revert (14.08s)
    // =========================================================================
    await beginSegment('seg_09_history_diff_revert');
    await smoothClick(page.getByRole('button', { name: /History & Versions/i }).first(), 420);
    await page.getByRole('heading', { name: 'Sync History & Version Control' }).waitFor();

    await waitUntilSegElapsed(1800);
    const diffRowBtn = page.locator('tbody tr').first().getByRole('button', { name: /Diff/i });
    await smoothClick(diffRowBtn, 380);
    await waitUntilSegElapsed(4200);
    await smoothClick(page.getByRole('dialog').getByRole('button', { name: /Cancel|Close/i }).first(), 320);

    await waitUntilSegElapsed(4900);
    await smoothClick(page.getByRole('button', { name: /^Dashboard/i }).first(), 400);
    await page.getByRole('heading', { name: 'Dashboard & Sync Control' }).waitFor();

    await waitUntilSegElapsed(6200);
    const revertUpdateBtn = page.getByTitle('Revert Mid-Feed Crash & Checkpoint Recovery');
    await smoothClick(revertUpdateBtn, 400);

    const confirmRevertBtn = diffDialog.getByRole('button', { name: /Confirm & Revert/i });
    await confirmRevertBtn.waitFor({ timeout: 15000 });
    await diffDialog.locator('tbody tr').first().waitFor({ timeout: 15000 });
    await waitUntilSegElapsed(9200);
    await smoothClick(confirmRevertBtn, 350);

    await page.getByTitle('Sync Mid-Feed Crash & Checkpoint Recovery').waitFor({ timeout: 25000 });
    await finishSegment('seg_09_history_diff_revert');

    // =========================================================================
    // SEGMENT 10: Multi-Tenant Store Switcher (11.20s)
    // =========================================================================
    await beginSegment('seg_10_tenant_switch');
    await waitUntilSegElapsed(1200);
    const switchBtn = page.locator('aside').getByRole('button', { name: /Switch/i });
    await smoothClick(switchBtn, 450);

    const switchDialog = page.getByRole('dialog');
    await switchDialog.getByText('Switch Tenant').waitFor();

    await waitUntilSegElapsed(3400);
    await typeSlowly(switchDialog.locator('#shop-id'), 'store-boutique-2', 50);
    await waitUntilSegElapsed(6400);
    await smoothClick(switchDialog.getByRole('button', { name: 'Switch', exact: true }), 380);
    await finishSegment('seg_10_tenant_switch');

  } finally {
    try {
      await cdp.send('Page.stopScreencast');
    } catch {}
    await page.close();
    await context.close();
    await browser.close();
    srv.kill('SIGTERM');
    cleanDb();

    // Build exact per-segment ffconcat file so every segment's video duration
    // matches its MOSS-TTS audio duration to 0.000s (zero cumulative drift!)
    const lines = ['ffconcat version 1.0'];
    let lastGlobalFile = allFrames.length > 0 ? allFrames[0].filePath : null;

    for (const seg of manifest) {
      const win = segWindows[seg.id];
      const targetSec = seg.duration_ms / 1000.0;
      const segFrames = allFrames.filter((f) => f.segId === seg.id);

      if (segFrames.length === 0 && lastGlobalFile) {
        lines.push(`file '${lastGlobalFile}'`);
        lines.push(`duration ${targetSec.toFixed(6)}`);
        continue;
      }

      const startMs = win ? win.startMs : segFrames[0].timeMs;
      const endMs = win && win.endMs > startMs ? win.endMs : segFrames[segFrames.length - 1].timeMs + 100;
      const rawTotalMs = Math.max(1, endMs - startMs);

      const entries = [];
      if (lastGlobalFile && segFrames[0].timeMs > startMs) {
        entries.push({
          filePath: lastGlobalFile,
          rawMs: segFrames[0].timeMs - startMs,
        });
      }
      for (let i = 0; i < segFrames.length; i++) {
        const curT = Math.max(startMs, segFrames[i].timeMs);
        const nextT = i + 1 < segFrames.length ? Math.max(curT, segFrames[i + 1].timeMs) : Math.max(curT + 16, endMs);
        entries.push({
          filePath: segFrames[i].filePath,
          rawMs: Math.max(1, nextT - curT),
        });
        lastGlobalFile = segFrames[i].filePath;
      }

      const sumRawMs = entries.reduce((acc, e) => acc + e.rawMs, 0) || rawTotalMs;
      const scale = targetSec / sumRawMs;

      for (const e of entries) {
        const durSec = Math.max(0.004, e.rawMs * scale);
        lines.push(`file '${e.filePath}'`);
        lines.push(`duration ${durSec.toFixed(6)}`);
      }
    }

    if (lastGlobalFile) {
      lines.push(`file '${lastGlobalFile}'`);
    }

    fs.writeFileSync(CONCAT_FILE, lines.join('\n') + '\n', 'utf-8');
    console.log(`Saved ${allFrames.length} screencast frames and zero-drift concat manifest to ${CONCAT_FILE}`);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
