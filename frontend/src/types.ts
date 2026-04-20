export interface RequestRecord {
  id: number;
  timestamp: string;
  hostname: string;
  path: string;
  queryParams: string;
  clientIp: string;
  status: number;
  durationMs: number;
  userAgent: string;
  pending: boolean;
}

export interface PageResponse {
  records: RequestRecord[];
  hasMore: boolean;
}

export type SortField = keyof RequestRecord;
export type SortDir = 'asc' | 'desc';

export interface SortState {
  field: SortField;
  dir: SortDir;
}

export interface Filters {
  hostname: string;
  path: string;
  clientIp: string;
  status: string;
  userAgent: string;
  excludedHostnames: string[];
  dateFrom: string;
  dateTo: string;
  datePreset: string;
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
  datePreset: '',
};
