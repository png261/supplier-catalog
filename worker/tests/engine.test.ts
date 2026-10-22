import { describe, it, expect, beforeEach } from 'vitest';
import { MockD1Database } from './d1-mock';
import { D1Storage } from '../src/storage';
import { SyncEngine } from '../src/engine';
import { Batch, ChangeSummary, SupplierUpdate } from '../src/types';

describe('SyncEngine Core Invariants', () => {
  let db: any;
  let storage: D1Storage;
  let engine: SyncEngine;
  const shopId = 'test-shop';

  beforeEach(async () => {
    db = new MockD1Database();
    storage = new D1Storage(db as any);
    engine = new SyncEngine(db as any, storage);

    // Seed test job
    await storage.createJob(shopId, {
      job_id: 'job-1',
      status: 'RUNNING',
      total_batches: 5,
      committed_batch_index: -1,
      summary: { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 },
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
    });
  });

  it('Invariant A: Deterministic Idempotency - duplicate payloads produce 0 mutations and increment skipped', async () => {
    const record: SupplierUpdate = {
      sku: 'SKU-001',
      name: 'Wireless Mouse',
      price: 29.99,
      status: 'ACTIVE',
      timestamp: '2026-01-01T10:00:00.000Z',
    };

    const batch1: Batch = {
      job_id: 'job-1',
      batch_index: 0,
      records: [record],
    };

    const delta1 = await engine.executeBatch(shopId, batch1, { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
    expect(delta1.added).toBe(1);
    expect(delta1.skipped).toBe(0);

    const product = await storage.getProduct(shopId, 'SKU-001');
    expect(product).not.toBeNull();
    expect(product?.name).toBe('Wireless Mouse');

    // Ingest the EXACT same batch again
    const batch2: Batch = {
      job_id: 'job-1',
      batch_index: 1,
      records: [record],
    };

    const delta2 = await engine.executeBatch(shopId, batch2, delta1);
    expect(delta2.added).toBe(0);
    expect(delta2.updated).toBe(0);
    expect(delta2.withdrawn).toBe(0);
    expect(delta2.skipped).toBe(1);
    expect(delta2.dead_letter).toBe(0);
  });

  it('Invariant B: Fast-Forward Timestamps - rejects records with older or equal timestamps', async () => {
    const recordNewer: SupplierUpdate = {
      sku: 'SKU-002',
      name: 'Mechanical Keyboard',
      price: 89.99,
      status: 'ACTIVE',
      timestamp: '2026-01-01T12:00:00.000Z',
    };

    await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 0, records: [recordNewer] }, { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });

    // Try applying older update (T_old < T_new) with modified price
    const recordOlder: SupplierUpdate = {
      sku: 'SKU-002',
      name: 'Mechanical Keyboard v1',
      price: 49.99,
      status: 'ACTIVE',
      timestamp: '2026-01-01T11:00:00.000Z',
    };

    const delta = await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 1, records: [recordOlder] }, { added: 1, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
    expect(delta.skipped).toBe(1);
    expect(delta.updated).toBe(0);

    // Verify catalog state is preserved
    const current = await storage.getProduct(shopId, 'SKU-002');
    expect(current?.price).toBe(89.99);
    expect(current?.name).toBe('Mechanical Keyboard');
  });

  it('Invariant C: Tombstone Preservation - product once WITHDRAWN can NEVER be resurrected to ACTIVE', async () => {
    const recordInitial: SupplierUpdate = {
      sku: 'SKU-003',
      name: 'Desk Lamp',
      price: 19.99,
      status: 'ACTIVE',
      timestamp: '2026-01-01T10:00:00.000Z',
    };
    await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 0, records: [recordInitial] }, { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });

    // Withdraw product
    const recordWithdrawn: SupplierUpdate = {
      sku: 'SKU-003',
      name: 'Desk Lamp',
      price: 19.99,
      status: 'WITHDRAWN',
      timestamp: '2026-01-01T11:00:00.000Z',
    };
    const deltaWithdraw = await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 1, records: [recordWithdrawn] }, { added: 1, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
    expect(deltaWithdraw.withdrawn).toBe(1);

    const productWithdrawn = await storage.getProduct(shopId, 'SKU-003');
    expect(productWithdrawn?.status).toBe('WITHDRAWN');

    // Attempt resurrection with newer timestamp
    const recordResurrect: SupplierUpdate = {
      sku: 'SKU-003',
      name: 'Desk Lamp Reactivated',
      price: 25.99,
      status: 'ACTIVE',
      timestamp: '2026-01-01T12:00:00.000Z',
    };
    const deltaResurrect = await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 2, records: [recordResurrect] }, { added: 1, updated: 0, withdrawn: 1, skipped: 0, dead_letter: 0 });
    expect(deltaResurrect.skipped).toBe(1);
    expect(deltaResurrect.updated).toBe(0);

    const productAfter = await storage.getProduct(shopId, 'SKU-003');
    expect(productAfter?.status).toBe('WITHDRAWN');
  });

  it('Invariant D: Dead-Letter Isolation - malformed records quarantined without halting batch', async () => {
    const records: SupplierUpdate[] = [
      { sku: '', name: 'Empty SKU', price: 10, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00.000Z' },
      { sku: 'SKU-004', name: 'Negative Price', price: -5, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00.000Z' },
      { sku: 'SKU-005', name: 'Invalid Status', price: 20, status: 'UNKNOWN' as any, timestamp: '2026-01-01T10:00:00.000Z' },
      { sku: 'SKU-006', name: 'Zero Timestamp', price: 30, status: 'ACTIVE', timestamp: '' },
      { sku: 'SKU-VALID', name: 'Valid Item', price: 100, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00.000Z' },
    ];

    const delta = await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 0, records }, { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
    expect(delta.dead_letter).toBe(4);
    expect(delta.added).toBe(1);

    const validProd = await storage.getProduct(shopId, 'SKU-VALID');
    expect(validProd).not.toBeNull();
    expect(validProd?.name).toBe('Valid Item');

    const dlIssues = await storage.listDeadLetterRecords(shopId);
    expect(dlIssues.total).toBe(4);
  });

  it('Invariant E: Atomic Batch Transactions & Checkpoints', async () => {
    const records: SupplierUpdate[] = [
      { sku: 'SKU-A', name: 'Item A', price: 10, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00.000Z' },
      { sku: 'SKU-B', name: 'Item B', price: 20, status: 'ACTIVE', timestamp: '2026-01-01T10:00:00.000Z' },
    ];

    const delta = await engine.executeBatch(shopId, { job_id: 'job-1', batch_index: 0, records }, { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 });
    expect(delta.added).toBe(2);

    const checkpoints = await storage.getBatchCheckpoints(shopId, 'job-1');
    expect(checkpoints.length).toBe(1);
    expect(checkpoints[0].batch_index).toBe(0);
    expect(checkpoints[0].summary.added).toBe(2);

    const job = await storage.getJob(shopId, 'job-1');
    expect(job?.committed_batch_index).toBe(0);
    expect(job?.summary.added).toBe(2);
  });
});
