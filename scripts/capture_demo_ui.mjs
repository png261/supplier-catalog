import { chromium } from '@playwright/test';
import { spawn } from 'child_process';
import fs from 'fs';
import path from 'path';

const PORT = 8094;
const BASE_URL = `http://localhost:${PORT}`;
const OUT_DIR = path.resolve(process.cwd(), 'demo_artifacts/ui_captures');
const DB_PATH = '/tmp/demo_showcase_en.db';

function cleanDb() {
  for (const ext of ['', '-wal', '-shm']) {
    try {
      if (fs.existsSync(DB_PATH + ext)) fs.unlinkSync(DB_PATH + ext);
    } catch {}
  }
}

async function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms));
}

async function main() {
  fs.mkdirSync(OUT_DIR, { recursive: true });
  cleanDb();

  console.log('Starting Go server on port', PORT);
  const srv = spawn('./bin/server', ['-port', String(PORT), '-db', DB_PATH], {
    stdio: 'ignore',
  });

  await sleep(1500);

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1920, height: 1080 },
    deviceScaleFactor: 1,
  });
  const page = await context.newPage();

  const snap = async (name) => {
    const filePath = path.join(OUT_DIR, `${name}.png`);
    await page.screenshot({ path: filePath });
    console.log('Captured:', name);
  };

  try {
    // --- SCENE 1: Gatekeeper & Tenant Validation ---
    await page.goto(BASE_URL, { waitUntil: 'networkidle' });
    await sleep(400);
    await snap('s1_gatekeeper_empty');

    const submitBtn = page.getByRole('button', { name: 'Enter', exact: true });
    const shopInput = page.getByPlaceholder('Tenant ID (e.g. store-1)');
    await shopInput.fill('invalid shop!@#');
    await submitBtn.click();
    await sleep(300);
    await snap('s1_gatekeeper_error');

    await shopInput.fill('store-flagship');
    await sleep(200);
    await snap('s1_gatekeeper_valid');

    await submitBtn.click();
    await page.getByRole('heading', { name: 'Dashboard & Sync Control' }).waitFor();
    await sleep(500);

    // --- SCENE 2: Clean Initial Dashboard ---
    await snap('s2_dashboard_clean');

    // --- SCENE 3: Trigger v1.0.0 Baseline Catalog Feed & Real-Time SSE ---
    await page.getByTitle('Sync Baseline Catalog Feed').click();
    await sleep(250);
    await snap('s3_sync_v100_running');

    await page.getByText('Synced').first().waitFor({ timeout: 25000 });
    await sleep(600);
    await snap('s3_sync_v100_completed');

    // Also open the "Start Update" modal to show custom workload options
    const startUpdateBtn = page.getByRole('button', { name: /Start Update/i }).first();
    if (await startUpdateBtn.isVisible().catch(() => false)) {
      await startUpdateBtn.click();
      await sleep(400);
      await snap('s3_start_update_modal');
      await page.keyboard.press('Escape');
      const closeModalBtn = page.getByRole('dialog').getByRole('button', { name: /Cancel|Close/i }).first();
      if (await closeModalBtn.isVisible().catch(() => false)) {
        await closeModalBtn.click();
      }
      await sleep(300);
    }

    // --- SCENE 4: v1.1.0 Price & Inventory Adjustment + Metric Drilldown + Analytics ---
    await page.getByTitle('Sync Price & Inventory Adjustment').click();
    await sleep(250);
    await snap('s4_sync_v110_running');

    await page.getByText('Synced').nth(1).waitFor({ timeout: 25000 });
    await sleep(600);
    await snap('s4_dashboard_after_v110');

    // Trigger v1.2.0 — Supplier Anomaly Feed (DLQ) so time_series has 3 jobs (>2 points) enabling chart zoom
    await page.getByTitle('Sync Supplier Anomaly Feed (DLQ)').click();
    await page.getByText('Synced').nth(2).waitFor({ timeout: 25000 });
    await sleep(600);
    await snap('s4_dashboard_after_v120');

    // Metric Drilldown: Price / Name Updated
    await page.getByText('Price / Name Updated', { exact: true }).first().click();
    await sleep(500);
    await snap('s4_drilldown_updated');
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
    await sleep(300);

    // Metric Drilldown: Withdrawn Products
    await page.getByText('Withdrawn Products', { exact: true }).first().click();
    await sleep(500);
    await snap('s4_drilldown_withdrawn');
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
    await sleep(300);

    // Metric Drilldown: Duplicates Skipped
    await page.getByText('Duplicates Skipped', { exact: true }).first().click();
    await sleep(500);
    await snap('s4_drilldown_skipped');
    await page.getByRole('dialog').getByRole('button', { name: 'Close' }).first().click();
    await sleep(300);

    // Analytics Time Range Filter & Zoom Controls
    await page.getByRole('button', { name: 'Custom', exact: true }).click();
    await sleep(350);
    await snap('s4_analytics_custom_range');
    await page.getByRole('button', { name: 'All Time', exact: true }).click();
    await sleep(350);
    const zoomInBtn = page.getByRole('button', { name: 'Zoom In' });
    if (await zoomInBtn.isVisible().catch(() => false)) {
      await zoomInBtn.click();
      await sleep(350);
    }
    await snap('s4_analytics_zoomed');
    const zoomOutBtn = page.getByRole('button', { name: 'Zoom Out' });
    if (await zoomOutBtn.isVisible().catch(() => false)) {
      await zoomOutBtn.click();
      await sleep(250);
    }

    // --- SCENE 5: v1.4.0 Mid-Feed Crash & Atomic Checkpoint Resume ---
    await page.getByTitle('Sync Mid-Feed Crash & Checkpoint Recovery').click();
    await page.getByText('Checkpoint Paused').waitFor({ timeout: 25000 });
    await sleep(600);
    await snap('s5_dashboard_checkpoint_paused');

    // Navigate to History & Versions and filter by Interrupted
    await page.getByRole('button', { name: /History & Versions/i }).first().click();
    await sleep(500);
    await page.getByRole('button', { name: 'Interrupted', exact: true }).click();
    await sleep(400);
    await snap('s5_history_interrupted_filter');

    // Click Resume on the interrupted job
    const resumeBtn = page.getByRole('button', { name: /^Resume/i }).first();
    await resumeBtn.click();
    await page.getByRole('button', { name: /^All/i }).click();
    await page.locator('tbody tr').first().getByText('COMPLETED').waitFor({ timeout: 25000 });
    await sleep(600);
    await snap('s5_history_resumed_completed');

    // --- SCENE 6: Live Product Catalog Explorer ---
    await page.getByRole('button', { name: /^Catalog/i }).first().click();
    await page.getByRole('heading', { name: 'Live Product Catalog' }).waitFor();
    await sleep(600);
    await snap('s6_catalog_all_active');

    // Filter by WITHDRAWN status
    const statusSelect = page.locator('select').first();
    await statusSelect.selectOption('WITHDRAWN');
    await sleep(500);
    await snap('s6_catalog_withdrawn_only');

    // Filter by ACTIVE status and search by SKU
    await statusSelect.selectOption('ACTIVE');
    await sleep(400);
    const firstSku = await page.locator('tbody tr').first().locator('td').first().innerText();
    const searchInput = page.getByPlaceholder('Search SKU or Name...');
    await searchInput.fill(firstSku.trim());
    await sleep(500);
    await snap('s6_catalog_sku_search');
    await page.getByRole('button', { name: 'Clear', exact: true }).click();
    await sleep(300);

    // --- SCENE 7: Quarantine (DLQ) & Staff Problem Remediation ---
    await page.getByRole('button', { name: /^Quarantine/i }).first().click();
    await page.getByRole('heading', { name: 'Quarantine (Dead-Letter Queue)' }).waitFor();
    await sleep(600);
    await snap('s7_quarantine_overview');

    // Filter by reason
    const reasonSelect = page.locator('select').first();
    await reasonSelect.selectOption('NEGATIVE_OR_ZERO_PRICE');
    await sleep(450);
    await snap('s7_quarantine_reason_filtered');

    // Open Review & Fix modal on first row
    await page.locator('tbody tr').first().getByRole('button', { name: /Review & Fix/i }).click();
    const remModal = page.getByRole('dialog');
    await remModal.getByText('Review & Remediate Quarantined Product').waitFor();
    await sleep(450);
    await snap('s7_remediation_modal_raw');

    await remModal.locator('#rem-name').fill('Remediated Enterprise Server Rack');
    await remModal.locator('#rem-price').fill('249.99');
    await sleep(300);
    await snap('s7_remediation_modal_corrected');

    await remModal.getByRole('button', { name: /Import Remediated Product/i }).click();
    await sleep(600);
    await snap('s7_quarantine_after_import');

    // --- SCENE 8: Unified History & Versions, Diff Modal & Git-Revert Rollback ---
    await page.getByRole('button', { name: /History & Versions/i }).first().click();
    await page.getByRole('heading', { name: 'Sync History & Version Control' }).waitFor();
    await sleep(600);
    await snap('s8_history_versions_timeline');

    // Click Diff on the top row
    await page.locator('tbody tr').first().getByRole('button', { name: /Diff/i }).click();
    const diffModal = page.getByRole('dialog');
    await sleep(500);
    await snap('s8_version_diff_modal');
    await diffModal.getByRole('button', { name: 'Close' }).first().click();
    await sleep(300);

    // Click Revert Before Sync on the top LIVE row
    const revertBtn = page.locator('tbody tr').first().getByRole('button', { name: /Revert Before Sync/i });
    await revertBtn.click();
    const confirmModal = page.getByRole('dialog');
    await confirmModal.getByText('Confirm Catalog Reversion').waitFor();
    await confirmModal.getByPlaceholder('staff-username').fill('senior-catalog-ops');
    await sleep(400);
    await snap('s8_confirm_revert_modal');

    await confirmModal.getByRole('button', { name: /Confirm Revert/i }).click();
    await sleep(700);
    await snap('s8_history_after_rollback');

    // --- SCENE 9: Multi-Tenant Store Switcher & Reset DB Modal ---
    await page.locator('aside').getByRole('button', { name: /Switch/i }).click();
    const switchDialog = page.getByRole('dialog');
    await switchDialog.getByText('Switch Tenant').waitFor();
    await switchDialog.locator('#shop-id').fill('store-boutique-2');
    await sleep(400);
    await snap('s9_switch_tenant_modal');

    await switchDialog.getByRole('button', { name: 'Switch', exact: true }).click();
    await sleep(600);
    await snap('s9_isolated_second_tenant');

  } finally {
    await browser.close();
    srv.kill('SIGTERM');
    cleanDb();
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
