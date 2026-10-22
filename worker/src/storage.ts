import {
  AggregatedMetrics,
  BatchCheckpoint,
  ChangeSummary,
  DeadLetterRecord,
  JobStatus,
  MetricsFilter,
  Product,
  SyncJob,
} from './types';

export class D1Storage {
  constructor(private db: D1Database) {}

  // --- Products ---

  async getProduct(shopId: string, sku: string): Promise<Product | null> {
    const query = `
      SELECT sku, name, price, status, supplier_timestamp, updated_at
      FROM products
      WHERE shop_id = ? AND sku = ?;
    `;
    const res = await this.db.prepare(query).bind(shopId, sku).first<Product>();
    return res ?? null;
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
      conditions.push('reason LIKE ?');
      args.push(`%${reason}%`);
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

  // --- Maintenance ---

  async resetDatabase(shopId: string): Promise<void> {
    await this.db.batch([
      this.db.prepare('DELETE FROM dead_letter_issues WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM batch_checkpoints WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM sync_jobs WHERE shop_id = ?;').bind(shopId),
      this.db.prepare('DELETE FROM products WHERE shop_id = ?;').bind(shopId),
    ]);
  }
}
