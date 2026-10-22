import { Batch, GeneratorConfig, ProductStatus, SupplierUpdate } from './types';

const ADJECTIVES = [
  'Pro', 'Ultra', 'Eco', 'Smart', 'Classic', 'Premium', 'Compact', 'Wireless',
  'Ergonomic', 'Heavy-Duty', 'Portable', 'Industrial', 'Advanced', 'Modular',
  'Precision', 'Quantum', 'Hyper', 'Apex', 'Omni', 'Vanguard',
];

const NOUNS = [
  'Keyboard', 'Monitor', 'Headphones', 'Desk', 'Chair', 'Mouse', 'Cable',
  'Hub', 'Stand', 'Adapter', 'Speaker', 'Webcam', 'Microphone', 'Pad',
  'Sensor', 'Switch', 'Router', 'Storage', 'Display', 'Controller',
];

// Linear congruential / Mulberry32 deterministic PRNG
class DeterministicRNG {
  private s: number;

  constructor(seed: number) {
    this.s = (seed >>> 0) || 123456789;
  }

  // Returns float in [0, 1)
  random(): number {
    let t = (this.s += 0x6d2b79f5);
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  }

  intRange(min: number, max: number): number {
    return Math.floor(this.random() * (max - min)) + min;
  }
}

interface ReservoirItem {
  sku: string;
  name: string;
  price: number;
  status: ProductStatus;
  timestamp: string;
}

export class BatchGenerator {
  private cfg: GeneratorConfig;
  private rng: DeterministicRNG;
  private reservoir: ReservoirItem[] = [];
  private reservoirHead = 0;
  private skuCounter = 0;
  private currentTimeMs: number;

  constructor(cfg: Partial<GeneratorConfig>) {
    this.cfg = {
      seed: cfg.seed ?? 42,
      total_batches: cfg.total_batches ?? 10,
      batch_size: cfg.batch_size ?? 100,
      update_ratio: cfg.update_ratio ?? 0.20,
      duplicate_ratio: cfg.duplicate_ratio ?? 0.05,
      out_of_order_ratio: cfg.out_of_order_ratio ?? 0.10,
      withdrawn_ratio: cfg.withdrawn_ratio ?? 0.05,
      invalid_ratio: cfg.invalid_ratio ?? 0.05,
      reservoir_capacity: cfg.reservoir_capacity ?? 1000,
      base_time: cfg.base_time ?? '2026-01-01T00:00:00.000Z',
    };
    this.rng = new DeterministicRNG(this.cfg.seed);
    this.currentTimeMs = new Date(this.cfg.base_time!).getTime();
  }

  generateBatch(jobId: string, batchIndex: number): Batch {
    const records: SupplierUpdate[] = [];
    const thInvalid = this.cfg.invalid_ratio;
    const thDup = thInvalid + this.cfg.duplicate_ratio;
    const thOOO = thDup + this.cfg.out_of_order_ratio;
    const thWithdrawn = thOOO + this.cfg.withdrawn_ratio;
    const thUpdate = thWithdrawn + this.cfg.update_ratio;

    for (let i = 0; i < this.cfg.batch_size; i++) {
      // Advance supplier clock 100ms - 1000ms
      this.currentTimeMs += 100 + this.rng.intRange(0, 900);
      const p = this.rng.random();

      if (p < thInvalid) {
        records.push(this.generateInvalidRecord());
      } else if (p < thDup && this.reservoir.length > 0) {
        records.push(this.generateDuplicateRecord(records));
      } else if (p < thOOO && this.reservoir.length > 0) {
        records.push(this.generateOutOfOrderRecord());
      } else if (p < thWithdrawn && this.reservoir.length > 0) {
        records.push(this.generateWithdrawnRecord());
      } else if (p < thUpdate && this.reservoir.length > 0) {
        records.push(this.generateForwardUpdateRecord());
      } else {
        records.push(this.generateNewProductRecord());
      }
    }

    return {
      job_id: jobId,
      batch_index: batchIndex,
      records,
    };
  }

  private pushReservoir(item: ReservoirItem) {
    if (this.reservoir.length < this.cfg.reservoir_capacity) {
      this.reservoir.push(item);
    } else {
      this.reservoir[this.reservoirHead] = item;
      this.reservoirHead = (this.reservoirHead + 1) % this.cfg.reservoir_capacity;
    }
  }

  private sampleReservoir(): { item: ReservoirItem; index: number } {
    const index = this.rng.intRange(0, this.reservoir.length);
    return { item: this.reservoir[index], index };
  }

  private generateNewProductRecord(): SupplierUpdate {
    this.skuCounter++;
    const sku = `SKU-${String(this.skuCounter).padStart(8, '0')}`;
    const adj = ADJECTIVES[this.rng.intRange(0, ADJECTIVES.length)];
    const noun = NOUNS[this.rng.intRange(0, NOUNS.length)];
    const name = `${adj} ${noun}`;
    const price = Math.round((5.0 + this.rng.random() * 990.0) * 100) / 100;
    const status: ProductStatus = 'ACTIVE';
    const timestamp = new Date(this.currentTimeMs).toISOString();

    const record: SupplierUpdate = { sku, name, price, status, timestamp };
    this.pushReservoir({ sku, name, price, status, timestamp });
    return record;
  }

  private generateForwardUpdateRecord(): SupplierUpdate {
    const { item, index } = this.sampleReservoir();
    const delta = (this.rng.random() * 0.30 - 0.15) * item.price;
    let newPrice = Math.round((item.price + delta) * 100) / 100;
    if (newPrice <= 0 || Math.abs(newPrice - item.price) < 1e-4) {
      newPrice = item.price + 1.0;
    }
    const timestamp = new Date(this.currentTimeMs).toISOString();
    const record: SupplierUpdate = {
      sku: item.sku,
      name: item.name,
      price: newPrice,
      status: 'ACTIVE',
      timestamp,
    };
    this.reservoir[index] = { ...record };
    return record;
  }

  private generateDuplicateRecord(currentBatch: SupplierUpdate[]): SupplierUpdate {
    if (currentBatch.length > 0 && this.rng.intRange(0, 2) === 0) {
      const idx = this.rng.intRange(0, currentBatch.length);
      return { ...currentBatch[idx] };
    }
    const { item } = this.sampleReservoir();
    return {
      sku: item.sku,
      name: item.name,
      price: item.price,
      status: item.status,
      timestamp: item.timestamp,
    };
  }

  private generateOutOfOrderRecord(): SupplierUpdate {
    const { item } = this.sampleReservoir();
    const timeDiffMs = (this.rng.intRange(0, 3600) + 10) * 1000;
    const oldTimestamp = new Date(new Date(item.timestamp).getTime() - timeDiffMs).toISOString();
    let alteredPrice = Math.round(item.price * 0.90 * 100) / 100;
    if (alteredPrice <= 0 || Math.abs(alteredPrice - item.price) < 1e-4) {
      alteredPrice = item.price + 2.0;
    }
    return {
      sku: item.sku,
      name: item.name,
      price: alteredPrice,
      status: item.status,
      timestamp: oldTimestamp,
    };
  }

  private generateWithdrawnRecord(): SupplierUpdate {
    const { item, index } = this.sampleReservoir();
    const timestamp = new Date(this.currentTimeMs).toISOString();
    const record: SupplierUpdate = {
      sku: item.sku,
      name: item.name,
      price: item.price,
      status: 'WITHDRAWN',
      timestamp,
    };
    this.reservoir[index] = { ...record };
    return record;
  }

  private generateInvalidRecord(): SupplierUpdate {
    const defectType = this.rng.intRange(0, 6);
    let sku = `SKU-INV-${String(this.rng.intRange(0, 100000)).padStart(6, '0')}`;
    const name = 'Corrupt Product';
    let price = 99.99;
    let status = 'ACTIVE' as ProductStatus;
    let timestamp = new Date(this.currentTimeMs).toISOString();

    switch (defectType) {
      case 0:
        sku = this.rng.intRange(0, 2) === 0 ? '' : '   \t\n';
        break;
      case 1:
        price = -(Math.round((1.0 + this.rng.random() * 500.0) * 100) / 100);
        break;
      case 2:
        price = NaN;
        break;
      case 3:
        price = this.rng.intRange(0, 2) === 0 ? Infinity : -Infinity;
        break;
      case 4:
        status = 'DISCONTINUED' as any;
        break;
      case 5:
        timestamp = '';
        break;
    }

    return { sku, name, price, status, timestamp };
  }
}
