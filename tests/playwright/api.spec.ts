import { test, expect, APIRequestContext } from '@playwright/test';

async function waitForJobStatus(
  request: APIRequestContext,
  jobId: string,
  expectedStatuses: string[],
  shopId = 'default',
  timeoutMs = 15000
): Promise<any> {
  const start = Date.now();
  while (Date.now() - start < timeoutMs) {
    const res = await request.get(`/api/jobs/${encodeURIComponent(jobId)}`, {
      headers: { 'X-Shop-Id': shopId },
    });
    if (res.ok()) {
      const body = await res.json();
      if (body.job && expectedStatuses.includes(body.job.status)) {
        return body;
      }
    }
    await new Promise((r) => setTimeout(r, 40));
  }
  throw new Error(`Timed out waiting for job ${jobId} to reach [${expectedStatuses.join(', ')}]`);
}

test.describe('Strict Backend REST API & Domain Invariant Suite', () => {
  test('1. SPA Root, Swagger OpenAPI Docs, and CORS Preflight Headers', async ({ request }) => {
    const rootRes = await request.get('/');
    expect(rootRes.status()).toBe(200);
    const rootHtml = await rootRes.text();
    expect(rootHtml.toLowerCase()).toContain('<!doctype html>');

    const swaggerDocRes = await request.get('/swagger/doc.json');
    expect(swaggerDocRes.status()).toBe(200);
    const swaggerDoc = await swaggerDocRes.json();
    expect(swaggerDoc.swagger || swaggerDoc.openapi).toBeTruthy();
    expect(swaggerDoc.paths['/api/jobs']).toBeTruthy();
    expect(swaggerDoc.paths['/api/versions']).toBeTruthy();

    const swaggerUiRes = await request.get('/swagger/index.html');
    expect(swaggerUiRes.status()).toBe(200);

    const corsRes = await request.fetch('/api/jobs', { method: 'OPTIONS' });
    expect(corsRes.status()).toBe(204);
    expect(corsRes.headers()['access-control-allow-origin']).toBe('*');
    expect(corsRes.headers()['access-control-allow-headers']).toContain('X-Shop-Id');
  });

  test('2. Multi-Tenant Isolation via X-Shop-Id, ?shop=, ?shop_id= and Invalid Tenant Rejection', async ({ request }) => {
    const shopA = 'pw-api-tenant-alpha';
    const shopB = 'pw-api-tenant-beta';

    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shopA } });
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shopB } });

    // Invalid tenant ID characters must be rejected with 400 INVALID_REQUEST
    const invalidShopRes = await request.get('/api/products', {
      headers: { 'X-Shop-Id': 'invalid shop id!@#' },
    });
    expect(invalidShopRes.status()).toBe(400);
    const invalidErr = await invalidShopRes.json();
    expect(invalidErr.code).toBe('INVALID_REQUEST');

    // Ingest product into shopA via X-Shop-Id header
    const startA = await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shopA },
      data: {
        mode: 'batches',
        job_id: 'job-alpha-1',
        batches: [
          {
            batch_index: 0,
            records: [
              {
                sku: 'SKU-ALPHA-001',
                name: 'Alpha Exclusive Headphones',
                price: 199.99,
                status: 'ACTIVE',
                timestamp: '2026-01-01T10:00:00Z',
              },
            ],
          },
        ],
      },
    });
    expect(startA.status()).toBe(202);
    await waitForJobStatus(request, 'job-alpha-1', ['COMPLETED'], shopA);

    // Query shopA via ?shop= query param
    const prodsA = await request.get(`/api/products?shop=${shopA}`);
    expect(prodsA.status()).toBe(200);
    const prodsAData = await prodsA.json();
    expect(prodsAData.total).toBe(1);
    expect(prodsAData.products[0].sku).toBe('SKU-ALPHA-001');

    // Query shopB via ?shop_id= query param -> must be completely isolated (0 products, 0 jobs, 0 versions)
    const prodsB = await request.get(`/api/products?shop_id=${shopB}`);
    expect(prodsB.status()).toBe(200);
    expect((await prodsB.json()).total).toBe(0);

    const jobsB = await request.get(`/api/jobs?shop=${shopB}`);
    expect((await jobsB.json()).total).toBe(0);

    const versB = await request.get(`/api/versions?shop=${shopB}`);
    expect((await versB.json()).total).toBe(0);
  });

  test('3. Core Sync Engine Invariants A, B, C, D, E in Direct Batches Mode', async ({ request }) => {
    const shop = 'pw-api-invariants';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });

    const jobPayload = {
      mode: 'batches',
      job_id: 'job-invariants-test',
      batches: [
        {
          batch_index: 0,
          records: [
            // 1. Valid initial insert (added = 1)
            {
              sku: 'SKU-INV-1',
              name: 'Baseline Monitor',
              price: 300.0,
              status: 'ACTIVE',
              timestamp: '2026-01-10T10:00:00Z',
            },
            // 2. Invariant A: Exact duplicate record -> skipped (skipped = 1)
            {
              sku: 'SKU-INV-1',
              name: 'Baseline Monitor',
              price: 300.0,
              status: 'ACTIVE',
              timestamp: '2026-01-10T10:00:00Z',
            },
            // 3. Invariant B: Out-of-order / older timestamp -> skipped (skipped = 2)
            {
              sku: 'SKU-INV-1',
              name: 'Stale Monitor Price',
              price: 250.0,
              status: 'ACTIVE',
              timestamp: '2026-01-09T10:00:00Z',
            },
            // 4. Valid forward price update (updated = 1)
            {
              sku: 'SKU-INV-1',
              name: 'Baseline Monitor Pro',
              price: 350.0,
              status: 'ACTIVE',
              timestamp: '2026-01-11T10:00:00Z',
            },
            // 5. Valid new product that will be withdrawn (added = 2)
            {
              sku: 'SKU-TOMBSTONE',
              name: 'Legacy Webcam',
              price: 80.0,
              status: 'ACTIVE',
              timestamp: '2026-01-10T10:00:00Z',
            },
            // 6. Withdraw SKU-TOMBSTONE (withdrawn = 1)
            {
              sku: 'SKU-TOMBSTONE',
              name: 'Legacy Webcam',
              price: 80.0,
              status: 'WITHDRAWN',
              timestamp: '2026-01-11T10:00:00Z',
            },
            // 7. Invariant C: Tombstone Preservation - attempt to reactivate WITHDRAWN product -> skipped (skipped = 3)
            {
              sku: 'SKU-TOMBSTONE',
              name: 'Resurrected Webcam',
              price: 90.0,
              status: 'ACTIVE',
              timestamp: '2026-01-12T10:00:00Z',
            },
            // 8. Invariant D: Dead-Letter Isolation (4 invalid records)
            {
              sku: '',
              name: 'Empty SKU Defect',
              price: 25.0,
              status: 'ACTIVE',
              timestamp: '2026-01-10T10:00:00Z',
            },
            {
              sku: 'SKU-BAD-PRICE',
              name: 'Negative Price Defect',
              price: -19.99,
              status: 'ACTIVE',
              timestamp: '2026-01-10T10:00:00Z',
            },
            {
              sku: 'SKU-BAD-STATUS',
              name: 'Invalid Status Defect',
              price: 19.99,
              status: 'ARCHIVED' as any,
              timestamp: '2026-01-10T10:00:00Z',
            },
            {
              sku: 'SKU-ZERO-TS',
              name: 'Zero Timestamp Defect',
              price: 19.99,
              status: 'ACTIVE',
              timestamp: '0001-01-01T00:00:00Z',
            },
          ],
        },
      ],
    };

    const startRes = await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: jobPayload,
    });
    expect(startRes.status()).toBe(202);

    const detail = await waitForJobStatus(request, 'job-invariants-test', ['COMPLETED'], shop);
    expect(detail.job.summary).toEqual({
      added: 2,
      updated: 1,
      withdrawn: 1,
      skipped: 3,
      dead_letter: 4,
    });

    // Invariant E: Atomic batch checkpoint recorded
    expect(detail.checkpoints).toHaveLength(1);
    expect(detail.checkpoints[0].batch_index).toBe(0);
    expect(detail.checkpoints[0].items_count).toBe(11);
    expect(detail.checkpoints[0].summary).toEqual(detail.job.summary);

    // Verify products table state
    const prodRes = await request.get('/api/products?limit=20', { headers: { 'X-Shop-Id': shop } });
    const prodData = await prodRes.json();
    expect(prodData.total).toBe(2);
    const p1 = prodData.products.find((p: any) => p.sku === 'SKU-INV-1');
    const p2 = prodData.products.find((p: any) => p.sku === 'SKU-TOMBSTONE');
    expect(p1.name).toBe('Baseline Monitor Pro');
    expect(p1.price).toBe(350.0);
    expect(p1.status).toBe('ACTIVE');
    expect(p2.status).toBe('WITHDRAWN');
  });

  test('4. Dead-Letter Quarantine Reason Filtering & Remediation with Invariant Enforcement', async ({ request }) => {
    const shop = 'pw-api-invariants'; // Re-use populated DLQ from test 3

    // Verify all 4 UI reason filter codes return exactly 1 matching defect each
    for (const reasonCode of [
      'EMPTY_SKU',
      'NEGATIVE_OR_ZERO_PRICE',
      'CORRUPTED_JSON',
      'ZERO_OR_MISSING_TIMESTAMP',
    ]) {
      const dlqRes = await request.get(`/api/dead-letter?reason=${reasonCode}`, {
        headers: { 'X-Shop-Id': shop },
      });
      expect(dlqRes.status()).toBe(200);
      const dlqData = await dlqRes.json();
      expect(dlqData.total).toBe(1);
      expect(dlqData.issues).toHaveLength(1);
    }

    // Fetch the negative price defect and remediate it
    const negRes = await request.get('/api/dead-letter?reason=NEGATIVE_OR_ZERO_PRICE', {
      headers: { 'X-Shop-Id': shop },
    });
    const negIssue = (await negRes.json()).issues[0];
    expect(negIssue.sku).toBe('SKU-BAD-PRICE');

    // Validation errors on remediation
    const badPriceRes = await request.post(`/api/dead-letter/${negIssue.issue_id}/resolve`, {
      headers: { 'X-Shop-Id': shop },
      data: { sku: 'SKU-BAD-PRICE', name: 'Still Negative', price: -5, status: 'ACTIVE' },
    });
    expect(badPriceRes.status()).toBe(400);

    // Attempt to resurrect WITHDRAWN SKU-TOMBSTONE via DLQ remediation -> must fail with 409 JOB_CONFLICT / Invariant C
    const resurrectRes = await request.post(`/api/dead-letter/${negIssue.issue_id}/resolve`, {
      headers: { 'X-Shop-Id': shop },
      data: {
        sku: 'SKU-TOMBSTONE',
        name: 'Illegal Resurrection via DLQ',
        price: 99.0,
        status: 'ACTIVE',
        timestamp: '2026-02-01T00:00:00Z',
      },
    });
    expect(resurrectRes.status()).toBe(409);

    // Valid remediation of SKU-BAD-PRICE
    const validResolveRes = await request.post(`/api/dead-letter/${negIssue.issue_id}/resolve`, {
      headers: { 'X-Shop-Id': shop },
      data: {
        sku: 'SKU-BAD-PRICE-FIXED',
        name: 'Remediated Monitor Stand',
        price: 49.99,
        status: 'ACTIVE',
      },
    });
    expect(validResolveRes.status()).toBe(200);
    const resolvedBody = await validResolveRes.json();
    expect(resolvedBody.status).toBe('success');
    expect(resolvedBody.product.sku).toBe('SKU-BAD-PRICE-FIXED');

    // Verify DLQ count decreased from 4 to 3 and product is in live catalog
    const afterDlq = await (await request.get('/api/dead-letter', { headers: { 'X-Shop-Id': shop } })).json();
    expect(afterDlq.total).toBe(3);

    const searchProd = await (
      await request.get('/api/products?search=SKU-BAD-PRICE-FIXED', { headers: { 'X-Shop-Id': shop } })
    ).json();
    expect(searchProd.total).toBe(1);
    expect(searchProd.products[0].price).toBe(49.99);
  });

  test('5. Generator Mode Validation, Mid-Stream Cancellation, and Checkpoint Resumption', async ({ request }) => {
    const shop = 'pw-api-resumability';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });

    // Validation edge cases
    const badRatio = await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: { mode: 'generator', update_ratio: -0.1 },
    });
    expect(badRatio.status()).toBe(400);

    const badSum = await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: { mode: 'generator', update_ratio: 0.7, invalid_ratio: 0.5 },
    });
    expect(badSum.status()).toBe(400);

    // Start a large job (60 batches of 200 items) and cancel it mid-flight
    const startRes = await request.post('/api/jobs', {
      headers: { 'X-Shop-Id': shop },
      data: {
        mode: 'generator',
        job_id: 'job-crash-resume-api',
        seed: 777,
        total_batches: 60,
        batch_size: 200,
        base_time: '2026-01-01T00:00:00Z',
      },
    });
    expect(startRes.status()).toBe(202);

    // Cancel immediately
    const cancelRes = await request.post('/api/jobs/job-crash-resume-api/cancel', {
      headers: { 'X-Shop-Id': shop },
    });
    expect([200, 404]).toContain(cancelRes.status());

    const interruptedDetail = await waitForJobStatus(
      request,
      'job-crash-resume-api',
      ['INTERRUPTED', 'COMPLETED'],
      shop
    );

    if (interruptedDetail.job.status === 'INTERRUPTED') {
      // Resume job from last committed batch checkpoint
      const resumeRes = await request.post('/api/jobs/job-crash-resume-api/resume', {
        headers: { 'X-Shop-Id': shop },
        data: {},
      });
      expect(resumeRes.status()).toBe(202);

      const finalDetail = await waitForJobStatus(request, 'job-crash-resume-api', ['COMPLETED'], shop, 25000);
      expect(finalDetail.job.status).toBe('COMPLETED');
      expect(finalDetail.job.committed_batch_index).toBe(59);
      expect(finalDetail.checkpoints).toHaveLength(60);

      // Resuming an already completed job must return 409 JOB_CONFLICT
      const resumeAgain = await request.post('/api/jobs/job-crash-resume-api/resume', {
        headers: { 'X-Shop-Id': shop },
      });
      expect(resumeAgain.status()).toBe(409);
    }
  });

  test('6. Patches Preview (Dry-Run Immutability), Publish, Versions Diff, and Git-Revert Rollback', async ({ request }) => {
    const shop = 'pw-api-versioning';
    await request.post('/api/database/reset', { headers: { 'X-Shop-Id': shop } });

    // 1. Publish v1 baseline via /api/patches/publish
    const pub1Res = await request.post('/api/patches/publish', {
      headers: { 'X-Shop-Id': shop },
      data: {
        patch_id: 'patch-v1',
        description: 'Initial Baseline v1',
        author: 'release-bot',
        items: [
          {
            sku: 'SKU-VER-1',
            name: 'Mechanical Switch Pack v1',
            price: 45.0,
            status: 'ACTIVE',
            timestamp: '2026-01-01T10:00:00Z',
          },
          {
            sku: 'SKU-VER-2',
            name: 'Keycap Puller v1',
            price: 12.0,
            status: 'ACTIVE',
            timestamp: '2026-01-01T10:00:00Z',
          },
        ],
      },
    });
    expect(pub1Res.status()).toBe(201);
    const pub1Data = await pub1Res.json();
    expect(pub1Data.version.version_number).toBe(1);
    const v1Id = pub1Data.version.version_id;

    // 2. Preview a dry-run patch and verify zero DB mutation
    const prevRes = await request.post('/api/patches/preview', {
      headers: { 'X-Shop-Id': shop },
      data: {
        description: 'Dry-run price increase',
        author: 'qa-engineer',
        items: [
          {
            sku: 'SKU-VER-1',
            name: 'Mechanical Switch Pack v2 Preview',
            price: 59.0,
            status: 'ACTIVE',
            timestamp: '2026-01-02T10:00:00Z',
          },
        ],
      },
    });
    expect(prevRes.status()).toBe(200);
    const prevData = await prevRes.json();
    expect(prevData.can_commit).toBe(true);
    expect(prevData.updated_count).toBe(1);

    // Verify live catalog was NOT mutated by preview
    const checkProd = await (
      await request.get('/api/products?search=SKU-VER-1', { headers: { 'X-Shop-Id': shop } })
    ).json();
    expect(checkProd.products[0].price).toBe(45.0);

    // 3. Publish v2 with price change on SKU-VER-1, withdrawal of SKU-VER-2, and addition of SKU-VER-3
    const pub2Res = await request.post('/api/patches/publish', {
      headers: { 'X-Shop-Id': shop },
      data: {
        patch_id: 'patch-v2',
        description: 'Update v2 and withdraw SKU-VER-2',
        author: 'release-bot',
        items: [
          {
            sku: 'SKU-VER-1',
            name: 'Mechanical Switch Pack v2',
            price: 65.0,
            status: 'ACTIVE',
            timestamp: '2026-01-02T10:00:00Z',
          },
          {
            sku: 'SKU-VER-2',
            name: 'Keycap Puller v1',
            price: 12.0,
            status: 'WITHDRAWN',
            timestamp: '2026-01-02T10:00:00Z',
          },
          {
            sku: 'SKU-VER-3',
            name: 'Lubricant Station v2',
            price: 30.0,
            status: 'ACTIVE',
            timestamp: '2026-01-02T10:00:00Z',
          },
        ],
      },
    });
    expect(pub2Res.status()).toBe(201);
    const v2Id = (await pub2Res.json()).version.version_id;

    // 4. Verify GET /api/versions/{id}/diff
    const diffRes = await request.get(`/api/versions/${v2Id}/diff`, { headers: { 'X-Shop-Id': shop } });
    expect(diffRes.status()).toBe(200);
    const diffItems = await diffRes.json();
    expect(Array.isArray(diffItems) ? diffItems : diffItems.changes).toHaveLength(3);

    // 5. Rollback to v1 and verify Tombstone Invariant (SKU-VER-2 stays WITHDRAWN, SKU-VER-1 reverts to $45, SKU-VER-3 becomes WITHDRAWN)
    const rollRes = await request.post(`/api/versions/${v1Id}/rollback`, {
      headers: { 'X-Shop-Id': shop },
      data: { operator: 'sre-oncall', description: 'Emergency rollback to v1' },
    });
    expect(rollRes.status()).toBe(200);
    const rollData = await rollRes.json();
    expect(rollData.version.type).toBe('ROLLBACK');
    expect(rollData.version.version_number).toBe(3);
    expect(rollData.version.author).toBe('sre-oncall');
    expect(rollData.version.revert_of_version_id).toBe(v1Id);

    const finalProds = await (await request.get('/api/products?limit=20', { headers: { 'X-Shop-Id': shop } })).json();
    const map = Object.fromEntries(finalProds.products.map((p: any) => [p.sku, p]));
    expect(map['SKU-VER-1'].price).toBe(45.0);
    expect(map['SKU-VER-1'].name).toBe('Mechanical Switch Pack v1');
    expect(map['SKU-VER-2'].status).toBe('WITHDRAWN'); // Tombstone preserved!
    expect(map['SKU-VER-3'].status).toBe('WITHDRAWN'); // Post-v1 addition withdrawn!
  });

  test('7. Metrics Filtering (today, yesterday, 7d, all, custom from/to, job_id) and Error Validation', async ({ request }) => {
    const shop = 'pw-api-invariants'; // Already has job-invariants-test

    for (const range of ['today', 'yesterday', '7d', 'last7days', 'all']) {
      const res = await request.get(`/api/metrics?time_range=${range}`, { headers: { 'X-Shop-Id': shop } });
      expect(res.status()).toBe(200);
      const data = await res.json();
      expect(data.metrics).toBeDefined();
      expect(typeof data.metrics.quality_score).toBe('number');
    }

    // Specific job_id filter
    const jobMetRes = await request.get('/api/metrics?job_id=job-invariants-test', {
      headers: { 'X-Shop-Id': shop },
    });
    expect(jobMetRes.status()).toBe(200);
    const jobMet = await jobMetRes.json();
    expect(jobMet.metrics.total_processed).toBe(11);
    expect(jobMet.metrics.summary.added).toBe(2);

    // Invalid time_range -> 400
    const badRange = await request.get('/api/metrics?time_range=invalid_range', {
      headers: { 'X-Shop-Id': shop },
    });
    expect(badRange.status()).toBe(400);

    // Invalid from / to date format -> 400
    const badFromFormat = await request.get('/api/metrics?from=not-a-valid-date', {
      headers: { 'X-Shop-Id': shop },
    });
    expect(badFromFormat.status()).toBe(400);

    const badToFormat = await request.get('/api/metrics?to=not-a-valid-date', {
      headers: { 'X-Shop-Id': shop },
    });
    expect(badToFormat.status()).toBe(400);

    // Valid custom date range
    const customRangeRes = await request.get(
      '/api/metrics?time_range=custom&from=2026-01-01&to=2030-12-31',
      { headers: { 'X-Shop-Id': shop } }
    );
    expect(customRangeRes.status()).toBe(200);
    const customRangeData = await customRangeRes.json();
    expect(customRangeData.metrics.total_processed).toBe(11);
  });
});
