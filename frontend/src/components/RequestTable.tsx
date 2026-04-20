import { useRef, useCallback, useEffect } from 'react';
import StatusBadge from './StatusBadge';
import type { RequestRecord, SortState } from '../types';

interface RequestTableProps {
  records: RequestRecord[];
  newIds: Set<number>;
  sort: SortState;
  onSortChange: (s: SortState) => void;
  onLoadMore: () => void;
  hasMore: boolean;
  loadingMore: boolean;
}

const MONTH_NAMES = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
const DAY_NAMES = ['Sun','Mon','Tue','Wed','Thu','Fri','Sat'];

function formatTime(ts: string): string {
  const d = new Date(ts);
  const now = new Date();

  const hours24 = d.getHours();
  const ampm = hours24 >= 12 ? 'PM' : 'AM';
  let hours = hours24 % 12;
  if (hours === 0) hours = 12;
  const m = String(d.getMinutes()).padStart(2, '0');
  const s = String(d.getSeconds()).padStart(2, '0');
  const ms = String(d.getMilliseconds()).padStart(3, '0');
  const timePart = `${hours}:${m}:${s}.${ms} ${ampm}`;

  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
  const recordDay = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const diffDays = Math.round((today.getTime() - recordDay.getTime()) / 86400000);

  let datePart: string;
  if (diffDays === 0) {
    datePart = 'Today';
  } else if (diffDays === 1) {
    datePart = 'Yesterday';
  } else if (diffDays >= 0 && diffDays <= 6) {
    datePart = DAY_NAMES[d.getDay()];
  } else {
    datePart = MONTH_NAMES[d.getMonth()] + ' ' + d.getDate();
  }

  if (d.getFullYear() !== now.getFullYear()) {
    datePart = d.getFullYear() + ' ' + datePart;
  }

  return datePart + ' ' + timePart;
}

function formatDuration(ms: number): string {
  if (ms < 1) return ms.toFixed(2) + 'ms';
  if (ms < 1000) return ms.toFixed(1) + 'ms';
  return (ms / 1000).toFixed(2) + 's';
}

function durationColor(ms: number): string {
  if (ms < 100) return 'text-green-400';
  if (ms < 500) return 'text-yellow-400';
  if (ms < 2000) return 'text-amber-400';
  return 'text-red-400';
}

interface Column {
  field: keyof RequestRecord;
  label: string;
  width: string;
}

const COLUMNS: Column[] = [
  { field: 'timestamp', label: 'Time', width: 'w-44' },
  { field: 'hostname', label: 'Hostname', width: 'w-40' },
  { field: 'path', label: 'Path', width: 'w-60' },
  { field: 'clientIp', label: 'Client IP', width: 'w-32' },
  { field: 'status', label: 'Status', width: 'w-20' },
  { field: 'durationMs', label: 'Duration', width: 'w-24' },
  { field: 'userAgent', label: 'User-Agent', width: 'w-52' },
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

  const handleSort = (field: keyof RequestRecord) => {
    if (sort.field === field) {
      onSortChange({ field, dir: sort.dir === 'desc' ? 'asc' : 'desc' });
    } else {
      onSortChange({ field, dir: 'desc' });
    }
  };

  const observerCallback = useCallback((entries: IntersectionObserverEntry[]) => {
    if (entries[0]?.isIntersecting && hasMore && !loadingMore) {
      onLoadMore();
    }
  }, [hasMore, loadingMore, onLoadMore]);

  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;
    const observer = new IntersectionObserver(observerCallback, { threshold: 0.1 });
    observer.observe(el);
    return () => observer.disconnect();
  }, [observerCallback]);

  return (
    <div className="flex-1 overflow-auto">
      <table className="w-full text-xs border-collapse table-fixed">
        <thead className="sticky top-0 z-10">
          <tr>
            {COLUMNS.map((col) => (
              <th
                key={col.field}
                onClick={() => handleSort(col.field)}
                className={`bg-surface border-b border-border-subtle px-3 py-2 text-left font-semibold cursor-pointer select-none whitespace-nowrap ${col.width} ${
                  sort.field === col.field ? 'text-accent' : 'text-muted hover:text-gray-100'
                }`}
              >
                {col.label}
                {sort.field === col.field && (
                  <span className="ml-1 text-[10px]">{sort.dir === 'asc' ? '\u25B2' : '\u25BC'}</span>
                )}
              </th>
            ))}
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
                <td className="px-3 py-1.5 whitespace-nowrap font-mono text-[11px] text-gray-300">{formatTime(r.timestamp)}</td>
                <td className="px-3 py-1.5 whitespace-nowrap font-mono text-[11px] text-cyan-400 truncate" title={r.hostname}>
                  {r.hostname}
                </td>
                <td className="px-3 py-1.5 whitespace-nowrap font-mono text-[11px] text-gray-300 truncate" title={r.path}>
                  {r.path}
                </td>
                <td className="px-3 py-1.5 whitespace-nowrap font-mono text-[11px] text-purple-400">{r.clientIp}</td>
                <td className="px-3 py-1.5 whitespace-nowrap"><StatusBadge status={r.status} /></td>
                <td className={`px-3 py-1.5 whitespace-nowrap font-mono text-[11px] ${durationColor(r.durationMs)}`}>
                  {formatDuration(r.durationMs)}
                </td>
                <td className="px-3 py-1.5 whitespace-nowrap font-mono text-[11px] text-muted truncate" title={r.userAgent}>
                  {r.userAgent}
                </td>
              </tr>
            );
          })}

          {/* Infinite scroll sentinel */}
          <tr ref={sentinelRef} className="h-1">
            <td colSpan={COLUMNS.length}>
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
  );
}
