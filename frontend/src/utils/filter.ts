import type { RequestRecord, Filters, SortState } from '../types';

const DATE_PRESET_MINUTES: Record<string, number> = {
  '15m': 15,
  '1h': 60,
  '24h': 1440,
  '7d': 10080,
};

function escapeRegex(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function matchesStatusFilter(statusCode: number, pattern: string): boolean {
  if (!pattern) return true;
  const s = String(statusCode);
  const p = pattern.trim().toLowerCase();
  const escaped = escapeRegex(p).replace(/x/g, '\\d');
  return new RegExp('^' + escaped + '$').test(s);
}

export function applyFilters(records: RequestRecord[], filters: Filters): RequestRecord[] {
  const excludeSet = new Set(filters.excludedHostnames);
  const hostnameFilter = filters.hostname.toLowerCase();
  const pathFilter = filters.path.toLowerCase();
  const uaFilter = filters.userAgent.toLowerCase();

  const presetMinutes = filters.datePreset ? DATE_PRESET_MINUTES[filters.datePreset] : 0;
  const presetFrom = presetMinutes ? Date.now() - presetMinutes * 60000 : 0;
  const dateFrom = !filters.datePreset && filters.dateFrom ? new Date(filters.dateFrom).getTime() : 0;
  const dateTo = !filters.datePreset && filters.dateTo ? new Date(filters.dateTo).getTime() : 0;

  return records.filter((r) => {
    if (excludeSet.size > 0 && excludeSet.has(r.hostname)) return false;
    if (hostnameFilter && !r.hostname.toLowerCase().includes(hostnameFilter)) return false;
    if (pathFilter && !r.path.toLowerCase().includes(pathFilter)) return false;
    if (filters.clientIp && !r.clientIp.includes(filters.clientIp)) return false;
    if (filters.status && !matchesStatusFilter(r.status, filters.status)) return false;
    if (uaFilter && !r.userAgent.toLowerCase().includes(uaFilter)) return false;
    if (presetFrom) {
      const ts = new Date(r.timestamp).getTime();
      if (ts < presetFrom) return false;
    }
    if (dateFrom) {
      const ts = new Date(r.timestamp).getTime();
      if (ts < dateFrom) return false;
    }
    if (dateTo) {
      const ts = new Date(r.timestamp).getTime();
      if (ts > dateTo) return false;
    }
    return true;
  });
}

export function sortRecords(records: RequestRecord[], sort: SortState): RequestRecord[] {
  return [...records].sort((a, b) => {
    let va: number | string = a[sort.field] as number | string;
    let vb: number | string = b[sort.field] as number | string;
    if (sort.field === 'timestamp') {
      va = new Date(va).getTime();
      vb = new Date(vb).getTime();
    }
    if (va < vb) return sort.dir === 'asc' ? -1 : 1;
    if (va > vb) return sort.dir === 'asc' ? 1 : -1;
    return 0;
  });
}
