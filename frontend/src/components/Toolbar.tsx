import { useState, useRef, useEffect } from 'react';

export interface Filters {
  hostname: string;
  path: string;
  clientIp: string;
  status: string;
  userAgent: string;
  excludedHostnames: string[];
  dateFrom: string;
  dateTo: string;
}

export const emptyFilters: Filters = {
  hostname: '',
  path: '',
  clientIp: '',
  status: '',
  userAgent: '',
  excludedHostnames: [],
  dateFrom: '',
  dateTo: '',
};

interface ToolbarProps {
  filters: Filters;
  onFiltersChange: (f: Filters) => void;
  totalCount: number;
  filteredCount: number;
  hostnames: string[];
}

export default function Toolbar({ filters, onFiltersChange, totalCount, filteredCount, hostnames }: ToolbarProps) {
  const [showExclude, setShowExclude] = useState(false);
  const excludeRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (excludeRef.current && !excludeRef.current.contains(e.target as Node)) {
        setShowExclude(false);
      }
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, []);

  const update = (patch: Partial<Filters>) => {
    onFiltersChange({ ...filters, ...patch });
  };

  const clearAll = () => {
    onFiltersChange(emptyFilters);
  };

  const toggleExcludedHostname = (h: string) => {
    const set = new Set(filters.excludedHostnames);
    if (set.has(h)) {
      set.delete(h);
    } else {
      set.add(h);
    }
    update({ excludedHostnames: [...set] });
  };

  const hasActiveFilters =
    filters.hostname || filters.path || filters.clientIp || filters.status ||
    filters.userAgent || filters.excludedHostnames.length > 0 || filters.dateFrom || filters.dateTo;

  return (
    <div className="flex items-center gap-2 px-4 py-2.5 bg-surface border-b border-border-subtle flex-wrap">
      {/* Text filters */}
      <input
        placeholder="Hostname..."
        title="Filter by hostname"
        value={filters.hostname}
        onChange={(e) => update({ hostname: e.target.value })}
        className="bg-gray-950 border border-border-subtle text-gray-100 px-2.5 py-1 rounded-md text-xs outline-none focus:border-accent w-32"
      />
      <input
        placeholder="Path..."
        title="Filter by path"
        value={filters.path}
        onChange={(e) => update({ path: e.target.value })}
        className="bg-gray-950 border border-border-subtle text-gray-100 px-2.5 py-1 rounded-md text-xs outline-none focus:border-accent w-28"
      />
      <input
        placeholder="Client IP..."
        title="Filter by client IP"
        value={filters.clientIp}
        onChange={(e) => update({ clientIp: e.target.value })}
        className="bg-gray-950 border border-border-subtle text-gray-100 px-2.5 py-1 rounded-md text-xs outline-none focus:border-accent w-28"
      />
      <input
        placeholder="Status (e.g. 2xx)..."
        title="Filter by status code. Use x as wildcard."
        value={filters.status}
        onChange={(e) => update({ status: e.target.value })}
        className="bg-gray-950 border border-border-subtle text-gray-100 px-2.5 py-1 rounded-md text-xs outline-none focus:border-accent w-32"
      />
      <input
        placeholder="User-Agent..."
        title="Filter by User-Agent"
        value={filters.userAgent}
        onChange={(e) => update({ userAgent: e.target.value })}
        className="bg-gray-950 border border-border-subtle text-gray-100 px-2.5 py-1 rounded-md text-xs outline-none focus:border-accent w-28"
      />

      {/* Date range with presets */}
      <div className="flex items-center gap-1 text-xs text-muted">
        {[
          { label: '15m', minutes: 15 },
          { label: '1h', minutes: 60 },
          { label: '24h', minutes: 1440 },
          { label: '7d', minutes: 10080 },
        ].map(({ label, minutes }) => {
          const fromValue = new Date(Date.now() - minutes * 60000).toISOString().slice(0, 16);
          const isActive = filters.dateFrom === fromValue && !filters.dateTo;
          return (
            <button
              key={label}
              onClick={() => update({ dateFrom: fromValue, dateTo: '' })}
              className={`px-2 py-1 rounded-md border cursor-pointer ${
                isActive
                  ? 'bg-accent/15 border-accent/40 text-accent'
                  : 'bg-gray-950 border-border-subtle hover:text-gray-100'
              }`}
            >
              {label}
            </button>
          );
        })}
        <input
          type="datetime-local"
          value={filters.dateFrom}
          onChange={(e) => update({ dateFrom: e.target.value })}
          title="From date"
          className="bg-gray-950 border border-border-subtle text-gray-100 px-2 py-1 rounded-md text-xs outline-none focus:border-accent w-[10rem]"
        />
        <span>–</span>
        <input
          type="datetime-local"
          value={filters.dateTo}
          onChange={(e) => update({ dateTo: e.target.value })}
          title="To date"
          className="bg-gray-950 border border-border-subtle text-gray-100 px-2 py-1 rounded-md text-xs outline-none focus:border-accent w-[10rem]"
        />
      </div>

      {/* Exclude hostnames dropdown */}
      <div className="relative" ref={excludeRef}>
        <button
          onClick={(e) => { e.stopPropagation(); setShowExclude(!showExclude); }}
          className={`text-xs px-2.5 py-1 rounded-md cursor-pointer border ${
            filters.excludedHostnames.length > 0
              ? 'bg-red-500/15 border-red-500/40 text-red-400'
              : 'bg-gray-950 border-border-subtle text-muted hover:text-gray-100'
          }`}
        >
          Exclude{filters.excludedHostnames.length > 0 && ` (${filters.excludedHostnames.length})`} &#9662;
        </button>
        {showExclude && (
          <div className="absolute top-full right-0 mt-1 bg-surface border border-border-subtle rounded-md shadow-lg z-50 min-w-48 max-h-64 overflow-y-auto">
            {hostnames.length === 0 ? (
              <div className="px-3 py-2 text-xs text-muted">No hostnames yet</div>
            ) : (
              hostnames.map((h) => (
                <label key={h} className="flex items-center gap-2 px-3 py-1.5 text-xs cursor-pointer hover:bg-gray-950">
                  <input
                    type="checkbox"
                    checked={filters.excludedHostnames.includes(h)}
                    onChange={() => toggleExcludedHostname(h)}
                    className="accent-red-500"
                  />
                  <span className={filters.excludedHostnames.includes(h) ? 'line-through text-muted' : ''}>{h}</span>
                </label>
              ))
            )}
          </div>
        )}
      </div>

      {/* Clear filters */}
      {hasActiveFilters && (
        <button
          onClick={clearAll}
          className="text-xs px-2.5 py-1 rounded-md bg-border-subtle text-gray-100 cursor-pointer hover:bg-muted"
        >
          Clear
        </button>
      )}

      <div className="flex-1" />

      {/* Count */}
      <span className="text-xs text-muted">
        {filteredCount === totalCount
          ? `${totalCount} requests`
          : `${filteredCount} / ${totalCount} requests`}
      </span>
    </div>
  );
}
