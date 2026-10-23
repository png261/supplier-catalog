import { Hono } from 'hono';
import { cors } from 'hono/cors';
import { Batch, ChangeSummary, Env, GeneratorConfig, Product, ProductStatus, SyncJob } from './types';
import { D1Storage } from './storage';
import { SyncEngine } from './engine';
import { BatchGenerator } from './generator';
import { broker } from './sse';

const app = new Hono<{ Bindings: Env }>();

// In-memory set for cancelled jobs within the active isolate
const activeJobs = new Map<string, string>(); // shopId -> jobId
const cancelledJobs = new Set<string>();

// CORS configuration
app.use(
  '*',
  cors({
    origin: '*',
    allowMethods: ['GET', 'POST', 'PUT', 'DELETE', 'OPTIONS'],
    allowHeaders: ['Content-Type', 'X-Shop-Id', 'Authorization'],
    exposeHeaders: ['Content-Length', 'X-Shop-Id'],
    maxAge: 86400,
  })
);

// Tenant resolution helper
function getTenantShopId(c: any): string {
  const headerShop = c.req.header('X-Shop-Id');
  const queryShop = c.req.query('shop') || c.req.query('shop_id');
  const raw = (headerShop || queryShop || 'default').trim().toLowerCase();
  if (!/^[a-zA-Z0-9_-]{1,64}$/.test(raw)) {
    return 'default';
  }
  return raw;
}

// Health check
app.get('/health', (c) => {
  return c.json({ status: 'ok', engine: 'Cloudflare D1 Catalog Sync', version: '1.0.0' });
});

app.get('/', (c) => {
  return c.json({
    message: 'Resilient Catalog Synchronization Engine API (Cloudflare Worker & D1)',
    version: '1.0.0',
    docs: 'https://github.com/coderpush/catalog-engine',
  });
});

// --- Server-Sent Events (SSE) ---

app.get('/api/events', (c) => {
  const shopId = getTenantShopId(c);
  const stream = broker.addSubscriber(shopId, c.req.raw.signal);

  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream; charset=utf-8',
      'Cache-Control': 'no-cache, no-transform',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no',
      'Access-Control-Allow-Origin': '*',
    },
  });
});

// --- Jobs Endpoints ---

app.post('/api/jobs', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const engine = new SyncEngine(c.env.DB, storage);

  // Check running job (recover orphaned/zombie jobs stuck in RUNNING > 15s)
  let existingActive = await storage.getActiveJob(shopId);
  if (existingActive) {
    const updatedMs = new Date(existingActive.updated_at).getTime();
    const isOrphaned = activeJobs.get(shopId) !== existingActive.job_id && (isNaN(updatedMs) || Date.now() - updatedMs > 15000);
    if (isOrphaned) {
      await storage.updateJobStatus(shopId, existingActive.job_id, 'INTERRUPTED');
      existingActive = null;
    } else {
      return c.json(
        { error: `a synchronization job is already running: ${existingActive.job_id}`, code: 'JOB_CONFLICT' },
        409
      );
    }
  }

  let body: any = {};
  try {
    body = await c.req.json();
  } catch {
    body = {};
  }

  const mode = body.mode || 'generator';
  const jobId = body.job_id || `job-${Date.now()}`;
  let totalBatches = body.total_batches ?? 10;
  let directBatches: Batch[] = [];
  let generatorConfig: GeneratorConfig | null = null;

  if (mode === 'batches') {
    if (!Array.isArray(body.batches) || body.batches.length === 0) {
      return c.json({ error: 'batches array cannot be empty in batches mode', code: 'INVALID_REQUEST' }, 400);
    }
    totalBatches = body.batches.length;
    directBatches = body.batches.map((b: any, idx: number) => ({
      job_id: jobId,
      batch_index: b.batch_index ?? idx,
      records: (b.records || []).map((r: any) => ({
        sku: r.sku,
        name: r.name,
        price: r.price,
        status: r.status,
        timestamp: r.timestamp || new Date().toISOString(),
      })),
    }));
  } else if (mode === 'generator') {
    const ratios = [
      body.update_ratio ?? 0.20,
      body.duplicate_ratio ?? 0.05,
      body.out_of_order_ratio ?? 0.10,
      body.withdrawn_ratio ?? 0.05,
      body.invalid_ratio ?? 0.05,
    ];
    for (const r of ratios) {
      if (r < 0 || r > 1) {
        return c.json({ error: 'ratio parameters must be between 0.0 and 1.0', code: 'INVALID_REQUEST' }, 400);
      }
    }
    const sumRatios = ratios.reduce((a, b) => a + b, 0);
    if (sumRatios > 1.00001) {
      return c.json({ error: 'sum of anomaly and update ratios cannot exceed 1.0', code: 'INVALID_REQUEST' }, 400);
    }

    let baseTime = body.base_time;
    const maxTs = await storage.getMaxSupplierTimestamp(shopId);
    if (maxTs) {
      const maxDate = new Date(maxTs).getTime();
      const reqDate = baseTime ? new Date(baseTime).getTime() : 0;
      if (!baseTime || isNaN(reqDate) || reqDate <= maxDate) {
        baseTime = new Date(maxDate + 86400000).toISOString();
      }
    } else if (!baseTime) {
      baseTime = '2026-01-01T00:00:00.000Z';
    }

    generatorConfig = {
      seed: body.seed ?? 42,
      total_batches: totalBatches,
      batch_size: body.batch_size ?? 100,
      update_ratio: body.update_ratio ?? 0.20,
      duplicate_ratio: body.duplicate_ratio ?? 0.05,
      out_of_order_ratio: body.out_of_order_ratio ?? 0.10,
      withdrawn_ratio: body.withdrawn_ratio ?? 0.05,
      invalid_ratio: body.invalid_ratio ?? 0.05,
      reservoir_capacity: 1000,
      base_time: baseTime,
    };
  } else {
    return c.json({ error: `unsupported mode: ${mode}`, code: 'INVALID_REQUEST' }, 400);
  }

  const now = new Date().toISOString();
  const initialSummary: ChangeSummary = { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 };
  const job: SyncJob = {
    job_id: jobId,
    status: 'RUNNING',
    total_batches: totalBatches,
    committed_batch_index: -1,
    summary: initialSummary,
    created_at: now,
    updated_at: now,
  };

  await storage.createJob(shopId, job);
  activeJobs.set(shopId, jobId);
  cancelledJobs.delete(jobId);

  // Broadcast job_started
  broker.publish(shopId, {
    type: 'JOB_STARTED',
    shop_id: shopId,
    job_id: jobId,
    status: 'RUNNING',
    total_batches: totalBatches,
    created_at: now,
  });

  // Background execution via ctx.waitUntil
  c.executionCtx.waitUntil(
    (async () => {
      try {
        let generator: BatchGenerator | null = null;
        if (generatorConfig) {
          generator = new BatchGenerator(generatorConfig);
        }

        const cumulativeSummary: ChangeSummary = { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 };

        for (let batchIndex = 0; batchIndex < totalBatches; batchIndex++) {
          // Check cancellation
          if (cancelledJobs.has(jobId)) {
            await storage.updateJobStatus(shopId, jobId, 'INTERRUPTED');
            activeJobs.delete(shopId);
            broker.publish(shopId, {
              type: 'JOB_INTERRUPTED',
              shop_id: shopId,
              job_id: jobId,
              status: 'INTERRUPTED',
              committed_batch_index: batchIndex - 1,
              total_batches: totalBatches,
              summary: cumulativeSummary,
              reason: 'cancelled by user request',
            });
            return;
          }

          let batch: Batch;
          if (mode === 'batches') {
            batch = directBatches[batchIndex];
          } else {
            batch = generator!.generateBatch(jobId, batchIndex);
          }

          // Execute batch inside atomic D1 transaction
          const delta = await engine.executeBatch(shopId, batch, cumulativeSummary, body.description || '');
          cumulativeSummary.added += delta.added;
          cumulativeSummary.updated += delta.updated;
          cumulativeSummary.withdrawn += delta.withdrawn;
          cumulativeSummary.skipped += delta.skipped;
          cumulativeSummary.dead_letter += delta.dead_letter;

          // Emit BATCH_COMMITTED
          broker.publish(shopId, {
            type: 'BATCH_COMMITTED',
            shop_id: shopId,
            job_id: jobId,
            batch_index: batchIndex,
            total_batches: totalBatches,
            batch_summary: delta,
            cumulative_summary: { ...cumulativeSummary },
          });

          // Also emit dead_letter event if new issues arose
          if (delta.dead_letter > 0) {
            broker.publish(shopId, {
              type: 'dead_letter',
              shop_id: shopId,
              job_id: jobId,
              batch_index: batchIndex,
              new_issues: delta.dead_letter,
              total_dead_letter: cumulativeSummary.dead_letter,
            });
          }
        }

        // Complete job
        const completedAt = new Date().toISOString();
        await storage.updateJobStatus(shopId, jobId, 'COMPLETED', completedAt);
        activeJobs.delete(shopId);

        broker.publish(shopId, {
          type: 'JOB_COMPLETED',
          shop_id: shopId,
          job_id: jobId,
          status: 'COMPLETED',
          total_batches: totalBatches,
          summary: cumulativeSummary,
          completed_at: completedAt,
        });
      } catch (err: any) {
        await storage.updateJobStatus(shopId, jobId, 'FAILED');
        activeJobs.delete(shopId);
        broker.publish(shopId, {
          type: 'JOB_FAILED',
          shop_id: shopId,
          job_id: jobId,
          error: err?.message || 'internal batch execution error',
        });
      }
    })()
  );

  return c.json(job, 202);
});

app.post('/api/jobs/:id/resume', async (c) => {
  const shopId = getTenantShopId(c);
  const jobId = c.req.param('id');
  const storage = new D1Storage(c.env.DB);
  const engine = new SyncEngine(c.env.DB, storage);

  let existingActive = await storage.getActiveJob(shopId);
  if (existingActive) {
    const updatedMs = new Date(existingActive.updated_at).getTime();
    const isOrphaned = activeJobs.get(shopId) !== existingActive.job_id && (isNaN(updatedMs) || Date.now() - updatedMs > 15000);
    if (isOrphaned) {
      await storage.updateJobStatus(shopId, existingActive.job_id, 'INTERRUPTED');
      existingActive = null;
    } else {
      return c.json({ error: `job ${existingActive.job_id} is already running`, code: 'JOB_CONFLICT' }, 409);
    }
  }

  const job = await storage.getJob(shopId, jobId);
  if (!job) {
    return c.json({ error: `sync job not found: ${jobId}`, code: 'JOB_NOT_FOUND' }, 404);
  }

  if (job.status === 'COMPLETED') {
    return c.json({ error: 'job is already completed and cannot be resumed', code: 'JOB_CONFLICT' }, 409);
  }
  if (job.status === 'RUNNING') {
    const updatedMs = new Date(job.updated_at).getTime();
    const isOrphaned = activeJobs.get(shopId) !== jobId && (isNaN(updatedMs) || Date.now() - updatedMs > 15000);
    if (!isOrphaned) {
      return c.json({ error: `job ${jobId} is already running`, code: 'JOB_CONFLICT' }, 409);
    }
  }

  let body: any = {};
  try {
    body = await c.req.json();
  } catch {}

  const startBatchIndex = job.committed_batch_index + 1;
  const totalBatches = body.total_batches ?? job.total_batches;

  let baseTime = body.base_time;
  const maxTs = await storage.getMaxSupplierTimestamp(shopId);
  if (maxTs) {
    const maxDate = new Date(maxTs).getTime();
    const reqDate = baseTime ? new Date(baseTime).getTime() : 0;
    if (!baseTime || isNaN(reqDate) || reqDate <= maxDate) {
      baseTime = new Date(maxDate + 86400000).toISOString();
    }
  } else if (!baseTime) {
    baseTime = '2026-01-01T00:00:00.000Z';
  }

  await storage.updateJobStatus(shopId, jobId, 'RUNNING');
  activeJobs.set(shopId, jobId);
  cancelledJobs.delete(jobId);

  const now = new Date().toISOString();
  broker.publish(shopId, {
    type: 'JOB_STARTED',
    shop_id: shopId,
    job_id: jobId,
    status: 'RUNNING',
    resumed_from_batch: startBatchIndex,
    total_batches: totalBatches,
    timestamp: now,
  });

  c.executionCtx.waitUntil(
    (async () => {
      try {
        const generator = new BatchGenerator({
          seed: body.seed ?? 42,
          total_batches: totalBatches,
          batch_size: body.batch_size ?? 100,
          update_ratio: body.update_ratio ?? 0.20,
          duplicate_ratio: body.duplicate_ratio ?? 0.05,
          out_of_order_ratio: body.out_of_order_ratio ?? 0.10,
          withdrawn_ratio: body.withdrawn_ratio ?? 0.05,
          invalid_ratio: body.invalid_ratio ?? 0.05,
          base_time: baseTime,
        });

        // Fast-forward generator state to startBatchIndex
        for (let i = 0; i < startBatchIndex; i++) {
          generator.generateBatch(jobId, i);
        }

        const cumulativeSummary = { ...job.summary };

        for (let batchIndex = startBatchIndex; batchIndex < totalBatches; batchIndex++) {
          if (cancelledJobs.has(jobId)) {
            await storage.updateJobStatus(shopId, jobId, 'INTERRUPTED');
            activeJobs.delete(shopId);
            broker.publish(shopId, {
              type: 'JOB_INTERRUPTED',
              shop_id: shopId,
              job_id: jobId,
              status: 'INTERRUPTED',
              committed_batch_index: batchIndex - 1,
              total_batches: totalBatches,
              summary: cumulativeSummary,
              reason: 'cancelled by user request',
            });
            return;
          }

          const batch = generator.generateBatch(jobId, batchIndex);
          const delta = await engine.executeBatch(shopId, batch, cumulativeSummary);
          cumulativeSummary.added += delta.added;
          cumulativeSummary.updated += delta.updated;
          cumulativeSummary.withdrawn += delta.withdrawn;
          cumulativeSummary.skipped += delta.skipped;
          cumulativeSummary.dead_letter += delta.dead_letter;

          broker.publish(shopId, {
            type: 'BATCH_COMMITTED',
            shop_id: shopId,
            job_id: jobId,
            batch_index: batchIndex,
            total_batches: totalBatches,
            batch_summary: delta,
            cumulative_summary: { ...cumulativeSummary },
          });
        }

        const completedAt = new Date().toISOString();
        await storage.updateJobStatus(shopId, jobId, 'COMPLETED', completedAt);
        activeJobs.delete(shopId);

        broker.publish(shopId, {
          type: 'JOB_COMPLETED',
          shop_id: shopId,
          job_id: jobId,
          status: 'COMPLETED',
          total_batches: totalBatches,
          summary: cumulativeSummary,
          completed_at: completedAt,
        });
      } catch (err: any) {
        await storage.updateJobStatus(shopId, jobId, 'FAILED');
        activeJobs.delete(shopId);
        broker.publish(shopId, {
          type: 'JOB_FAILED',
          shop_id: shopId,
          job_id: jobId,
          error: err?.message || 'internal error on resume',
        });
      }
    })()
  );

  return c.json(
    {
      job_id: jobId,
      status: 'RUNNING',
      total_batches: totalBatches,
      committed_batch_index: job.committed_batch_index,
      summary: job.summary,
      created_at: job.created_at,
      updated_at: now,
    },
    202
  );
});

app.post('/api/jobs/:id/cancel', async (c) => {
  const shopId = getTenantShopId(c);
  const jobId = c.req.param('id');
  const storage = new D1Storage(c.env.DB);

  cancelledJobs.add(jobId);
  await storage.updateJobStatus(shopId, jobId, 'INTERRUPTED');
  activeJobs.delete(shopId);

  return c.json({
    status: 'cancelling',
    job_id: jobId,
    message: 'cancellation signal sent successfully',
  });
});

app.get('/api/jobs', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const limit = Math.max(1, parseInt(c.req.query('limit') || '20', 10));
  const offset = Math.max(0, parseInt(c.req.query('offset') || '0', 10));
  const status = c.req.query('status');

  const res = await storage.listJobs(shopId, limit, offset, status);
  return c.json({
    jobs: res.jobs,
    total: res.total,
    limit,
    offset,
  });
});

app.get('/api/jobs/:id', async (c) => {
  const shopId = getTenantShopId(c);
  const jobId = c.req.param('id');
  const storage = new D1Storage(c.env.DB);

  const job = await storage.getJob(shopId, jobId);
  if (!job) {
    return c.json({ error: `sync job not found: ${jobId}`, code: 'JOB_NOT_FOUND' }, 404);
  }

  const checkpoints = await storage.getBatchCheckpoints(shopId, jobId);
  return c.json({ job, checkpoints });
});

// --- Products Endpoints ---

app.get('/api/products', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const limit = Math.max(1, parseInt(c.req.query('limit') || '20', 10));
  const offset = Math.max(0, parseInt(c.req.query('offset') || '0', 10));
  const status = c.req.query('status');
  const search = c.req.query('search');

  const res = await storage.listProducts(shopId, status, search, limit, offset);
  return c.json({
    products: res.products,
    total: res.total,
    limit,
    offset,
  });
});

// --- Dead Letter Endpoints ---

app.get('/api/dead-letter', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const limit = Math.max(1, parseInt(c.req.query('limit') || '20', 10));
  const offset = Math.max(0, parseInt(c.req.query('offset') || '0', 10));
  const jobId = c.req.query('job_id');
  const reason = c.req.query('reason');

  const res = await storage.listDeadLetterRecords(shopId, jobId, reason, limit, offset);
  return c.json({
    issues: res.issues,
    total: res.total,
    limit,
    offset,
  });
});

app.post('/api/dead-letter/:id/resolve', async (c) => {
  const shopId = getTenantShopId(c);
  const issueId = c.req.param('id');
  const storage = new D1Storage(c.env.DB);

  let body: any = {};
  try {
    body = await c.req.json();
  } catch {
    return c.json({ error: 'malformed JSON body', code: 'INVALID_REQUEST' }, 400);
  }

  const sku = (body.sku || '').trim();
  if (!sku) {
    return c.json({ error: 'sku cannot be empty', code: 'INVALID_REQUEST' }, 400);
  }
  const price = Number(body.price);
  if (isNaN(price) || price < 0 || !isFinite(price)) {
    return c.json({ error: 'price must be a finite non-negative number', code: 'INVALID_REQUEST' }, 400);
  }
  const status = body.status as ProductStatus;
  if (status !== 'ACTIVE' && status !== 'WITHDRAWN') {
    return c.json({ error: 'status must be ACTIVE or WITHDRAWN', code: 'INVALID_REQUEST' }, 400);
  }

  const name = (body.name || 'Remediated Product').trim();
  const timestamp = body.timestamp ? new Date(body.timestamp).toISOString() : new Date().toISOString();
  const updatedProduct: Product = {
    sku,
    name,
    price,
    status,
    supplier_timestamp: timestamp,
    updated_at: new Date().toISOString(),
  };

  const resolved = await storage.resolveDeadLetter(shopId, issueId, updatedProduct);
  if (!resolved) {
    return c.json({ error: 'dead letter issue not found', code: 'NOT_FOUND' }, 404);
  }

  return c.json({
    status: 'success',
    message: 'product resolved and imported to catalog',
    product: updatedProduct,
  });
});

// --- Metrics Endpoints ---

app.get('/api/metrics', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  let timeRange = (c.req.query('time_range') || '').toLowerCase().trim();
  const jobId = (c.req.query('job_id') || '').trim();
  let from = (c.req.query('from') || '').trim();
  let to = (c.req.query('to') || '').trim();

  if (!timeRange && !jobId && !from) {
    timeRange = 'today';
  }

  if (!jobId && !from && !to) {
    const now = new Date();
    if (timeRange === 'today') {
      from = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate())).toISOString();
      to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate(), 23, 59, 59, 999)).toISOString();
    } else if (timeRange === 'yesterday') {
      from = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - 1)).toISOString();
      to = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate() - 1, 23, 59, 59, 999)).toISOString();
    } else if (timeRange === '7d' || timeRange === 'last7days') {
      from = new Date(now.getTime() - 7 * 86400000).toISOString();
      to = now.toISOString();
    }
  }

  const { metrics, sync_jobs } = await storage.getMetrics(shopId, {
    time_range: timeRange,
    job_id: jobId || undefined,
    from: from || undefined,
    to: to || undefined,
  });

  return c.json({
    filter: {
      time_range: timeRange,
      job_id: jobId,
      from,
      to,
    },
    metrics,
    sync_jobs,
  });
});

// --- Catalog Versions & Rollback Endpoints ---

app.get('/api/versions', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const limit = parseInt(c.req.query('limit') || '20', 10);
  const offset = parseInt(c.req.query('offset') || '0', 10);

  const { versions, total } = await storage.listVersions(shopId, limit, offset);
  return c.json({
    versions,
    total,
    limit,
    offset,
  });
});

app.get('/api/versions/:id/diff', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const versionId = c.req.param('id');

  const version = await storage.getVersion(shopId, versionId);
  if (!version) {
    return c.json({ error: `version ${versionId} not found`, code: 'NOT_FOUND' }, 404);
  }

  const deltas = await storage.getVersionDeltas(shopId, versionId);
  const changes = deltas.map((d) => ({
    sku: d.sku,
    action: d.action,
    old_name: d.old_name,
    new_name: d.new_name,
    old_price: d.old_price,
    new_price: d.new_price,
    old_status: d.old_status,
    new_status: d.new_status,
  }));

  return c.json({
    version,
    changes,
  });
});

app.post('/api/versions/:id/rollback', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);
  const targetVersionId = c.req.param('id');

  let body: any = {};
  try {
    body = await c.req.json();
  } catch {
    body = {};
  }

  const operator = body.operator || body.author || 'staff';
  const description = body.description || '';

  try {
    const result = await storage.rollbackVersion(shopId, targetVersionId, operator, description);

    // Notify real-time subscribers via SSE broker
    broker.publish(shopId, {
      type: 'version_rollback',
      data: {
        shop_id: shopId,
        reverted_version_id: targetVersionId,
        new_version_id: result.version.version_id,
        new_version_number: result.version.version_number,
        items_affected: result.version.total_items_affected,
        author: operator,
        timestamp: result.version.created_at,
      },
    });

    return c.json({
      status: 'success',
      version: result.version,
      deltas: result.deltas,
    });
  } catch (err: any) {
    if (err.message && err.message.includes('not found')) {
      return c.json({ error: err.message, code: 'NOT_FOUND' }, 404);
    }
    return c.json({ error: err.message || 'failed to rollback version', code: 'INTERNAL_ERROR' }, 500);
  }
});

// --- Database Reset ---

app.post('/api/database/reset', async (c) => {
  const shopId = getTenantShopId(c);
  const storage = new D1Storage(c.env.DB);

  if (activeJobs.has(shopId)) {
    const activeJobId = activeJobs.get(shopId);
    if (activeJobId) {
      cancelledJobs.add(activeJobId);
      try {
        await storage.updateJobStatus(shopId, activeJobId, 'INTERRUPTED');
      } catch (_) {}
    }
    activeJobs.delete(shopId);
  }

  await storage.resetDatabase(shopId);
  return c.json({
    status: 'success',
    message: 'database reset to clean state',
  });
});

export default app;
