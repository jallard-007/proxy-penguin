import { useState, useRef, useEffect } from 'react';
import StatusBadge from './StatusBadge';
import type { RequestRecord, SortState } from '../types';
import { formatTime, formatDuration, durationColor, formatQueryParams } from '../utils/format';
import { useColumnResize } from '../hooks/useColumnResize';

interface RequestTableProps {
  records: RequestRecord[];
  newIds: Set<number>;
  sort: SortState;
  onSortChange: (s: SortState) => void;
  onLoadMore: () => void;
  hasMore: boolean;
  loadingMore: boolean;
}

function QueryParamsCell({ raw }: { raw: string }) {
  const { text, parts } = formatQueryParams(raw);
  if (!parts.length) return null;
  return (
    <span title={text}>
      {parts.map((p, i) => (
        <span key={i}>
          {i > 0 && <span className="text-muted">, </span>}
          <span className="text-amber-300">{p.key}</span>
          <span className="text-muted">=</span>
          <span className="text-gray-300">{p.value}</span>
        </span>
      ))}
    </span>
  );
}

interface Column {
  field: keyof RequestRecord;
  label: string;
  defaultWidth: number;
}

const COLUMNS: Column[] = [
  { field: 'timestamp', label: 'Time', defaultWidth: 210 },
  { field: 'hostname', label: 'Hostname', defaultWidth: 160 },
  { field: 'path', label: 'Path', defaultWidth: 240 },
  { field: 'queryParams', label: 'Query Params', defaultWidth: 200 },
  { field: 'clientIp', label: 'Client IP', defaultWidth: 140 },
  { field: 'status', label: 'Status', defaultWidth: 90 },
  { field: 'durationMs', label: 'Duration', defaultWidth: 110 },
  { field: 'userAgent', label: 'User-Agent', defaultWidth: 220 },
];

export default function RequestTable({
  records,
  newIds,
  sort,
  onSortChange,
  onLoadMore,
  hasMore,
  loadingMore,
}: RequestTableProps) {
  const sentinelRef = useRef<HTMLTableRowElement>(null);
  const { columnWidths, handleResizeStart, consumeResize } = useColumnResize(COLUMNS.map((c) => c.defaultWidth));
  const [visibleFields, setVisibleFields] = useState<Set<keyof RequestRecord>>(
    () => new Set(COLUMNS.map((c) => c.field)),
  );
  const [showColumnMenu, setShowColumnMenu] = useState(false);
  const columnMenuRef = useRef<HTMLDivElement>(null);

  const handleSort = (field: keyof RequestRecord) => {
    if (consumeResize()) return;
    if (sort.field === field) {
      onSortChange({ field, dir: sort.dir === 'desc' ? 'asc' : 'desc' });
    } else {
      onSortChange({ field, dir: 'desc' });
    }
  };

  const onLoadMoreRef = useRef(onLoadMore);
  const hasMoreRef = useRef(hasMore);
  const loadingMoreRef = useRef(loadingMore);
  onLoadMoreRef.current = onLoadMore;
  hasMoreRef.current = hasMore;
  loadingMoreRef.current = loadingMore;

  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver((entries) => {
      if (entries[0]?.isIntersecting && hasMoreRef.current && !loadingMoreRef.current) {
        onLoadMoreRef.current();
      }
    }, { threshold: 0.1 });
    observer.observe(el);
    return () => observer.disconnect();
  }, []);

  // Close column menu on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (columnMenuRef.current && !columnMenuRef.current.contains(e.target as Node)) {
        setShowColumnMenu(false);
      }
    };
    document.addEventListener('click', handler);
    return () => document.removeEventListener('click', handler);
  }, []);

  const toggleColumn = (field: keyof RequestRecord) => {
    setVisibleFields((prev) => {
      const next = new Set(prev);
      if (next.has(field)) {
        if (next.size > 1) next.delete(field);
      } else {
        next.add(field);
      }
      return next;
    });
  };

  const visibleColumns = COLUMNS.filter((c) => visibleFields.has(c.field));
  const visibleColumnWidths = COLUMNS.map((_, i) => columnWidths[i]).filter((_, i) => visibleFields.has(COLUMNS[i].field));

  return (
    <div className="flex-1 min-h-0 flex flex-col">
      {/* Column visibility dropdown */}
      <div className="relative flex justify-end px-2 pt-1 pb-1 z-20 shrink-0" ref={columnMenuRef}>
        <button
          onClick={(e) => { e.stopPropagation(); setShowColumnMenu(!showColumnMenu); }}
          className="text-[10px] px-2 py-0.5 rounded border border-border-subtle bg-surface text-muted hover:text-gray-100 cursor-pointer"
          title="Show/hide columns"
        >
          Columns &#9662;
        </button>
        {showColumnMenu && (
          <div className="absolute top-full right-0 mt-1 bg-surface border border-border-subtle rounded-md shadow-lg min-w-36 py-1">
            {COLUMNS.map((col) => (
              <label key={col.field} className="flex items-center gap-2 px-3 py-1 text-xs cursor-pointer hover:bg-surface-hover">
                <input
                  type="checkbox"
                  checked={visibleFields.has(col.field)}
                  onChange={() => toggleColumn(col.field)}
                  className="accent-accent"
                />
                <span>{col.label}</span>
              </label>
            ))}
          </div>
        )}
      </div>

      <div className="flex-1 overflow-auto relative">
        <table className="text-xs border-collapse" style={{ tableLayout: 'fixed', width: visibleColumnWidths.reduce((a, b) => a + b, 0) }}>
          <colgroup>
            {visibleColumnWidths.map((w, i) => (
              <col key={i} style={{ width: w }} />
            ))}
          </colgroup>
          <thead className="sticky top-0 z-10">
            <tr>
              {visibleColumns.map((col, i) => {
                const globalIndex = COLUMNS.indexOf(col);
                return (
                  <th
                    key={col.field}
                    onClick={() => handleSort(col.field)}
                    className={`relative bg-surface border-b border-border-subtle px-3 py-2 text-left font-semibold cursor-pointer select-none whitespace-nowrap overflow-hidden ${
                      sort.field === col.field ? 'text-accent' : 'text-muted hover:text-gray-100'
                    }`}
                  >
                    {col.label}
                    {sort.field === col.field && (
                      <span className="ml-1 text-[10px]">{sort.dir === 'asc' ? '\u25B2' : '\u25BC'}</span>
                    )}
                    {/* Resize handle */}
                    {i < visibleColumns.length - 1 && (
                      <div
                        onMouseDown={(e) => handleResizeStart(e, globalIndex)}
                        className="absolute top-0 right-0 w-[5px] h-full cursor-col-resize group z-10"
                      >
                        <div className="absolute top-1 bottom-1 right-[2px] w-px bg-border-subtle group-hover:bg-accent group-active:bg-accent" />
                      </div>
                    )}
                  </th>
                );
              })}
            </tr>
          </thead>
          <tbody>
            {records.map((r) => {
              const isNew = newIds.has(r.id);
              return (
                <tr
                  key={r.id}
                  className={`border-b border-border-subtle hover:bg-surface-hover ${isNew ? 'animate-flash-new' : ''}`}
                >
                  {visibleFields.has('timestamp') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px] text-gray-300">{formatTime(r.timestamp)}</td>
                  )}
                  {visibleFields.has('hostname') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px] text-cyan-400" title={r.hostname}>
                      {r.hostname}
                    </td>
                  )}
                  {visibleFields.has('path') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px] text-gray-300" title={r.path}>
                      {r.path}
                    </td>
                  )}
                  {visibleFields.has('queryParams') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px]">
                      <QueryParamsCell raw={r.queryParams} />
                    </td>
                  )}
                  {visibleFields.has('clientIp') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px] text-purple-400">{r.clientIp}</td>
                  )}
                  {visibleFields.has('status') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden">
                      {r.pending
                        ? <span className="inline-flex items-center gap-1.5 text-[11px] text-yellow-400">
                            <span className="w-3 h-3 border-[1.5px] border-yellow-400 border-t-transparent rounded-full animate-spin" />
                            Pending
                          </span>
                        : <StatusBadge status={r.status} />
                      }
                    </td>
                  )}
                  {visibleFields.has('durationMs') && (
                    <td className={`px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px] ${r.pending ? 'text-muted' : durationColor(r.durationMs)}`}>
                      {r.pending ? '\u2014' : formatDuration(r.durationMs)}
                    </td>
                  )}
                  {visibleFields.has('userAgent') && (
                    <td className="px-3 py-1.5 whitespace-nowrap overflow-hidden text-ellipsis font-mono text-[11px] text-muted" title={r.userAgent}>
                      {r.userAgent}
                    </td>
                  )}
                </tr>
              );
            })}

            {/* Infinite scroll sentinel */}
            <tr ref={sentinelRef} className="h-1">
              <td colSpan={visibleColumns.length}>
                {loadingMore && (
                  <div className="flex justify-center py-4">
                    <div className="w-5 h-5 border-2 border-accent border-t-transparent rounded-full animate-spin" />
                  </div>
                )}
                {!hasMore && records.length > 0 && (
                  <div className="text-center py-3 text-xs text-muted">All records loaded</div>
                )}
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  );
}
