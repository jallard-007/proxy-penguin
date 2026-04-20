export interface RequestRecord {
  id: number;
  timestamp: string;
  hostname: string;
  path: string;
  clientIp: string;
  status: number;
  durationMs: number;
  userAgent: string;
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
