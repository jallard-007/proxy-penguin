import { useState, useMemo } from 'react';
import type { Filters, SortState } from './types';
import { emptyFilters } from './types';
import { applyFilters, sortRecords } from './utils/filter';
import { useAuth } from './hooks/useAuth';
import { useRecords } from './hooks/useRecords';
import Toolbar from './components/Toolbar';
import RequestTable from './components/RequestTable';
import LoginForm from './components/LoginForm';

export default function App() {
  const { authState, loginError, handleLoginSuccess, handleLogout, handleAuthExpired } = useAuth();

  const sseEnabled = authState === 'authenticated' || authState === 'noauth';
  const { allRecords, newIds, sseStatus, hasMore, loadingMore, handleLoadMore, reset } = useRecords({
    enabled: sseEnabled,
    onAuthExpired: handleAuthExpired,
  });

  const [sort, setSort] = useState<SortState>({ field: 'timestamp', dir: 'desc' });
  const [filters, setFilters] = useState<Filters>(emptyFilters);

  const hostnames = useMemo(() => {
    const set = new Set<string>();
    for (const r of allRecords) set.add(r.hostname);
    return [...set].sort();
  }, [allRecords]);

  const filteredRecords = useMemo(
    () => sortRecords(applyFilters(allRecords, filters), sort),
    [allRecords, filters, sort],
  );

  const onLoginSuccess = () => {
    reset();
    handleLoginSuccess();
  };

  const onLogout = () => {
    handleLogout();
    reset();
  };

  if (authState === 'checking') {
    return (
      <div className="h-screen flex items-center justify-center">
        <div className="w-6 h-6 border-2 border-accent border-t-transparent rounded-full animate-spin" />
      </div>
    );
  }

  if (authState === 'login') {
    return <LoginForm onSuccess={onLoginSuccess} initialError={loginError} />;
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
              onClick={onLogout}
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
