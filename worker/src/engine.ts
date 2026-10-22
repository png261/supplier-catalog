import { Batch, BatchCheckpoint, ChangeSummary, DeadLetterRecord, Product, SupplierUpdate } from './types';
import { D1Storage } from './storage';
import { broker } from './sse';

export function isInvalidRecord(record: SupplierUpdate): string | null {
  if (!record.sku || record.sku.trim() === '') {
    return 'sku cannot be empty';
  }
  if (record.price < 0 || isNaN(record.price) || !isFinite(record.price)) {
    return 'price cannot be negative or invalid number';
  }
  if (record.status !== 'ACTIVE' && record.status !== 'WITHDRAWN') {
    return 'status must be ACTIVE or WITHDRAWN';
  }
  if (!record.timestamp || record.timestamp.trim() === '' || isNaN(new Date(record.timestamp).getTime())) {
    return 'timestamp must not be zero or invalid';
  }
  return null;
}

export function isIdentical(existing: Product, record: SupplierUpdate): boolean {
  if (existing.sku !== record.sku) return false;
  if (existing.name !== record.name) return false;
  if (Math.abs(existing.price - record.price) > 1e-6) return false;
  if (existing.status !== record.status) return false;

  const existingTime = new Date(existing.supplier_timestamp).getTime();
  const recordTime = new Date(record.timestamp).getTime();
  return existingTime === recordTime;
}

export class SyncEngine {
  constructor(private db: D1Database, private storage: D1Storage) {}

  async executeBatch(
    shopId: string,
    batch: Batch,
    cumulativeSummary: ChangeSummary
  ): Promise<ChangeSummary> {
    if (batch.records.length === 0) {
      return { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 };
    }

    const delta: ChangeSummary = { added: 0, updated: 0, withdrawn: 0, skipped: 0, dead_letter: 0 };
    const batchState = new Map<string, Product | null>();
    const statements: D1PreparedStatement[] = [];
    const now = new Date().toISOString();

    for (let i = 0; i < batch.records.length; i++) {
      const record = batch.records[i];
      const sku = record.sku?.trim() ?? '';

      // Invariant D: Dead-Letter Isolation
      const invalidReason = isInvalidRecord(record);
      if (invalidReason) {
        delta.dead_letter++;
        const issueId = `dl-${batch.job_id}-${batch.batch_index}-${i}-${Date.now()}`;
        const deadLetter: DeadLetterRecord = {
          issue_id: issueId,
          job_id: batch.job_id,
          batch_index: batch.batch_index,
          sku,
          raw_payload: JSON.stringify(record),
          reason: invalidReason,
          created_at: now,
        };
        statements.push(this.storage.buildRecordDeadLetterStmt(shopId, deadLetter));
        continue;
      }

      // Resolve existing product state
      let existingProduct: Product | null | undefined = batchState.get(sku);
      if (existingProduct === undefined) {
        existingProduct = await this.storage.getProduct(shopId, sku);
        batchState.set(sku, existingProduct);
      }

      const recordTime = new Date(record.timestamp).getTime();

      // Case 1: New product insertion
      if (!existingProduct) {
        const newProduct: Product = {
          sku,
          name: record.name,
          price: record.price,
          status: record.status,
          supplier_timestamp: record.timestamp,
          updated_at: now,
        };
        statements.push(this.storage.buildUpsertProductStmt(shopId, newProduct));
        batchState.set(sku, newProduct);
        delta.added++;
        continue;
      }

      // Invariant A: Deterministic Idempotency
      if (isIdentical(existingProduct, record)) {
        delta.skipped++;
        continue;
      }

      // Invariant C: Tombstone Preservation (cannot reactivate WITHDRAWN)
      if (existingProduct.status === 'WITHDRAWN' && record.status === 'ACTIVE') {
        delta.skipped++;
        continue;
      }

      // Invariant B: Fast-Forward Timestamps (T_new <= T_old rejected)
      const existingTime = new Date(existingProduct.supplier_timestamp).getTime();
      if (recordTime <= existingTime) {
        delta.skipped++;
        continue;
      }

      // Valid forward update
      const wasWithdrawn = existingProduct.status === 'WITHDRAWN';
      const nowWithdrawn = record.status === 'WITHDRAWN';

      const updatedProduct: Product = {
        sku,
        name: record.name,
        price: record.price,
        status: record.status,
        supplier_timestamp: record.timestamp,
        updated_at: now,
      };

      statements.push(this.storage.buildUpsertProductStmt(shopId, updatedProduct));
      batchState.set(sku, updatedProduct);

      if (!wasWithdrawn && nowWithdrawn) {
        delta.withdrawn++;
      } else {
        delta.updated++;
      }
    }

    // Invariant E: Atomic Batch Checkpoint & Progress Commit
    const checkpoint: BatchCheckpoint = {
      job_id: batch.job_id,
      batch_index: batch.batch_index,
      items_count: batch.records.length,
      summary: { ...delta },
      committed_at: now,
    };

    statements.push(this.storage.buildRecordCheckpointStmt(shopId, checkpoint));
    statements.push(
      this.storage.buildUpdateJobProgressStmt(
        shopId,
        batch.job_id,
        batch.batch_index,
        delta,
        now
      )
    );

    // Atomically execute entire batch transaction in D1!
    if (statements.length > 0) {
      await this.db.batch(statements);
    }

    return delta;
  }
}
