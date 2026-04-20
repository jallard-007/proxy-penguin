import { useState, useCallback, useRef, useMemo, useEffect } from 'react';
import type { RequestRecord, SortState } from './types';
import { fetchRequests, checkAuth, logout } from './api';
import { useSSE } from './hooks/useSSE';
import Toolbar, { type Filters, emptyFilters } from './components/Toolbar';
import RequestTable from './components/RequestTable';
import LoginForm from './components/LoginForm';

type AuthState = 'checking' | 'login' | 'authenticated' | 'noauth';

function matchesStatusFilter(statusCode: number, pattern: string): boolean {
  if (!pattern) return true;
  const s = String(statusCode);
  const p = pattern.trim().toLowerCase().replace(/x/g, '\\d');
  try {
    return new RegExp('^' + p).test(s);
  } catch {
    return s.startsWith(pattern);
  }
}

function applyFilters(records: RequestRecord[], filters: Filters): RequestRecord[] {
  const excludeSet = new Set(filters.excludedHostnames);
  const dateFrom = filters.dateFrom ? new Date(filters.dateFrom).getTime() : 0;
  const dateTo = filters.dateTo ? new Date(filters.dateTo).getTime() : 0;
  const hostnameFilter = filters.hostname.toLowerCase();
  const pathFilter = filters.path.toLowerCase();
  const uaFilter = filters.userAgent.toLowerCase();

  return records.filter((r) => {
    if (excludeSet.size > 0 && excludeSet.has(r.hostname)) return false;
    if (hostnameFilter && !r.hostname.toLowerCase().includes(hostnameFilter)) return false;
    if (pathFilter && !r.path.toLowerCase().includes(pathFilter)) return false;
    if (filters.clientIp && !r.clientIp.includes(filters.clientIp)) return false;
    if (filters.status && !matchesStatusFilter(r.status, filters.status)) return false;
    if (uaFilter && !r.userAgent.toLowerCase().includes(uaFilter)) return false;
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

function sortRecords(records: RequestRecord[], sort: SortState): RequestRecord[] {
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

export default function App() {
  const [authState, setAuthState] = useState<AuthState>('checking');
  const [loginError, setLoginError] = useState('');
  const [sseStatus, setSseStatus] = useState<'connecting' | 'connected' | 'disconnected'>('connecting');

  // All records: historical + live
  const [historicalRecords, setHistoricalRecords] = useState<RequestRecord[]>([]);
  const [liveRecords, setLiveRecords] = useState<RequestRecord[]>([]);
  const [newIds, setNewIds] = useState<Set<number>>(new Set());
  const newIdTimers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [initialLoaded, setInitialLoaded] = useState(false);

  const [sort, setSort] = useState<SortState>({ field: 'timestamp', dir: 'desc' });
  const [filters, setFilters] = useState<Filters>(emptyFilters);

  // Combine historical and live records, deduplicating by ID
  const allRecords = useMemo(() => {
    const map = new Map<number, RequestRecord>();
    for (const r of historicalRecords) map.set(r.id, r);
    for (const r of liveRecords) map.set(r.id, r);
    return Array.from(map.values());
  }, [historicalRecords, liveRecords]);

  // Unique hostnames for exclusion filter
  const hostnames = useMemo(() => {
    const set = new Set<string>();
    for (const r of allRecords) set.add(r.hostname);
    return [...set].sort();
  }, [allRecords]);

  // Filtered + sorted
  const filteredRecords = useMemo(
    () => sortRecords(applyFilters(allRecords, filters), sort),
    [allRecords, filters, sort],
  );

  // Load a page of historical records
  const loadPage = useCallback(async (beforeId?: number) => {
    setLoadingMore(true);
    try {
      const resp = await fetchRequests(beforeId, 50);
      const page = resp.records ?? [];
      setHistoricalRecords((prev) => {
        const existing = new Set(prev.map((r) => r.id));
        const newRecs = page.filter((r) => !existing.has(r.id));
        return [...prev, ...newRecs];
      });
      setHasMore(resp.hasMore);
    } catch (err) {
      console.error('Failed to load records:', err);
    } finally {
      setLoadingMore(false);
    }
  }, []);

  // Initial load
  useEffect(() => {
    if ((authState === 'authenticated' || authState === 'noauth') && !initialLoaded) {
      setInitialLoaded(true);
      loadPage();
    }
  }, [authState, initialLoaded, loadPage]);

  const handleLoadMore = useCallback(() => {
    // Find the lowest ID in historical records to use as cursor
    if (historicalRecords.length === 0) return;
    const minId = Math.min(...historicalRecords.map((r) => r.id));
    loadPage(minId);
  }, [historicalRecords, loadPage]);

  // SSE callbacks
  const onRecord = useCallback((rec: RequestRecord) => {
    setLiveRecords((prev) => [...prev, rec]);
    setNewIds((prev) => {
      const next = new Set(prev);
      next.add(rec.id);
      return next;
    });

    // Clear new highlight after 3 seconds
    const timer = setTimeout(() => {
      setNewIds((prev) => {
        const next = new Set(prev);
        next.delete(rec.id);
        return next;
      });
      newIdTimers.current.delete(rec.id);
    }, 3000);
    newIdTimers.current.set(rec.id, timer);
  }, []);

  const onAuthExpired = useCallback(() => {
    setAuthState('login');
    setLoginError('Your session has expired. Please log in again.');
  }, []);

  const onStatusChange = useCallback((status: 'connecting' | 'connected' | 'disconnected') => {
    setSseStatus(status);
  }, []);

  const sseEnabled = authState === 'authenticated' || authState === 'noauth';
  const closeSSE = useSSE({ onRecord, onAuthExpired, onStatusChange, enabled: sseEnabled });

  // Check auth on mount
  useEffect(() => {
    checkAuth().then((result) => {
      if (!result.authRequired) {
        setAuthState('noauth');
      } else if (!result.error) {
        setAuthState('authenticated');
      } else {
        setAuthState('login');
      }
    }).catch(() => {
      setAuthState('noauth');
    });
  }, []);

  const handleLoginSuccess = () => {
    setAuthState('authenticated');
    setLoginError('');
    setInitialLoaded(false);
    setHistoricalRecords([]);
    setLiveRecords([]);
  };

  const handleLogout = async () => {
    closeSSE();
    try { await logout(); } catch { /* ignore */ }
    setAuthState('login');
    setHistoricalRecords([]);
    setLiveRecords([]);
    setNewIds(new Set());
    setSseStatus('disconnected');
    setInitialLoaded(false);
  };

  if (authState === 'checking') {
    return (
      <div className="h-screen flex items-center justify-center">
        <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (authState === 'login') {
    return <LoginForm onSuccess={handleLoginSuccess} initialError={loginError} />;
  }

  const statusDotClass =
    sseStatus === 'connected' ? 'bg-green-500' :
    sseStatus === 'connecting' ? 'bg-yellow-500 animate-pulse-dot' :
    'bg-red-500';

  const statusLabel =
    sseStatus === 'connected' ? 'Connected' :
    sseStatus === 'connecting' ? 'Connecting...' :
    'Disconnected';

  return (
    <div className="h-screen flex flex-col overflow-hidden">
      {/* Header */}
      <header className="flex items-center justify-between px-5 py-3 bg-surface border-b border-border-subtle shrink-0">
        <h1 className="text-lg font-semibold">&#x1F427; Proxy Penguin</h1>
        <div className="flex items-center gap-4">
          {authState === 'authenticated' && (
            <button
              onClick={handleLogout}
              className="text-xs px-3 py-1 rounded-md bg-border-subtle text-gray-100 cursor-pointer hover:bg-muted"
            >
              Logout
            </button>
          )}
          <div className="flex items-center gap-1.5 text-xs text-muted">
            <div className={`w-2 h-2 rounded-full ${statusDotClass}`} />
            <span>{statusLabel}</span>
          </div>
        </div>
      </header>

      {/* Toolbar */}
      <Toolbar
        filters={filters}
        onFiltersChange={setFilters}
        totalCount={allRecords.length}
        filteredCount={filteredRecords.length}
        hostnames={hostnames}
      />

      {/* Table */}
      <RequestTable
        records={filteredRecords}
        newIds={newIds}
        sort={sort}
        onSortChange={setSort}
        onLoadMore={handleLoadMore}
        hasMore={hasMore}
        loadingMore={loadingMore}
      />
    </div>
  );
}
