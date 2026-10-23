import {
  AggregatedMetrics,
  BatchCheckpoint,
  CatalogVersion,
  ChangeSummary,
  DeadLetterRecord,
  ItemDelta,
  JobStatus,
  MetricsFilter,
  Product,
  SyncJob,
} from './types';

export class D1Storage {
  constructor(private db: D1Database) {}

  // --- Products ---

  async getMaxSupplierTimestamp(shopId: string): Promise<string | null> {
    const query = `
      SELECT MAX(supplier_timestamp) as max_ts
      FROM products
      WHERE shop_id = ?;
    `;
    const res = await this.db.prepare(query).bind(shopId).first<{ max_ts: string | null }>();
    return res?.max_ts || null;
  }

  async getProduct(shopId: string, sku: string): Promise<Product | null> {
    const query = `
      SELECT sku, name, price, status, supplier_timestamp, updated_at
      FROM products
      WHERE shop_id = ? AND sku = ?;
    `;
    const res = await this.db.prepare(query).bind(shopId, sku).first<Product>();
    return res ?? null;
  }

  async getProductsBySkus(shopId: string, skus: string[]): Promise<Map<string, Product>> {
    const map = new Map<string, Product>();
    if (!skus || skus.length === 0) return map;

    const uniqueSkus = Array.from(new Set(skus.filter((s) => s && s.trim() !== '')));
    const chunkSize = 50;
    for (let i = 0; i < uniqueSkus.length; i += chunkSize) {
      const chunk = uniqueSkus.slice(i, i + chunkSize);
      const placeholders = chunk.map(() => '?').join(', ');
      const query = `
        SELECT sku, name, price, status, supplier_timestamp, updated_at
        FROM products
        WHERE shop_id = ? AND sku IN (${placeholders});
      `;
      const res = await this.db.prepare(query).bind(shopId, ...chunk).all<Product>();
      for (const p of res.results ?? []) {
        map.set(p.sku, p);
      }
    }
    return map;
  }

  async listProducts(
    shopId: string,
    status?: string,
    search?: string,
    limit = 20,
    offset = 0
  ): Promise<{ products: Product[]; total: number }> {
    const conditions: string[] = ['shop_id = ?'];
    const args: any[] = [shopId];

    if (status) {
      conditions.push('status = ?');
      args.push(status);
    }

    if (search) {
      conditions.push('(sku LIKE ? OR name LIKE ?)');
      const pattern = `%${search}%`;
      args.push(pattern, pattern);
    }

    const whereClause = conditions.join(' AND ');

    const countQuery = `SELECT COUNT(*) as total FROM products WHERE ${whereClause};`;
    const countRes = await this.db.prepare(countQuery).bind(...args).first<{ total: number }>();
    const total = countRes?.total ?? 0;

    const selectQuery = `
      SELECT sku, name, price, status, supplier_timestamp, updated_at
      FROM products
      WHERE ${whereClause}
      ORDER BY sku ASC
      LIMIT ? OFFSET ?;
    `;
    const selectArgs = [...args, limit, offset];
    const selectRes = await this.db.prepare(selectQuery).bind(...selectArgs).all<Product>();

    return {
      products: selectRes.results ?? [],
      total,
    };
  }

  buildUpsertProductStmt(shopId: string, p: Product): D1PreparedStatement {
    const query = `
      INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at)
      VALUES (?, ?, ?, ?, ?, ?, ?)
      ON CONFLICT(shop_id, sku) DO UPDATE SET
        name = excluded.name,
        price = excluded.price,
        status = excluded.status,
        supplier_timestamp = excluded.supplier_timestamp,
        updated_at = excluded.updated_at;
    `;
    return this.db.prepare(query).bind(
      shopId,
      p.sku,
      p.name,
      p.price,
      p.status,
      p.supplier_timestamp,
      p.updated_at
    );
  }

  // --- Sync Jobs ---

  async createJob(shopId: string, job: SyncJob): Promise<void> {
    const query = `
      INSERT INTO sync_jobs (
        shop_id, job_id, status, total_batches, committed_batch_index,
        items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
        created_at, updated_at, completed_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
    `;
    await this.db
      .prepare(query)
      .bind(
        shopId,
        job.job_id,
        job.status,
        job.total_batches,
        job.committed_batch_index,
        job.summary.added,
        job.summary.updated,
        job.summary.withdrawn,
        job.summary.skipped,
        job.summary.dead_letter,
        job.created_at,
        job.updated_at,
        job.completed_at ?? null
      )
      .run();
  }

  async getJob(shopId: string, jobId: string): Promise<SyncJob | null> {
    const query = `
      SELECT job_id, status, total_batches, committed_batch_index,
             items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
             created_at, updated_at, completed_at
      FROM sync_jobs
      WHERE shop_id = ? AND job_id = ?;
    `;
    const row = await this.db.prepare(query).bind(shopId, jobId).first<any>();
    if (!row) return null;

    return {
      job_id: row.job_id,
      status: row.status as JobStatus,
      total_batches: row.total_batches,
      committed_batch_index: row.committed_batch_index,
      summary: {
        added: row.items_added,
        updated: row.items_updated,
        withdrawn: row.items_withdrawn,
        skipped: row.items_skipped,
        dead_letter: row.items_dead_letter,
      },
      created_at: row.created_at,
      updated_at: row.updated_at,
      completed_at: row.completed_at,
    };
  }

  async getActiveJob(shopId: string): Promise<SyncJob | null> {
    const query = `
      SELECT job_id, status, total_batches, committed_batch_index,
             items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
             created_at, updated_at, completed_at
      FROM sync_jobs
      WHERE shop_id = ? AND status = 'RUNNING'
      LIMIT 1;
    `;
    const row = await this.db.prepare(query).bind(shopId).first<any>();
    if (!row) return null;

    return {
      job_id: row.job_id,
      status: row.status as JobStatus,
      total_batches: row.total_batches,
      committed_batch_index: row.committed_batch_index,
      summary: {
        added: row.items_added,
        updated: row.items_updated,
        withdrawn: row.items_withdrawn,
        skipped: row.items_skipped,
        dead_letter: row.items_dead_letter,
      },
      created_at: row.created_at,
      updated_at: row.updated_at,
      completed_at: row.completed_at,
    };
  }

  async updateJobStatus(
    shopId: string,
    jobId: string,
    status: JobStatus,
    completedAt?: string | null
  ): Promise<void> {
    const now = new Date().toISOString();
    const query = `
      UPDATE sync_jobs
      SET status = ?, updated_at = ?, completed_at = COALESCE(?, completed_at)
      WHERE shop_id = ? AND job_id = ?;
    `;
    await this.db.prepare(query).bind(status, now, completedAt ?? null, shopId, jobId).run();
  }

  async listJobs(
    shopId: string,
    limit = 20,
    offset = 0,
    statusFilter?: string
  ): Promise<{ jobs: SyncJob[]; total: number }> {
    const conditions = ['shop_id = ?'];
    const args: any[] = [shopId];

    if (statusFilter) {
      conditions.push('status = ?');
      args.push(statusFilter);
    }

    const whereClause = conditions.join(' AND ');
    const countQuery = `SELECT COUNT(*) as total FROM sync_jobs WHERE ${whereClause};`;
    const countRes = await this.db.prepare(countQuery).bind(...args).first<{ total: number }>();
    const total = countRes?.total ?? 0;

    const selectQuery = `
      SELECT job_id, status, total_batches, committed_batch_index,
             items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
             created_at, updated_at, completed_at
      FROM sync_jobs
      WHERE ${whereClause}
      ORDER BY created_at DESC
      LIMIT ? OFFSET ?;
    `;
    const selectArgs = [...args, limit, offset];
    const selectRes = await this.db.prepare(selectQuery).bind(...selectArgs).all<any>();

    const jobs: SyncJob[] = (selectRes.results ?? []).map((row) => ({
      job_id: row.job_id,
      status: row.status as JobStatus,
      total_batches: row.total_batches,
      committed_batch_index: row.committed_batch_index,
      summary: {
        added: row.items_added,
        updated: row.items_updated,
        withdrawn: row.items_withdrawn,
        skipped: row.items_skipped,
        dead_letter: row.items_dead_letter,
      },
      created_at: row.created_at,
      updated_at: row.updated_at,
      completed_at: row.completed_at,
    }));

    return { jobs, total };
  }

  buildUpdateJobProgressStmt(
    shopId: string,
    jobId: string,
    batchIndex: number,
    delta: ChangeSummary,
    updatedAt: string
  ): D1PreparedStatement {
    const query = `
      UPDATE sync_jobs
      SET committed_batch_index = ?,
          items_added = items_added + ?,
          items_updated = items_updated + ?,
          items_withdrawn = items_withdrawn + ?,
          items_skipped = items_skipped + ?,
          items_dead_letter = items_dead_letter + ?,
          updated_at = ?
      WHERE shop_id = ? AND job_id = ?;
    `;
    return this.db.prepare(query).bind(
      batchIndex,
      delta.added,
      delta.updated,
      delta.withdrawn,
      delta.skipped,
      delta.dead_letter,
      updatedAt,
      shopId,
      jobId
    );
  }

  // --- Checkpoints ---

  buildRecordCheckpointStmt(shopId: string, cp: BatchCheckpoint): D1PreparedStatement {
    const query = `
      INSERT INTO batch_checkpoints (
        shop_id, job_id, batch_index, items_count,
        added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count,
        committed_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
    `;
    return this.db.prepare(query).bind(
      shopId,
      cp.job_id,
      cp.batch_index,
      cp.items_count,
      cp.summary.added,
      cp.summary.updated,
      cp.summary.withdrawn,
      cp.summary.skipped,
      cp.summary.dead_letter,
      cp.committed_at
    );
  }

  async getBatchCheckpoints(shopId: string, jobId: string): Promise<BatchCheckpoint[]> {
    const query = `
      SELECT job_id, batch_index, items_count,
             added_count, updated_count, withdrawn_count, skipped_count, dead_letter_count,
             committed_at
      FROM batch_checkpoints
      WHERE shop_id = ? AND job_id = ?
      ORDER BY batch_index ASC;
    `;
    const res = await this.db.prepare(query).bind(shopId, jobId).all<any>();
    return (res.results ?? []).map((row) => ({
      job_id: row.job_id,
      batch_index: row.batch_index,
      items_count: row.items_count,
      summary: {
        added: row.added_count,
        updated: row.updated_count,
        withdrawn: row.withdrawn_count,
        skipped: row.skipped_count,
        dead_letter: row.dead_letter_count,
      },
      committed_at: row.committed_at,
    }));
  }

  // --- Dead Letter Issues ---

  buildRecordDeadLetterStmt(shopId: string, issue: DeadLetterRecord): D1PreparedStatement {
    const query = `
      INSERT INTO dead_letter_issues (
        shop_id, issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?);
    `;
    return this.db.prepare(query).bind(
      shopId,
      issue.issue_id,
      issue.job_id,
      issue.batch_index,
      issue.sku,
      issue.raw_payload,
      issue.reason,
      issue.created_at
    );
  }

  async listDeadLetterRecords(
    shopId: string,
    jobId?: string,
    reason?: string,
    limit = 20,
    offset = 0
  ): Promise<{ issues: DeadLetterRecord[]; total: number }> {
    const conditions = ['shop_id = ?'];
    const args: any[] = [shopId];

    if (jobId) {
      conditions.push('job_id = ?');
      args.push(jobId);
    }
    if (reason) {
      const upper = reason.trim().toUpperCase();
      if (upper === 'NEGATIVE_OR_ZERO_PRICE') {
        conditions.push("(reason LIKE '%price%' OR reason LIKE '%NEGATIVE_OR_ZERO_PRICE%')");
      } else if (upper === 'EMPTY_SKU') {
        conditions.push("(reason LIKE '%sku%' OR reason LIKE '%SKU%' OR reason LIKE '%EMPTY_SKU%')");
      } else if (upper === 'ZERO_OR_MISSING_TIMESTAMP') {
        conditions.push("(reason LIKE '%timestamp%' OR reason LIKE '%ZERO_OR_MISSING_TIMESTAMP%')");
      } else if (upper === 'CORRUPTED_JSON') {
        conditions.push("(reason LIKE '%status%' OR reason LIKE '%malformed%' OR reason LIKE '%CORRUPTED_JSON%')");
      } else {
        conditions.push('reason LIKE ?');
        args.push(`%${reason}%`);
      }
    }

    const whereClause = conditions.join(' AND ');
    const countQuery = `SELECT COUNT(*) as total FROM dead_letter_issues WHERE ${whereClause};`;
    const countRes = await this.db.prepare(countQuery).bind(...args).first<{ total: number }>();
    const total = countRes?.total ?? 0;

    const selectQuery = `
      SELECT issue_id, job_id, batch_index, sku, raw_payload, reason, created_at
      FROM dead_letter_issues
      WHERE ${whereClause}
      ORDER BY created_at DESC
      LIMIT ? OFFSET ?;
    `;
    const selectArgs = [...args, limit, offset];
    const selectRes = await this.db.prepare(selectQuery).bind(...selectArgs).all<DeadLetterRecord>();

    return {
      issues: selectRes.results ?? [],
      total,
    };
  }

  async resolveDeadLetter(shopId: string, issueId: string, p: Product): Promise<boolean> {
    const getIssue = `SELECT issue_id FROM dead_letter_issues WHERE shop_id = ? AND issue_id = ?;`;
    const issue = await this.db.prepare(getIssue).bind(shopId, issueId).first();
    if (!issue) return false;

    const upsertProduct = this.buildUpsertProductStmt(shopId, p);
    const deleteIssue = this.db
      .prepare(`DELETE FROM dead_letter_issues WHERE shop_id = ? AND issue_id = ?;`)
      .bind(shopId, issueId);

    await this.db.batch([upsertProduct, deleteIssue]);
    return true;
  }

  // --- Metrics ---

  async getMetrics(
    shopId: string,
    filter: MetricsFilter
  ): Promise<{ metrics: AggregatedMetrics; sync_jobs: SyncJob[] }> {
    const conditions = ['shop_id = ?'];
    const args: any[] = [shopId];

    if (filter.job_id) {
      conditions.push('job_id = ?');
      args.push(filter.job_id);
    } else {
      if (filter.from) {
        conditions.push('created_at >= ?');
        args.push(filter.from);
      }
      if (filter.to) {
        conditions.push('created_at <= ?');
        args.push(filter.to);
      }
    }

    const whereClause = conditions.join(' AND ');
    const aggQuery = `
      SELECT
        COALESCE(SUM(items_added), 0) as added,
        COALESCE(SUM(items_updated), 0) as updated,
        COALESCE(SUM(items_withdrawn), 0) as withdrawn,
        COALESCE(SUM(items_skipped), 0) as skipped,
        COALESCE(SUM(items_dead_letter), 0) as dead_letter,
        COUNT(*) as total_syncs,
        COALESCE(SUM(CASE WHEN status = 'COMPLETED' THEN 1 ELSE 0 END), 0) as completed_syncs
      FROM sync_jobs
      WHERE ${whereClause};
    `;

    const aggRow = await this.db.prepare(aggQuery).bind(...args).first<any>();

    const summary: ChangeSummary = {
      added: aggRow?.added ?? 0,
      updated: aggRow?.updated ?? 0,
      withdrawn: aggRow?.withdrawn ?? 0,
      skipped: aggRow?.skipped ?? 0,
      dead_letter: aggRow?.dead_letter ?? 0,
    };

    const totalProcessed =
      summary.added + summary.updated + summary.withdrawn + summary.skipped + summary.dead_letter;
    let qualityScore = 100.0;
    if (totalProcessed > 0) {
      const valid = Math.max(0, totalProcessed - summary.dead_letter);
      qualityScore = (valid / totalProcessed) * 100.0;
    }

    // Matching jobs (most recent 100)
    const jobsQuery = `
      SELECT job_id, status, total_batches, committed_batch_index,
             items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
             created_at, updated_at, completed_at
      FROM sync_jobs
      WHERE ${whereClause}
      ORDER BY created_at DESC
      LIMIT 100;
    `;
    const jobsRes = await this.db.prepare(jobsQuery).bind(...args).all<any>();
    const sync_jobs: SyncJob[] = (jobsRes.results ?? []).map((row) => ({
      job_id: row.job_id,
      status: row.status as JobStatus,
      total_batches: row.total_batches,
      committed_batch_index: row.committed_batch_index,
      summary: {
        added: row.items_added,
        updated: row.items_updated,
        withdrawn: row.items_withdrawn,
        skipped: row.items_skipped,
        dead_letter: row.items_dead_letter,
      },
      created_at: row.created_at,
      updated_at: row.updated_at,
      completed_at: row.completed_at,
    }));

    return {
      metrics: {
        summary,
        total_processed: totalProcessed,
        quality_score: qualityScore,
        total_syncs: aggRow?.total_syncs ?? 0,
        completed_syncs: aggRow?.completed_syncs ?? 0,
      },
      sync_jobs,
    };
  }

  // --- Catalog Versions & Rollback ---

  buildUpsertSyncVersionStmt(
    shopId: string,
    jobId: string,
    description: string,
    delta: ChangeSummary,
    affectedCount: number,
    now: string
  ): D1PreparedStatement {
    const query = `
      INSERT INTO catalog_versions (
        shop_id, version_id, version_number, description, author, type,
        items_added, items_updated, items_withdrawn, items_skipped, items_dead_letter,
        total_items_affected, metadata, created_at
      ) VALUES (
        ?, ?,
        COALESCE((SELECT MAX(version_number) FROM catalog_versions WHERE shop_id = ?), 0) + 1,
        ?, 'supplier-sync', 'SYNC',
        ?, ?, ?, ?, ?,
        ?, ?, ?
      )
      ON CONFLICT(shop_id, version_id) DO UPDATE SET
        items_added = catalog_versions.items_added + excluded.items_added,
        items_updated = catalog_versions.items_updated + excluded.items_updated,
        items_withdrawn = catalog_versions.items_withdrawn + excluded.items_withdrawn,
        items_skipped = catalog_versions.items_skipped + excluded.items_skipped,
        items_dead_letter = catalog_versions.items_dead_letter + excluded.items_dead_letter,
        total_items_affected = catalog_versions.total_items_affected + excluded.total_items_affected;
    `;
    return this.db.prepare(query).bind(
      shopId,
      jobId,
      shopId,
      description || `Supplier Catalog Sync (${jobId})`,
      delta.added,
      delta.updated,
      delta.withdrawn,
      delta.skipped,
      delta.dead_letter,
      affectedCount,
      JSON.stringify({ job_id: jobId }),
      now
    );
  }

  buildInsertVersionDeltaStmt(shopId: string, d: ItemDelta): D1PreparedStatement {
    const query = `
      INSERT INTO catalog_version_deltas (
        shop_id, version_id, sku, action,
        old_name, new_name, old_price, new_price,
        old_status, new_status, old_timestamp, new_timestamp
      ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
    `;
    return this.db.prepare(query).bind(
      shopId,
      d.version_id,
      d.sku,
      d.action,
      d.old_name ?? null,
      d.new_name,
      d.old_price ?? null,
      d.new_price,
      d.old_status ?? null,
      d.new_status,
      d.old_timestamp ?? null,
      d.new_timestamp
    );
  }

  async listVersions(
    shopId: string,
    limit = 20,
    offset = 0
  ): Promise<{ versions: CatalogVersion[]; total: number }> {
    const l = Math.min(100, Math.max(1, limit));
    const o = Math.max(0, offset);

    const countRes = await this.db
      .prepare('SELECT COUNT(*) as total FROM catalog_versions WHERE shop_id = ?;')
      .bind(shopId)
      .first<{ total: number }>();
    const total = countRes?.total || 0;

    const query = `
      SELECT shop_id, version_id, version_number, description, author, type,
             revert_of_version_id, items_added, items_updated, items_withdrawn,
             items_skipped, items_dead_letter, total_items_affected, metadata, created_at
      FROM catalog_versions
      WHERE shop_id = ?
      ORDER BY version_number DESC
      LIMIT ? OFFSET ?;
    `;
    const res = await this.db.prepare(query).bind(shopId, l, o).all<CatalogVersion>();
    return {
      versions: res.results || [],
      total,
    };
  }

  async getVersion(shopId: string, versionId: string): Promise<CatalogVersion | null> {
    const query = `
      SELECT shop_id, version_id, version_number, description, author, type,
             revert_of_version_id, items_added, items_updated, items_withdrawn,
             items_skipped, items_dead_letter, total_items_affected, metadata, created_at
      FROM catalog_versions
      WHERE shop_id = ? AND version_id = ?;
    `;
    const found = await this.db.prepare(query).bind(shopId, versionId).first<CatalogVersion>();
    if (found) return found;
    if (versionId.trim() === 'v0') {
      return {
        shop_id: shopId,
        version_id: 'v0',
        version_number: 0,
        description: 'Initial Pre-Sync Baseline (Empty Catalog)',
        author: 'system',
        type: 'INITIAL',
        revert_of_version_id: null,
        items_added: 0,
        items_updated: 0,
        items_withdrawn: 0,
        items_skipped: 0,
        items_dead_letter: 0,
        total_items_affected: 0,
        metadata: '{}',
        created_at: '1970-01-01T00:00:00.000Z',
      };
    }
    return null;
  }

  async getVersionDeltas(shopId: string, versionId: string): Promise<ItemDelta[]> {
    const query = `
      SELECT delta_id, shop_id, version_id, sku, action,
             old_name, new_name, old_price, new_price,
             old_status, new_status, old_timestamp, new_timestamp
      FROM catalog_version_deltas
      WHERE shop_id = ? AND version_id = ?
      ORDER BY delta_id ASC;
    `;
    const res = await this.db.prepare(query).bind(shopId, versionId).all<ItemDelta>();
    return res.results || [];
  }

  async rollbackVersion(
    shopId: string,
    targetVersionId: string,
    author = 'staff',
    description = ''
  ): Promise<{ version: CatalogVersion; deltas: ItemDelta[] }> {
    // 1. Verify target version exists (or materialize v0 initial baseline)
    const targetVer = await this.getVersion(shopId, targetVersionId);
    if (!targetVer) {
      throw new Error(`target version ${targetVersionId} not found`);
    }
    if (targetVersionId.trim() === 'v0') {
      await this.db
        .prepare(`
          INSERT OR IGNORE INTO catalog_versions (
            shop_id, version_id, version_number, description, author, type, created_at
          ) VALUES (?, 'v0', 0, 'Initial Pre-Sync Baseline (Empty Catalog)', 'system', 'INITIAL', '1970-01-01T00:00:00.000Z');
        `)
        .bind(shopId)
        .run();
    }

    // 2. Get latest version number
    const maxVerRes = await this.db
      .prepare('SELECT MAX(version_number) as max_v FROM catalog_versions WHERE shop_id = ?;')
      .bind(shopId)
      .first<{ max_v: number | null }>();
    const nextVerNum = (maxVerRes?.max_v ?? 0) + 1;

    // 3. Reconstruct baseline state at target version using SQL window function
    const baselineQuery = `
      WITH ranked_deltas AS (
        SELECT 
          d.sku,
          d.new_name,
          d.new_price,
          d.new_status,
          d.new_timestamp,
          ROW_NUMBER() OVER (
            PARTITION BY d.sku 
            ORDER BY v.version_number DESC, d.delta_id DESC
          ) AS rn
        FROM catalog_version_deltas d
        JOIN catalog_versions v ON d.shop_id = v.shop_id AND d.version_id = v.version_id
        WHERE d.shop_id = ? AND v.version_number <= ?
      )
      SELECT sku, new_name as name, new_price as price, new_status as status, new_timestamp as supplier_timestamp
      FROM ranked_deltas
      WHERE rn = 1;
    `;
    const baselineRes = await this.db
      .prepare(baselineQuery)
      .bind(shopId, targetVer.version_number)
      .all<{ sku: string; name: string; price: number; status: 'ACTIVE' | 'WITHDRAWN'; supplier_timestamp: string }>();

    const baselineMap = new Map<string, { sku: string; name: string; price: number; status: 'ACTIVE' | 'WITHDRAWN'; supplier_timestamp: string }>();
    for (const b of (baselineRes.results || [])) {
      baselineMap.set(b.sku, b);
    }

    // 4. Get all live products
    const liveRes = await this.db
      .prepare('SELECT sku, name, price, status, supplier_timestamp FROM products WHERE shop_id = ?;')
      .bind(shopId)
      .all<{ sku: string; name: string; price: number; status: 'ACTIVE' | 'WITHDRAWN'; supplier_timestamp: string }>();
    const liveMap = new Map<string, { sku: string; name: string; price: number; status: 'ACTIVE' | 'WITHDRAWN'; supplier_timestamp: string }>();
    for (const p of (liveRes.results || [])) {
      liveMap.set(p.sku, p);
    }

    // 5. Compute inverse deltas
    const rollbackVerId = `v${nextVerNum}-${Date.now().toString(36)}`;
    const nowIso = new Date().toISOString();
    const rollbackDeltas: ItemDelta[] = [];
    const dbStatements: D1PreparedStatement[] = [];

    let itemsUpdated = 0;
    let itemsWithdrawn = 0;
    let itemsAdded = 0;

    // Check products in baseline
    for (const [sku, base] of baselineMap.entries()) {
      const live = liveMap.get(sku);
      if (live) {
        // INVARIANT A: Tombstone Preservation - If currently WITHDRAWN, NEVER reactivate to ACTIVE!
        let effectiveStatus = base.status;
        if (live.status === 'WITHDRAWN') {
          effectiveStatus = 'WITHDRAWN';
        }

        const nameChanged = live.name !== base.name;
        const priceChanged = Math.abs(live.price - base.price) > 0.0001;
        const statusChanged = live.status !== effectiveStatus;

        if (nameChanged || priceChanged || statusChanged) {
          rollbackDeltas.push({
            shop_id: shopId,
            version_id: rollbackVerId,
            sku,
            action: 'UPDATED',
            old_name: live.name,
            new_name: base.name,
            old_price: live.price,
            new_price: base.price,
            old_status: live.status,
            new_status: effectiveStatus,
            old_timestamp: live.supplier_timestamp,
            new_timestamp: nowIso,
          });
          itemsUpdated++;

          dbStatements.push(
            this.db
              .prepare(
                'UPDATE products SET name = ?, price = ?, status = ?, supplier_timestamp = ?, updated_at = ? WHERE shop_id = ? AND sku = ?;'
              )
              .bind(base.name, base.price, effectiveStatus, nowIso, nowIso, shopId, sku)
          );
        }
      } else {
        // Product in baseline but not in live -> Add as ACTIVE
        rollbackDeltas.push({
          shop_id: shopId,
          version_id: rollbackVerId,
          sku,
          action: 'ADDED',
          old_name: null,
          new_name: base.name,
          old_price: null,
          new_price: base.price,
          old_status: null,
          new_status: 'ACTIVE',
          old_timestamp: null,
          new_timestamp: nowIso,
        });
        itemsAdded++;

        dbStatements.push(
          this.db
            .prepare(
              'INSERT INTO products (shop_id, sku, name, price, status, supplier_timestamp, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?);'
            )
            .bind(shopId, sku, base.name, base.price, 'ACTIVE', nowIso, nowIso)
        );
      }
    }

    // Check products introduced after target version
    for (const [sku, live] of liveMap.entries()) {
      if (!baselineMap.has(sku) && live.status !== 'WITHDRAWN') {
        rollbackDeltas.push({
          shop_id: shopId,
          version_id: rollbackVerId,
          sku,
          action: 'WITHDRAWN',
          old_name: live.name,
          new_name: live.name,
          old_price: live.price,
          new_price: live.price,
          old_status: live.status,
          new_status: 'WITHDRAWN',
          old_timestamp: live.supplier_timestamp,
          new_timestamp: nowIso,
        });
        itemsWithdrawn++;

        dbStatements.push(
          this.db
            .prepare(
              'UPDATE products SET status = ?, supplier_timestamp = ?, updated_at = ? WHERE shop_id = ? AND sku = ?;'
            )
            .bind('WITHDRAWN', nowIso, nowIso, shopId, sku)
        );
      }
    }

    const totalAffected = itemsAdded + itemsUpdated + itemsWithdrawn;
    const finalDesc = description || `Revert to version v${targetVer.version_number} (${targetVer.version_id})`;

    const rollbackVer: CatalogVersion = {
      shop_id: shopId,
      version_id: rollbackVerId,
      version_number: nextVerNum,
      description: finalDesc,
      author: author || 'staff',
      type: 'ROLLBACK',
      revert_of_version_id: targetVersionId,
      items_added: itemsAdded,
      items_updated: itemsUpdated,
      items_withdrawn: itemsWithdrawn,
      items_skipped: 0,
      items_dead_letter: 0,
      total_items_affected: totalAffected,
      metadata: JSON.stringify({ reverted_version_number: targetVer.version_number }),
      created_at: nowIso,
    };

    // Prepare version row insert
    dbStatements.push(
      this.db
        .prepare(`
          INSERT INTO catalog_versions (
            shop_id, version_id, version_number, description, author, type,
            revert_of_version_id, items_added, items_updated, items_withdrawn,
            items_skipped, items_dead_letter, total_items_affected, metadata, created_at
          ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
        `)
        .bind(
          shopId,
          rollbackVer.version_id,
          rollbackVer.version_number,
          rollbackVer.description,
          rollbackVer.author,
          rollbackVer.type,
          rollbackVer.revert_of_version_id,
          rollbackVer.items_added,
          rollbackVer.items_updated,
          rollbackVer.items_withdrawn,
          rollbackVer.items_skipped,
          rollbackVer.items_dead_letter,
          rollbackVer.total_items_affected,
          rollbackVer.metadata,
          rollbackVer.created_at
        )
    );

    // Prepare deltas insert
    for (const d of rollbackDeltas) {
      dbStatements.push(
        this.db
          .prepare(`
            INSERT INTO catalog_version_deltas (
              shop_id, version_id, sku, action,
              old_name, new_name, old_price, new_price,
              old_status, new_status, old_timestamp, new_timestamp
            ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);
          `)
          .bind(
            shopId,
            d.version_id,
            d.sku,
            d.action,
            d.old_name ?? null,
            d.new_name,
            d.old_price ?? null,
            d.new_price,
            d.old_status ?? null,
            d.new_status,
            d.old_timestamp ?? null,
            d.new_timestamp
          )
      );
    }

    // Execute in chunks of 50 to respect D1 batch limits
    for (let i = 0; i < dbStatements.length; i += 50) {
      const chunk = dbStatements.slice(i, i + 50);
      await this.db.batch(chunk);
    }

    return {
      version: rollbackVer,
      deltas: rollbackDeltas,
    };
  }

  // --- Maintenance ---

  async resetDatabase(shopId: string): Promise<void> {
    await this.db.batch([
      this.db.prepare('DELETE FROM catalog_version_deltas WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM catalog_versions WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM dead_letter_issues WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM batch_checkpoints WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM sync_jobs WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM products WHERE shop_id = ?;').bind(shopId),
    ]);
  }
}
