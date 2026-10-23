import { test, expect, Page } from '@playwright/test';

async function enterShopWorkspace(page: Page, shopId: string) {
  await page.goto('/');
  await page.evaluate(() => localStorage.clear());
  await page.goto('/');

  await expect(page.getByRole('heading', { name: 'Supplier Catalog' })).toBeVisible();
  const input = page.getByPlaceholder('Tenant ID (e.g. store-1)');
  await expect(input).toBeVisible();
  await input.fill(shopId);
  await page.getByRole('button', { name: 'Enter', exact: true }).click();

  await expect(page).toHaveURL(new RegExp(`shop=${shopId}`));
  await expect(page.getByRole('heading', { name: 'Dashboard & Sync Control' })).toBeVisible();
}

test.describe('Strict End-to-End UI Suite (100% Feature Coverage)', () => {
  test('1. Shop Gatekeeper Validation, Random Tenant Generator, Switch Tenant Modal & Multi-Tenant UI Isolation', async ({
    page,
    request,
  }) => {
    const shop1 = 'pw-ui-tenant-one';
    const shop2 = 'pw-ui-tenant-two';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop1 } });
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop2 } });

    await page.goto('/');
    await page.evaluate(() => localStorage.clear());
    await page.goto('/');

    // 1. Verify Gatekeeper renders and validates empty & invalid Shop IDs
    await expect(page.getByRole('heading', { name: 'Supplier Catalog' })).toBeVisible();
    await expect(page.getByText('Enter a tenant name to access your catalog')).toBeVisible();

    const submitBtn = page.getByRole('button', { name: 'Enter', exact: true });
    await submitBtn.click();
    await expect(page.getByText('Please enter a shop name.')).toBeVisible();

    const shopInput = page.getByPlaceholder('Tenant ID (e.g. store-1)');
    await shopInput.fill('invalid shop!@#');
    await submitBtn.click();
    await expect(
      page.getByText('Lowercase letters, numbers, hyphens or underscores only.')
    ).toBeVisible();

    // 2. Enter shop1 workspace
    await shopInput.fill(shop1);
    await submitBtn.click();
    await expect(page).toHaveURL(new RegExp(`shop=${shop1}`));
    await expect(page.getByRole('heading', { name: 'Dashboard & Sync Control' })).toBeVisible();

    // Seed 1 product into shop1 via API and navigate to Catalog to verify shop1 sees it
    await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop1 },
      data: {
        mode: 'batches',
        job_id: 'job-ui-tenant-1',
        batches: [
          {
            batch_index: 0,
            records: [
              {
                sku: 'SKU-TENANT1-ONLY',
                name: 'Tenant One Exclusive Keyboard',
                price: 149.0,
                status: 'ACTIVE',
                timestamp: '2026-01-01T10:00:00Z',
              },
            ],
          },
        ],
      },
    });

    await page.getByRole('button', { name: /^Catalog/i }).first().click();
    await expect(page.getByRole('heading', { name: 'Live Product Catalog' })).toBeVisible();
    await expect(page.getByText('SKU-TENANT1-ONLY')).toBeVisible();

    // 3. Open Switch Tenant Modal from Sidebar and switch to shop2
    await page.locator('aside').getByRole('button', { name: /Switch/i }).click();
    const dialog = page.getByRole('dialog');
    await expect(dialog.getByText('Switch Tenant')).toBeVisible();

    // Test Random button inside Switch Tenant modal
    await dialog.getByTitle('Generate random name').click();
    const modalInput = dialog.locator('#shop-id');
    const generatedModalVal = await modalInput.inputValue();
    expect(generatedModalVal.length).toBeGreaterThan(3);

    // Test validation inside Switch Tenant modal
    await modalInput.fill('bad tenant!');
    await dialog.getByRole('button', { name: 'Switch', exact: true }).click();
    await expect(
      dialog.getByText('Lowercase letters, numbers, hyphens or underscores only.')
    ).toBeVisible();

    // Switch to shop2
    await modalInput.fill(shop2);
    await dialog.getByRole('button', { name: 'Switch', exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`shop=${shop2}`));

    // Verify shop2 catalog is completely empty (isolated from shop1)
    await expect(
      page.getByText('No products found matching filter criteria. Start a sync or adjust filters.')
    ).toBeVisible();

    // Open Switch Tenant Modal again and click shop1 from "Recent:" quick badges
    await page.locator('aside').getByRole('button', { name: /Switch/i }).click();
    await expect(dialog.getByText('Recent:')).toBeVisible();
    await dialog.getByText(shop1, { exact: true }).click();
    await expect(page).toHaveURL(new RegExp(`shop=${shop1}`));
    await expect(page.getByText('SKU-TENANT1-ONLY')).toBeVisible();
  });

  test('2. Sidebar Collapse/Expand, Mobile Navigation Drawer, URL ?page= Routing & Browser History', async ({
    page,
  }) => {
    await enterShopWorkspace(page, 'pw-ui-nav-shop');

    // 1. Navigate across all 4 views and verify URL ?page= and Header titles
    await page.getByRole('button', { name: /^Catalog/i }).first().click();
    await expect(page).toHaveURL(/page=catalog/);
    await expect(page.getByRole('heading', { name: 'Live Product Catalog' })).toBeVisible();

    await page.getByRole('button', { name: /^Quarantine/i }).first().click();
    await expect(page).toHaveURL(/page=attention/);
    await expect(page.getByRole('heading', { name: 'Quarantine (Dead-Letter Queue)' })).toBeVisible();

    await page.getByRole('button', { name: /History & Versions/i }).first().click();
    await expect(page).toHaveURL(/page=history/);
    await expect(page.getByRole('heading', { name: 'Sync History & Version Control' })).toBeVisible();

    await page.getByRole('button', { name: /^Dashboard/i }).first().click();
    await expect(page.getByRole('heading', { name: 'Dashboard & Sync Control' })).toBeVisible();

    // 2. Verify legacy ?page=versions redirects cleanly to History & Versions view
    await page.goto('/?shop=pw-ui-nav-shop&page=versions');
    await expect(page.getByRole('heading', { name: 'Sync History & Version Control' })).toBeVisible();

    // 3. Test browser Back / Forward navigation (popstate listener)
    await page.goBack();
    await expect(page.getByRole('heading', { name: 'Dashboard & Sync Control' })).toBeVisible();
    await page.goForward();
    await expect(page.getByRole('heading', { name: 'Sync History & Version Control' })).toBeVisible();

    // 4. Test Desktop Sidebar Collapse and Expand
    const collapseBtn = page.getByRole('button', { name: 'Collapse sidebar' });
    await expect(collapseBtn).toBeVisible();
    await collapseBtn.click();
    const expandBtn = page.getByRole('button', { name: 'Expand sidebar' });
    await expect(expandBtn).toBeVisible();
    await expandBtn.click();
    await expect(page.getByRole('button', { name: 'Collapse sidebar' })).toBeVisible();

    // 5. Test Mobile Viewport & Mobile Navigation Drawer
    await page.setViewportSize({ width: 390, height: 844 });
    const mobileMenuBtn = page.getByRole('button', { name: 'Open navigation menu' });
    await expect(mobileMenuBtn).toBeVisible();
    await mobileMenuBtn.click();

    // Click Dashboard inside mobile drawer
    await page.getByRole('button', { name: /^Dashboard/i }).last().click();
    await expect(page.getByRole('heading', { name: 'Dashboard & Sync Control' })).toBeVisible();

    // Restore desktop viewport
    await page.setViewportSize({ width: 1440, height: 900 });
  });

  test('3. Dashboard Workloads (v1.0.0 & v1.1.0), Analytics Filters, Charts & Metric Drilldown Modals', async ({
    page,
    request,
  }) => {
    const shop = 'pw-ui-dashboard-shop';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });
    await enterShopWorkspace(page, shop);

    // Verify all 5 default workload scenarios are listed
    await expect(page.getByText('Baseline Catalog Feed')).toBeVisible();
    await expect(page.getByText('Price & Inventory Adjustment')).toBeVisible();
    await expect(page.getByText('Supplier Anomaly Feed (DLQ)')).toBeVisible();
    await expect(page.getByText('High-Volume Restock Stream')).toBeVisible();
    await expect(page.getByText('Mid-Feed Crash & Checkpoint Recovery')).toBeVisible();

    // 1. Trigger v1.0.0 — Baseline Catalog Feed from UI
    await page.getByTitle('Sync Baseline Catalog Feed').click();

    // Wait for v1.0.0 to finish and show "Synced"
    await expect(page.getByText('Synced').first()).toBeVisible({ timeout: 25000 });

    // Verify Contextual Action Bars appear on Overview
    await expect(page.getByText(/Quarantined Supplier Record/i)).toBeVisible();
    await expect(page.getByText(/Catalog Milestone Version/i)).toBeVisible();

    // 2. Trigger v1.1.0 — Price & Inventory Adjustment
    await page.getByTitle('Sync Price & Inventory Adjustment').click();
    await expect(page.getByText('Synced').nth(1)).toBeVisible({ timeout: 25000 });

    // 2b. Trigger v1.2.0 — Supplier Anomaly Feed (DLQ) so time_series has 3 jobs (>2 points) enabling chart zoom
    await page.getByTitle('Sync Supplier Anomaly Feed (DLQ)').click();
    await expect(page.getByText('Synced').nth(2)).toBeVisible({ timeout: 25000 });

    // 3. Verify Analytics Time Range Filter Pills (Today, Yesterday, Last 7 Days, All Time, Custom)
    await page.getByRole('button', { name: 'Yesterday', exact: true }).click();
    await page.getByRole('button', { name: 'Last 7 Days', exact: true }).click();
    await page.getByRole('button', { name: 'Custom', exact: true }).click();
    await expect(page.getByText('Custom Date Range:')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Apply Range' })).toBeVisible();
    await page.getByRole('button', { name: 'All Time', exact: true }).click();

    // Verify Recharts charts and Zoom controls on Overview
    await expect(page.getByText('Catalog Distribution')).toBeVisible();
    await expect(page.getByText('Product Ingestion & Updates Activity')).toBeVisible();
    const zoomInBtn = page.getByRole('button', { name: 'Zoom In' });
    await expect(zoomInBtn).toBeVisible();
    await zoomInBtn.click();
    await page.getByRole('button', { name: 'Zoom Out' }).click();

    // 4. Click each of the 5 Metric Cards and verify MetricDrilldownModal
    const metricCards = [
      { label: 'Products Added', modalTitle: 'Products Added to Catalog' },
      { label: 'Price / Name Updated', modalTitle: 'Price & Attribute Updated Products' },
      { label: 'Withdrawn Products', modalTitle: 'Withdrawn (Tombstoned) Products' },
      { label: 'Duplicates Skipped', modalTitle: 'Idempotent & Out-of-Order Skipped Records' },
      { label: 'Quarantined (DLQ)', modalTitle: 'Quarantined Dead-Letter Records (DLQ)' },
    ];

    for (const card of metricCards) {
      await page.getByText(card.label, { exact: true }).first().click();
      const dialog = page.getByRole('dialog');
      await expect(dialog.getByText(card.modalTitle)).toBeVisible();
      await dialog.getByRole('button', { name: 'Close' }).first().click();
      await expect(dialog).toBeHidden();
    }
  });

  test('4. Live Catalog Search, Status Filtering, Clear Filters & Pagination', async ({ page }) => {
    const shop = 'pw-ui-dashboard-shop'; // Already populated with ~1000+ products from test 3
    await page.goto(`/?shop=${shop}&page=catalog`);
    await expect(page.getByRole('heading', { name: 'Live Product Catalog' })).toBeVisible();

    // 1. Verify table is populated and pagination works (10 items per page)
    await expect(page.getByText(/Showing 1 to 10 of/i)).toBeVisible();
    const nextBtn = page.getByRole('button', { name: /Next/i });
    await expect(nextBtn).toBeEnabled();
    await nextBtn.click();
    await expect(page.getByText(/Showing 11 to 20 of/i)).toBeVisible();

    const prevBtn = page.getByRole('button', { name: /Prev/i });
    await prevBtn.click();
    await expect(page.getByText(/Showing 1 to 10 of/i)).toBeVisible();

    // 2. Filter by WITHDRAWN only
    const statusSelect = page.locator('select').first();
    await statusSelect.selectOption('WITHDRAWN');
    await expect(page.locator('tbody tr').first()).toContainText('WITHDRAWN');

    // 3. Filter by ACTIVE only
    await statusSelect.selectOption('ACTIVE');
    await expect(page.locator('tbody tr').first()).toContainText('ACTIVE');

    // 4. Search by specific SKU substring
    const firstSku = await page.locator('tbody tr').first().locator('td').first().innerText();
    const searchInput = page.getByPlaceholder('Search SKU or Name...');
    await searchInput.fill(firstSku.trim());
    await expect(page.getByText(/Showing 1 to 1 of 1 products/i)).toBeVisible();
    await expect(page.locator('tbody tr').first()).toContainText(firstSku.trim());

    // 5. Click "Clear" button to reset all catalog filters
    await page.getByRole('button', { name: 'Clear', exact: true }).click();
    await expect(searchInput).toHaveValue('');
    await expect(statusSelect).toHaveValue('');
    await expect(page.getByText(/Showing 1 to 10 of/i)).toBeVisible();
  });

  test('5. Quarantine (DLQ) Reason Filtering, Raw JSON Payload Inspector & Remediation to Live Catalog', async ({
    page,
    request,
  }) => {
    const shop = 'pw-ui-dlq-shop';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });

    // Seed 2 quarantined issues (negative price and empty SKU)
    await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: {
        mode: 'batches',
        job_id: 'job-ui-dlq-seed',
        batches: [
          {
            batch_index: 0,
            records: [
              {
                sku: 'SKU-UI-FIX-ME',
                name: 'Broken Price Desk Chair',
                price: -99.95,
                status: 'ACTIVE',
                timestamp: '2026-01-01T10:00:00Z',
              },
              {
                sku: '',
                name: 'Missing SKU Lamp',
                price: 39.99,
                status: 'ACTIVE',
                timestamp: '2026-01-01T10:00:00Z',
              },
            ],
          },
        ],
      },
    });

    await page.goto(`/?shop=${shop}&page=attention`);
    await expect(page.getByRole('heading', { name: 'Quarantine (Dead-Letter Queue)' })).toBeVisible();
    await expect(page.getByText('Showing 1 to 2 of 2 quarantined records')).toBeVisible();

    // 1. Test Reason Filter Dropdown
    const reasonSelect = page.locator('select').first();
    await reasonSelect.selectOption('NEGATIVE_OR_ZERO_PRICE');
    await expect(page.getByText('Showing 1 to 1 of 1 quarantined records')).toBeVisible();
    await expect(page.getByText('SKU-UI-FIX-ME')).toBeVisible();

    await reasonSelect.selectOption('EMPTY_SKU');
    await expect(page.getByText('Showing 1 to 1 of 1 quarantined records')).toBeVisible();
    await expect(page.getByText('EMPTY SKU', { exact: true })).toBeVisible();

    // Clear filter
    await page.getByRole('button', { name: 'Clear', exact: true }).click();
    await expect(page.getByText('Showing 1 to 2 of 2 quarantined records')).toBeVisible();

    // 2. Open RemediationModal for SKU-UI-FIX-ME
    const targetRow = page.locator('tbody tr', { hasText: 'SKU-UI-FIX-ME' });
    await targetRow.getByRole('button', { name: /Review & Fix/i }).click();

    const modal = page.getByRole('dialog');
    await expect(modal.getByText('Review & Remediate Quarantined Product')).toBeVisible();
    await expect(modal.getByText('Raw Supplier Payload (Defective)')).toBeVisible();
    await expect(modal.locator('pre')).toContainText('-99.95');

    // Fill corrected Name & Price and submit
    await modal.locator('#rem-name').fill('Remediated Ergonomic Desk Chair');
    await modal.locator('#rem-price').fill('189.50');
    await modal.getByRole('button', { name: /Import Remediated Product/i }).click();

    await expect(modal).toBeHidden();
    await expect(page.getByText('Showing 1 to 1 of 1 quarantined records')).toBeVisible();

    // 3. Navigate to Live Catalog and verify the remediated product was imported
    await page.getByRole('button', { name: /^Catalog/i }).first().click();
    await expect(page.getByRole('cell', { name: 'SKU-UI-FIX-ME' })).toBeVisible();
    await expect(page.getByText('Remediated Ergonomic Desk Chair')).toBeVisible();
    await expect(page.getByText('$189.50')).toBeVisible();
  });

  test('6. Version Diff Modal, Git-Revert Rollback with Tombstone Preservation & Reset DB Modal', async ({
    page,
    request,
  }) => {
    const shop = 'pw-ui-history-rollback-shop';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });

    // Seed v1 and v2 via Sync Jobs so we can inspect diffs and execute rollback in the UI
    await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: {
        mode: 'batches',
        job_id: 'job-ui-v1-baseline',
        batches: [
          {
            batch_index: 0,
            records: [
              {
                sku: 'SKU-ROLL-100',
                name: 'Studio Microphone v1',
                price: 120.0,
                status: 'ACTIVE',
                timestamp: '2026-01-01T10:00:00Z',
              },
              {
                sku: 'SKU-ROLL-200',
                name: 'XLR Cable v1',
                price: 25.0,
                status: 'ACTIVE',
                timestamp: '2026-01-01T10:00:00Z',
              },
            ],
          },
        ],
      },
    });
    await new Promise((r) => setTimeout(r, 150));

    await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: {
        mode: 'batches',
        job_id: 'job-ui-v2-update',
        batches: [
          {
            batch_index: 0,
            records: [
              {
                sku: 'SKU-ROLL-100',
                name: 'Studio Microphone v2 Surge',
                price: 299.0,
                status: 'ACTIVE',
                timestamp: '2026-01-02T10:00:00Z',
              },
              {
                sku: 'SKU-ROLL-200',
                name: 'XLR Cable v1',
                price: 25.0,
                status: 'WITHDRAWN',
                timestamp: '2026-01-02T10:00:00Z',
              },
            ],
          },
        ],
      },
    });
    await new Promise((r) => setTimeout(r, 150));

    await page.goto(`/?shop=${shop}&page=history`);
    await expect(page.getByRole('heading', { name: 'Sync History & Version Control' })).toBeVisible();

    // 1. Verify v2 (LIVE) and v1 are displayed in the Unified History & Versions table
    await expect(page.getByText('job-ui-v2-update', { exact: true })).toBeVisible();
    await expect(page.getByText('job-ui-v1-baseline', { exact: true })).toBeVisible();
    await expect(page.getByText('LIVE', { exact: true }).first()).toBeVisible();

    // 2. Click "Diff" on v2 to inspect SKU-level changes in VersionDiffModal
    const v2Row = page.locator('tbody tr', { hasText: 'job-ui-v2-update' });
    await v2Row.getByRole('button', { name: /Diff/i }).click();

    const diffModal = page.getByRole('dialog');
    await expect(diffModal.getByText('Version v2 Diff')).toBeVisible();
    await expect(diffModal.getByText('SKU-ROLL-100')).toBeVisible();
    await expect(diffModal.getByText('Studio Microphone v2 Surge')).toBeVisible();
    await expect(diffModal.getByText('SKU-ROLL-200')).toBeVisible();
    await diffModal.getByRole('button', { name: 'Close' }).first().click();
    await expect(diffModal).toBeHidden();

    // 3. Click "Revert to v1" on job-ui-v1-baseline row
    const v1Row = page.locator('tbody tr', { hasText: 'job-ui-v1-baseline' });
    await v1Row.getByRole('button', { name: /Revert to v1/i }).click();

    const confirmModal = page.getByRole('dialog');
    await expect(confirmModal.getByText('Confirm Catalog Reversion')).toBeVisible();
    await expect(confirmModal.getByText(/Withdrawn items remain withdrawn/i)).toBeVisible();

    const operatorInput = confirmModal.getByPlaceholder('staff-username');
    await operatorInput.fill('playwright-sre-lead');
    await confirmModal.getByRole('button', { name: /Confirm Revert to v1/i }).click();

    await expect(confirmModal).toBeHidden();

    // 4. Verify new ROLLBACK version v3 appears in the timeline and "Rollbacks" filter works
    await expect(page.getByText('ROLLBACK', { exact: true }).first()).toBeVisible();
    await page.getByRole('button', { name: 'Rollbacks', exact: true }).click();
    await expect(page.getByText(/playwright-sre-lead/i)).toBeVisible();
    await page.getByRole('button', { name: /^All/i }).click();

    // 5. Verify in Catalog view that SKU-ROLL-100 reverted to $120.00 while SKU-ROLL-200 remained WITHDRAWN (Tombstone preserved)
    await page.getByRole('button', { name: /^Catalog/i }).first().click();
    const sku100Row = page.locator('tbody tr', { hasText: 'SKU-ROLL-100' });
    await expect(sku100Row).toContainText('Studio Microphone v1');
    await expect(sku100Row).toContainText('$120.00');
    await expect(sku100Row).toContainText('ACTIVE');

    const sku200Row = page.locator('tbody tr', { hasText: 'SKU-ROLL-200' });
    await expect(sku200Row).toContainText('WITHDRAWN');

    // 6. Test Reset DB Confirmation Modal from Header
    await page.getByRole('button', { name: /Reset DB/i }).click();
    const resetModal = page.getByRole('dialog');
    await expect(resetModal.getByText('Reset Tenant Catalog Database?')).toBeVisible();
    // Click Cancel first
    await resetModal.getByRole('button', { name: 'Cancel' }).click();
    await expect(resetModal).toBeHidden();
    await expect(page.getByText('SKU-ROLL-100')).toBeVisible();

    // Now confirm Reset DB
    await page.getByRole('button', { name: /Reset DB/i }).click();
    await page.getByRole('dialog').getByRole('button', { name: 'Confirm Reset Database' }).click();
    await expect(
      page.getByText('No products found matching filter criteria. Start a sync or adjust filters.')
    ).toBeVisible();
  });

  test('7. Interrupted Session Filter & Checkpoint Resumption in UI (v1.4.0 Crash Recovery)', async ({
    page,
    request,
  }) => {
    const shop = 'pw-ui-resume-shop';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });
    await enterShopWorkspace(page, shop);

    // 1. Trigger v1.4.0 — Mid-Feed Crash & Checkpoint Recovery directly from Dashboard
    await page.getByTitle('Sync Mid-Feed Crash & Checkpoint Recovery').click();

    // Wait for v1.4.0 to automatically halt at batch 5/10 and show "Checkpoint Paused" & "Resume Checkpoint"
    await expect(page.getByText('Checkpoint Paused')).toBeVisible({ timeout: 25000 });
    await expect(page.getByRole('button', { name: /Resume Checkpoint/i })).toBeVisible();

    // 2. Navigate to History & Versions and verify the Interrupted filter shows the halted job
    await page.getByRole('button', { name: /History & Versions/i }).first().click();
    await page.getByRole('button', { name: 'Interrupted', exact: true }).click();
    const resumeBtn = page.getByRole('button', { name: /^Resume/i }).first();
    await expect(resumeBtn).toBeVisible();

    // 3. Click Resume in History table and verify the job completes 10/10 batches
    await resumeBtn.click();
    await page.getByRole('button', { name: /^All/i }).click();
    await expect(page.locator('tbody tr').first()).toContainText('COMPLETED', { timeout: 25000 });
    await expect(page.locator('tbody tr').first()).toContainText('10/10');
  });
});
