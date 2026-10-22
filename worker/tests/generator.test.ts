import { describe, it, expect } from 'vitest';
import { BatchGenerator } from '../src/generator';

describe('BatchGenerator', () => {
  it('generates reproducible batches with given seed', () => {
    const gen1 = new BatchGenerator({ seed: 42, batch_size: 10 });
    const gen2 = new BatchGenerator({ seed: 42, batch_size: 10 });

    const batch1 = gen1.generateBatch('job-1', 0);
    const batch2 = gen2.generateBatch('job-1', 0);

    expect(batch1.records.length).toBe(10);
    expect(batch1.records).toEqual(batch2.records);
  });

  it('generates corrupt records when invalid_ratio > 0', () => {
    const gen = new BatchGenerator({
      seed: 99,
      batch_size: 50,
      invalid_ratio: 0.30,
    });
    const batch = gen.generateBatch('job-2', 0);

    let invalidCount = 0;
    for (const r of batch.records) {
      if (!r.sku || r.price < 0 || isNaN(r.price) || !isFinite(r.price) || r.status !== 'ACTIVE' || !r.timestamp) {
        invalidCount++;
      }
    }
    expect(invalidCount).toBeGreaterThan(0);
  });
});
