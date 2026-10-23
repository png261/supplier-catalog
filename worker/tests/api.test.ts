import { describe, it, expect, beforeEach } from 'vitest';
import app from '../src/index';
import { MockD1Database } from './d1-mock';

describe('Cloudflare Worker REST API', () => {
  let mockDB: any;

  beforeEach(() => {
    mockDB = new MockD1Database();
  });

  const makeRequest = async (path: string, options: RequestInit = {}, shopId = 'default') => {
    const headers = new Headers(options.headers || {});
    if (shopId && !headers.has('X-Shop-Id')) {
      headers.set('X-Shop-Id', shopId);
    }
    const req = new Request(`http://localhost${path}`, {
      ...options,
      headers,
    });
    return app.fetch(req, { DB: mockDB } as any, {
      waitUntil: (promise: Promise<any>) => {
        // synchronously wait in test
        promise.catch(() => {});
      },
      passThroughOnException: () => {},
    } as any);
  };

  it('GET /health returns 200 ok', async () => {
    const res = await makeRequest('/health');
    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.status).toBe('ok');
  });

  it('POST /api/jobs starts a synchronization job in batches mode', async () => {
    const payload = {
      mode: 'batches',
      job_id: 'test-job-batches',
      batches: [
        {
          batch_index: 0,
          records: [
            { sku: 'SKU-001', name: 'Item 1', price: 10, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00Z' },
            { sku: 'SKU-002', name: 'Item 2', price: 20, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00Z' },
          ],
        },
      ],
    };

    const res = await makeRequest('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(payload),
    });

    expect(res.status).toBe(202);
    const data = await res.json();
    expect(data.job_id).toBe('test-job-batches');
    expect(data.status).toBe('RUNNING');
  });

  it('GET /api/products returns paginated products', async () => {
    // Insert a product via database
    await mockDB.exec(`
      INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at)
      VALUES ('default', 'SKU-P1', 'Test Product', 99.99, 'ACTIVE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
    `);

    const res = await makeRequest('/api/products?limit=10&offset=0');
    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.total).toBe(1);
    expect(data.products[0].sku).toBe('SKU-P1');
  });

  it('Supports multi-tenant isolation via X-Shop-Id', async () => {
    await mockDB.exec(`
      INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at)
      VALUES ('shop-nike', 'SKU-NIKE-1', 'Air Max', 150.0, 'ACTIVE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
      INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at)
      VALUES ('shop-adidas', 'SKU-ADI-1', 'Ultraboost', 180.0, 'ACTIVE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
    `);

    // Query Nike
    const resNike = await makeRequest('/api/products', {}, 'shop-nike');
    const dataNike = await resNike.json();
    expect(dataNike.total).toBe(1);
    expect(dataNike.products[0].sku).toBe('SKU-NIKE-1');

    // Query Adidas
    const resAdidas = await makeRequest('/api/products', {}, 'shop-adidas');
    const dataAdidas = await resAdidas.json();
    expect(dataAdidas.total).toBe(1);
    expect(dataAdidas.products[0].sku).toBe('SKU-ADI-1');
  });

  it('POST /api/dead-letter/:id/resolve remediates issue and imports to catalog', async () => {
    await mockDB.exec(`
      INSERT INTO dead_letter_issues (shop_id, issue_id, job_id, batch_index, sku, raw_payload, reason, created_at)
      VALUES ('default', 'dl-100', 'job-x', 0, 'SKU-FIX', '{"sku":""}', 'sku cannot be empty', '2026-01-01T00:00:00Z');
    `);

    const resolveRes = await makeRequest('/api/dead-letter/dl-100/resolve', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        sku: 'SKU-FIXED',
        name: 'Fixed Chair',
        price: 120.0,
        status: 'ACTIVE',
      }),
    });

    expect(resolveRes.status).toBe(200);
    const data = await resolveRes.json();
    expect(data.status).toBe('success');
    expect(data.product.sku).toBe('SKU-FIXED');

    // Verify issue was deleted
    const issuesRes = await makeRequest('/api/dead-letter');
    const issuesData = await issuesRes.json();
    expect(issuesData.total).toBe(0);

    // Verify product is in catalog
    const prodRes = await makeRequest('/api/products');
    const prodData = await prodRes.json();
    expect(prodData.total).toBe(1);
    expect(prodData.products[0].sku).toBe('SKU-FIXED');
  });

  it('POST /api/database/reset resets all tenant tables', async () => {
    await mockDB.exec(`
      INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at)
      VALUES ('default', 'SKU-TO-DELETE', 'Old Item', 10, 'ACTIVE', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z');
    `);

    const resetRes = await makeRequest('/api/database/reset', { method: 'POST' });
    expect(resetRes.status).toBe(200);

    const prods = await makeRequest('/api/products');
    const data = await prods.json();
    expect(data.total).toBe(0);
  });

  it('GET /api/versions and POST /api/versions/:id/rollback executes non-destructive rollback', async () => {
    // 1. Seed version v1 and deltas
    await mockDB.exec(`
      INSERT INTO catalog_versions (
        shop_id, version_id, version_number, description, author, type,
        items_added, items_updated, items_withdrawn, total_items_affected, metadata, created_at
      ) VALUES (
        'default', 'v1-init', 1, 'Initial Catalog Baseline', 'seed', 'INITIAL',
        1, 0, 0, 1, '{}', '2026-01-01T00:00:00Z'
      );

      INSERT INTO catalog_version_deltas (
        shop_id, version_id, sku, action, old_name, new_name, old_price, new_price,
        old_status, new_status, old_timestamp, new_timestamp
      ) VALUES (
        'default', 'v1-init', 'SKU-001', 'ADDED', NULL, 'Widget v1', NULL, 100.0,
        NULL, 'ACTIVE', NULL, '2026-01-01T00:00:00Z'
      );

      INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at)
      VALUES ('default', 'SKU-001', 'Widget v2 Modified', 150.0, 'ACTIVE', '2026-01-02T00:00:00Z', '2026-01-02T00:00:00Z');
    `);

    // 2. Query versions list
    const listRes = await makeRequest('/api/versions');
    expect(listRes.status).toBe(200);
    const listData = await listRes.json();
    expect(listData.total).toBe(1);
    expect(listData.versions[0].version_id).toBe('v1-init');

    // 3. Query diff
    const diffRes = await makeRequest('/api/versions/v1-init/diff');
    expect(diffRes.status).toBe(200);
    const diffData = await diffRes.json();
    expect(diffData.changes.length).toBe(1);
    expect(diffData.changes[0].sku).toBe('SKU-001');

    // 4. Execute rollback to v1-init
    const rollbackRes = await makeRequest('/api/versions/v1-init/rollback', {
      method: 'POST',
      body: JSON.stringify({ operator: 'test-staff', description: 'Revert to initial widget price' }),
    });
    expect(rollbackRes.status).toBe(200);
    const rollbackData = await rollbackRes.json();
    expect(rollbackData.version.type).toBe('ROLLBACK');
    expect(rollbackData.version.version_number).toBe(2);
    expect(rollbackData.version.revert_of_version_id).toBe('v1-init');

    // 5. Verify live product was restored to price 100.0 and name 'Widget v1'
    const prodRes = await makeRequest('/api/products');
    const prodData = await prodRes.json();
    expect(prodData.products[0].price).toBe(100.0);
    expect(prodData.products[0].name).toBe('Widget v1');
  });

  it('GET / returns API metadata and GET /api/events returns SSE stream', async () => {
    const rootRes = await makeRequest('/');
    expect(rootRes.status).toBe(200);
    const rootData = await rootRes.json();
    expect(rootData.version).toBe('1.0.0');

    const sseRes = await makeRequest('/api/events?shop=test-shop');
    expect(sseRes.status).toBe(200);
    expect(sseRes.headers.get('Content-Type')).toContain('text/event-stream');
  });

  it('Validates POST /api/jobs payloads and supports generator mode, cancel, resume, and job listing', async () => {
    // Invalid mode
    const badMode = await makeRequest('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'invalid-mode' }),
    });
    expect(badMode.status).toBe(400);

    // Empty batches in batches mode
    const emptyBatches = await makeRequest('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'batches', batches: [] }),
    });
    expect(emptyBatches.status).toBe(400);

    // Invalid ratio > 1.0
    const badRatio = await makeRequest('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'generator', update_ratio: 1.5 }),
    });
    expect(badRatio.status).toBe(400);

    // Sum of ratios > 1.0
    const badSum = await makeRequest('/api/jobs', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ mode: 'generator', update_ratio: 0.6, duplicate_ratio: 0.6 }),
    });
    expect(badSum.status).toBe(400);

    // Seed an interrupted job to test cancel & resume
    await mockDB.exec(`
      INSERT INTO sync_jobs (
        shop_id, job_id, status, total_batches, committed_batch_index,
        items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
        created_at, updated_at
      ) VALUES (
        'default', 'job-interrupted-1', 'INTERRUPTED', 2, 0,
        5, 0, 0, 0, 0, '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z'
      );
    `);

    const listJobsRes = await makeRequest('/api/jobs?status=INTERRUPTED');
    expect(listJobsRes.status).toBe(200);
    const listJobsData = await listJobsRes.json();
    expect(listJobsData.total).toBe(1);
    expect(listJobsData.jobs[0].job_id).toBe('job-interrupted-1');

    const getJobRes = await makeRequest('/api/jobs/job-interrupted-1');
    expect(getJobRes.status).toBe(200);
    const getJobData = await getJobRes.json();
    expect(getJobData.job.job_id).toBe('job-interrupted-1');

    const missingJobRes = await makeRequest('/api/jobs/non-existent-job');
    expect(missingJobRes.status).toBe(404);

    // Resume interrupted job
    const resumeRes = await makeRequest('/api/jobs/job-interrupted-1/resume', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ seed: 42, total_batches: 2, batch_size: 5 }),
    });
    expect(resumeRes.status).toBe(202);

    // Cancel job
    const cancelRes = await makeRequest('/api/jobs/job-interrupted-1/cancel', { method: 'POST' });
    expect(cancelRes.status).toBe(200);
  });

  it('Filters dead-letter records by UI reason codes and validates resolve payloads', async () => {
    await mockDB.exec(`
      INSERT INTO dead_letter_issues (shop_id, issue_id, job_id, batch_index, sku, raw_payload, reason, created_at)
      VALUES
        ('default', 'dl-e1', 'job-1', 0, '', '{}', 'sku cannot be empty', '2026-01-01T00:00:00Z'),
        ('default', 'dl-e2', 'job-1', 0, 'S2', '{}', 'price cannot be negative', '2026-01-01T00:00:00Z'),
        ('default', 'dl-e3', 'job-1', 0, 'S3', '{}', 'timestamp must not be zero', '2026-01-01T00:00:00Z'),
        ('default', 'dl-e4', 'job-1', 0, 'S4', '{}', 'status must be ACTIVE or WITHDRAWN', '2026-01-01T00:00:00Z');
    `);

    for (const code of ['EMPTY_SKU', 'NEGATIVE_OR_ZERO_PRICE', 'ZERO_OR_MISSING_TIMESTAMP', 'CORRUPTED_JSON']) {
      const res = await makeRequest(`/api/dead-letter?reason=${code}`);
      expect(res.status).toBe(200);
      const data = await res.json();
      expect(data.total).toBe(1);
    }

    // Validation errors on resolve
    const badSku = await makeRequest('/api/dead-letter/dl-e1/resolve', {
      method: 'POST',
      body: JSON.stringify({ sku: '', price: 10, status: 'ACTIVE' }),
    });
    expect(badSku.status).toBe(400);

    const badPrice = await makeRequest('/api/dead-letter/dl-e1/resolve', {
      method: 'POST',
      body: JSON.stringify({ sku: 'S1', price: -5, status: 'ACTIVE' }),
    });
    expect(badPrice.status).toBe(400);

    const badStatus = await makeRequest('/api/dead-letter/dl-e1/resolve', {
      method: 'POST',
      body: JSON.stringify({ sku: 'S1', price: 10, status: 'INVALID' }),
    });
    expect(badStatus.status).toBe(400);

    const notFound = await makeRequest('/api/dead-letter/dl-missing/resolve', {
      method: 'POST',
      body: JSON.stringify({ sku: 'S1', price: 10, status: 'ACTIVE' }),
    });
    expect(notFound.status).toBe(404);
  });

  it('Supports GET /api/metrics time_range filters (today, yesterday, 7d, all, job_id)', async () => {
    const nowIso = new Date().toISOString();
    await mockDB.exec(`
      INSERT INTO sync_jobs (
        shop_id, job_id, status, total_batches, committed_batch_index,
        items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
        created_at, updated_at, completed_at
      ) VALUES (
        'default', 'job-metrics-1', 'COMPLETED', 1, 0,
        10, 2, 1, 3, 1, '${nowIso}', '${nowIso}', '${nowIso}'
      );
    `);

    for (const range of ['today', 'yesterday', '7d', 'all']) {
      const res = await makeRequest(`/api/metrics?time_range=${range}`);
      expect(res.status).toBe(200);
      const data = await res.json();
      expect(data.filter.time_range).toBe(range);
    }

    const jobFilterRes = await makeRequest('/api/metrics?job_id=job-metrics-1');
    expect(jobFilterRes.status).toBe(200);
    const jobFilterData = await jobFilterRes.json();
    expect(jobFilterData.metrics.total_processed).toBe(17);
  });
});
