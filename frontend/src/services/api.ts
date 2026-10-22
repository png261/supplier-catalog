import {
  CatalogProduct,
  DeadLetterIssue,
  SyncJob,
  AnalyticsResponse,
} from '../types';

const SHOP_STORAGE_KEY = 'coderpush_shop_id';
const RECENT_SHOPS_KEY = 'coderpush_recent_shops';

export function getActiveShopId(): string {
  if (typeof window !== 'undefined') {
    const params = new URLSearchParams(window.location.search);
    const fromUrl = params.get('shop') || params.get('shop_id');
    if (fromUrl && fromUrl.trim()) {
      const clean = fromUrl.trim().toLowerCase();
      localStorage.setItem(SHOP_STORAGE_KEY, clean);
      saveRecentShop(clean);
      return clean;
    }
    const saved = localStorage.getItem(SHOP_STORAGE_KEY);
    if (saved && saved.trim()) {
      const clean = saved.trim().toLowerCase();
      saveRecentShop(clean);
      // Sync URL
      const url = new URL(window.location.href);
      if (!url.searchParams.has('shop')) {
        url.searchParams.set('shop', clean);
        window.history.replaceState(null, '', url.toString());
      }
      return clean;
    }
    // No shop selected yet
    return '';
  }
  return '';
}

export function setActiveShopId(shopId: string): void {
  const clean = shopId.trim().toLowerCase();
  localStorage.setItem(SHOP_STORAGE_KEY, clean);
  saveRecentShop(clean);
  const url = new URL(window.location.href);
  url.searchParams.set('shop', clean);
  window.history.pushState(null, '', url.toString());
}

export function clearActiveShopId(): void {
  if (typeof window !== 'undefined') {
    localStorage.removeItem(SHOP_STORAGE_KEY);
    const url = new URL(window.location.href);
    url.searchParams.delete('shop');
    url.searchParams.delete('shop_id');
    window.history.pushState(null, '', url.pathname + (url.search ? url.search : ''));
  }
}

export function getRecentShops(): string[] {
  try {
    const raw = localStorage.getItem(RECENT_SHOPS_KEY);
    if (!raw) return [];
    return JSON.parse(raw);
  } catch {
    return [];
  }
}

export function saveRecentShop(shopId: string): void {
  try {
    const list = getRecentShops().filter((s) => s !== shopId);
    list.unshift(shopId);
    localStorage.setItem(RECENT_SHOPS_KEY, JSON.stringify(list.slice(0, 5)));
  } catch {}
}

export function getSSEUrl(): string {
  const shopId = getActiveShopId();
  return shopId ? `/api/events?shop=${encodeURIComponent(shopId)}` : '/api/events';
}

async function shopFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const shopId = getActiveShopId();
  const headers = new Headers(init?.headers || {});
  if (shopId && !headers.has('X-Shop-Id')) {
    headers.set('X-Shop-Id', shopId);
  }
  return fetch(input, {
    ...init,
    headers,
  });
}

export async function fetchCatalog(
  limit = 20,
  offset = 0,
  search = '',
  status = ''
): Promise<{ products: CatalogProduct[]; total: number }> {
  let url = `/api/products?limit=${limit}&offset=${offset}`;
  if (search) url += `&search=${encodeURIComponent(search)}`;
  if (status) url += `&status=${encodeURIComponent(status)}`;
  const res = await shopFetch(url);
  if (!res.ok) throw new Error(`Failed to fetch catalog: ${res.statusText}`);
  return res.json();
}

export async function fetchDLQ(
  limit = 20,
  offset = 0,
  reason = ''
): Promise<{ issues: DeadLetterIssue[]; total: number }> {
  let url = `/api/dead-letter?limit=${limit}&offset=${offset}`;
  if (reason) url += `&reason=${encodeURIComponent(reason)}`;
  const res = await shopFetch(url);
  if (!res.ok) throw new Error(`Failed to fetch DLQ: ${res.statusText}`);
  return res.json();
}

export async function fetchHistory(
  limit = 20,
  offset = 0
): Promise<{ jobs: SyncJob[]; total: number }> {
  const res = await shopFetch(`/api/jobs?limit=${limit}&offset=${offset}`);
  if (!res.ok) throw new Error(`Failed to fetch history: ${res.statusText}`);
  return res.json();
}

export async function fetchInterruptedJobs(limit = 1): Promise<{ jobs: SyncJob[]; total: number }> {
  const res = await shopFetch(`/api/jobs?status=INTERRUPTED&limit=${limit}`);
  if (!res.ok) return { jobs: [], total: 0 };
  return res.json();
}

export async function fetchAnalytics(params: {
  range?: string;
  from?: string;
  to?: string;
  jobId?: string;
}): Promise<any> {
  const query = new URLSearchParams();
  if (params.range) query.set('time_range', params.range);
  if (params.from) query.set('from', params.from);
  if (params.to) query.set('to', params.to);
  if (params.jobId && params.jobId !== 'all') query.set('job_id', params.jobId);
  const res = await shopFetch(`/api/metrics?${query.toString()}`);
  if (!res.ok) throw new Error(`Failed to fetch analytics: ${res.statusText}`);
  return res.json();
}

export async function startJob(payload: any): Promise<{ job_id: string; status: string }> {
  const res = await shopFetch('/api/jobs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to start synchronization');
  return data;
}

export async function resumeJob(jobId: string): Promise<any> {
  const res = await shopFetch(`/api/jobs/${encodeURIComponent(jobId)}/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to resume synchronization');
  return data;
}

export async function cancelJob(jobId: string): Promise<any> {
  const res = await shopFetch(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, {
    method: 'POST',
  });
  if (!res.ok) throw new Error('Failed to cancel synchronization');
  return res.json();
}

export async function resetDatabase(): Promise<any> {
  const res = await shopFetch('/api/database/reset', {
    method: 'POST',
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to reset database');
  return data;
}

export async function remediateProduct(payload: {
  issue_id: string;
  sku: string;
  name: string;
  price: number;
  status: string;
}): Promise<any> {
  const res = await shopFetch(`/api/dead-letter/${encodeURIComponent(payload.issue_id)}/resolve`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      sku: payload.sku,
      name: payload.name,
      price: payload.price,
      status: payload.status,
    }),
  });
  const data = await res.json();
  if (!res.ok) throw new Error(data.error || 'Failed to remediate product');
  return data;
}
