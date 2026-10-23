import { createRequire } from 'node:module';
import * as fs from 'node:fs';
import * as path from 'node:path';

const require = createRequire(import.meta.url);
const { DatabaseSync } = require('node:sqlite');

export class MockD1PreparedStatement {
  constructor(private db: DatabaseSync, private sql: string, private params: any[] = []) {}

  bind(...args: any[]) {
    return new MockD1PreparedStatement(this.db, this.sql, args);
  }

  async first<T = unknown>(colName?: string): Promise<T | null> {
    const stmt = this.db.prepare(this.sql);
    const row = stmt.get(...this.params) as any;
    if (!row) return null;
    if (colName) return row[colName] ?? null;
    return row as T;
  }

  async all<T = unknown>(): Promise<{ results: T[]; success: boolean }> {
    const stmt = this.db.prepare(this.sql);
    const results = stmt.all(...this.params) as T[];
    return { results, success: true };
  }

  async run(): Promise<{ success: boolean; meta: any }> {
    const stmt = this.db.prepare(this.sql);
    const res = stmt.run(...this.params);
    return { success: true, meta: res };
  }

  execute() {
    const stmt = this.db.prepare(this.sql);
    return stmt.run(...this.params);
  }
}

export class MockD1Database {
  private db: DatabaseSync;

  constructor() {
    this.db = new DatabaseSync(':memory:');
    this.initSchema();
  }

  private initSchema() {
    const migrationsDir = path.resolve(__dirname, '../../migrations');
    const files = fs.readdirSync(migrationsDir).filter((f) => f.endsWith('.sql')).sort();
    for (const file of files) {
      const sql = fs.readFileSync(path.join(migrationsDir, file), 'utf-8');
      this.db.exec(sql);
    }
  }

  prepare(query: string): MockD1PreparedStatement {
    return new MockD1PreparedStatement(this.db, query);
  }

  async batch(statements: MockD1PreparedStatement[]) {
    this.db.exec('BEGIN IMMEDIATE;');
    try {
      const results = [];
      for (const stmt of statements) {
        results.push(stmt.execute());
      }
      this.db.exec('COMMIT;');
      return results;
    } catch (err) {
      this.db.exec('ROLLBACK;');
      throw err;
    }
  }

  async exec(query: string) {
    this.db.exec(query);
  }
}
