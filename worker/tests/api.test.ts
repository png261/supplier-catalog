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
});
