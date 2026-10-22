import {
  CatalogProduct,
  DeadLetterIssue,
  SyncJob,
  AnalyticsResponse,
  TimeSeriesPoint,
} from '../types';

const SHOP_STORAGE_KEY = 'catalog_sync_active_shop_id';
const RECENT_SHOPS_KEY = 'catalog_sync_recent_shops';
const CLOUDFLARE_WORKER_URL = 'https://catalog-sync-worker.ezcert.workers.dev';

export function getApiBaseUrl(): string {
  const envUrl = (import.meta.env.VITE_API_URL || '').replace(/\/$/, '');
  if (envUrl) return envUrl;
  if (typeof window !== 'undefined') {
    const host = window.location.hostname;
    if (host.includes('pages.dev') || host.includes('workers.dev')) {
      return CLOUDFLARE_WORKER_URL;
    }
  }
  return '';
}

export function getActiveShopId(): string {
  if (typeof window !== 'undefined') {
    const params = new URLSearchParams(window.location.search);
    const queryShop = params.get('shop') || params.get('shop_id');
    if (queryShop && /^[a-z0-9_-]{1,64}$/i.test(queryShop.trim())) {
      const clean = queryShop.trim().toLowerCase();
      localStorage.setItem(SHOP_STORAGE_KEY, clean);
      saveRecentShop(clean);
      return clean;
    }
    const stored = localStorage.getItem(SHOP_STORAGE_KEY);
    if (stored && /^[a-z0-9_-]{1,64}$/i.test(stored.trim())) {
      const clean = stored.trim().toLowerCase();
      const url = new URL(window.location.href);
      if (!url.searchParams.has('shop')) {
        url.searchParams.set('shop', clean);
        window.history.replaceState(null, '', url.toString());
      }
      return clean;
    }
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
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
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
  const base = getApiBaseUrl();
  const path = shopId ? `/api/events?shop=${encodeURIComponent(shopId)}` : '/api/events';
  return base ? `${base}${path}` : path;
}

async function shopFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
  const shopId = getActiveShopId();
  const headers = new Headers(init?.headers || {});
  if (shopId && !headers.has('X-Shop-Id')) {
    headers.set('X-Shop-Id', shopId);
  }
  const base = getApiBaseUrl();
  let finalInput = input;
  if (typeof input === 'string' && input.startsWith('/api') && base) {
    finalInput = `${base}${input}`;
  }
  return fetch(finalInput, {
    ...init,
    headers,
  });
}

async function handleResponse<T = any>(res: Response, fallbackErrMsg: string): Promise<T> {
  const contentType = res.headers.get('content-type') || '';
  let data: any = null;
  if (contentType.includes('application/json')) {
    try {
      data = await res.json();
    } catch (_) {
      data = null;
    }
  }

  if (!res.ok) {
    let errMessage = '';
    if (data && typeof data === 'object') {
      errMessage = data.error || data.message || '';
    }
    if (!errMessage) {
      try {
        const text = await res.text();
        errMessage = text.slice(0, 300);
      } catch (_) {}
    }
    throw new Error(errMessage || `${fallbackErrMsg} (HTTP ${res.status})`);
  }

  return (data !== null ? data : ({} as T));
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
  return handleResponse<{ products: CatalogProduct[]; total: number }>(res, 'Failed to fetch catalog');
}

export async function fetchDLQ(
  limit = 20,
  offset = 0,
  reason = ''
): Promise<{ issues: DeadLetterIssue[]; total: number }> {
  let url = `/api/dead-letter?limit=${limit}&offset=${offset}`;
  if (reason) url += `&reason=${encodeURIComponent(reason)}`;
  const res = await shopFetch(url);
  return handleResponse<{ issues: DeadLetterIssue[]; total: number }>(res, 'Failed to fetch DLQ');
}

export async function fetchHistory(
  limit = 20,
  offset = 0,
  status = ''
): Promise<{ jobs: SyncJob[]; total: number }> {
  let url = `/api/jobs?limit=${limit}&offset=${offset}`;
  if (status && status !== 'ALL') {
    url += `&status=${encodeURIComponent(status)}`;
  }
  const res = await shopFetch(url);
  return handleResponse<{ jobs: SyncJob[]; total: number }>(res, 'Failed to fetch history');
}

export async function fetchInterruptedJobs(limit = 1): Promise<{ jobs: SyncJob[]; total: number }> {
  const res = await shopFetch(`/api/jobs?status=INTERRUPTED&limit=${limit}`);
  if (!res.ok) return { jobs: [], total: 0 };
  return handleResponse<{ jobs: SyncJob[]; total: number }>(res, 'Failed to fetch interrupted jobs');
}

export async function fetchJob(jobId: string): Promise<any> {
  const res = await shopFetch(`/api/jobs/${encodeURIComponent(jobId)}`);
  return handleResponse<any>(res, 'Failed to fetch job');
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
  return handleResponse<any>(res, 'Failed to fetch analytics');
}

export async function startJob(payload: any): Promise<{ job_id: string; status: string }> {
  const res = await shopFetch('/api/jobs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  return handleResponse<{ job_id: string; status: string }>(res, 'Failed to start synchronization');
}

export async function resumeJob(jobId: string): Promise<any> {
  const res = await shopFetch(`/api/jobs/${encodeURIComponent(jobId)}/resume`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
  });
  return handleResponse<any>(res, 'Failed to resume synchronization');
}

export async function cancelJob(jobId: string): Promise<any> {
  const res = await shopFetch(`/api/jobs/${encodeURIComponent(jobId)}/cancel`, {
    method: 'POST',
  });
  return handleResponse<any>(res, 'Failed to cancel synchronization');
}

export async function resetDatabase(): Promise<any> {
  const res = await shopFetch('/api/database/reset', {
    method: 'POST',
  });
  return handleResponse<any>(res, 'Failed to reset database');
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
  return handleResponse<any>(res, 'Failed to remediate product');
}
