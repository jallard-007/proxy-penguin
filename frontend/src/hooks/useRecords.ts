import { useState, useCallback, useRef, useMemo, useEffect } from 'react';
import type { RequestRecord } from '../types';
import { fetchRequests } from '../api';
import { useSSE } from './useSSE';

interface UseRecordsOptions {
  enabled: boolean;
  onAuthExpired: () => void;
}

export function useRecords({ enabled, onAuthExpired }: UseRecordsOptions) {
  const [historicalRecords, setHistoricalRecords] = useState<RequestRecord[]>([]);
  const [liveRecords, setLiveRecords] = useState<RequestRecord[]>([]);
  const [newIds, setNewIds] = useState<Set<number>>(new Set());
  const newIdTimers = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

  const [hasMore, setHasMore] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [initialLoaded, setInitialLoaded] = useState(false);
  const [minHistoricalId, setMinHistoricalId] = useState<number | undefined>(undefined);
  const maxKnownId = useRef(0);

  // SSE is only enabled after the initial page load completes, so the
  // stream connects with the correct after_id and doesn't miss events.
  const [sseReady, setSseReady] = useState(false);
  const [sseStatus, setSseStatus] = useState<'connecting' | 'connected' | 'disconnected'>('connecting');

  const allRecords = useMemo(() => {
    const map = new Map<number, RequestRecord>();
    for (const r of historicalRecords) map.set(r.id, r);
    for (const r of liveRecords) map.set(r.id, r);
    return Array.from(map.values());
  }, [historicalRecords, liveRecords]);

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
      if (page.length > 0) {
        let pageMin = page[0].id;
        let pageMax = page[0].id;
        for (let i = 1; i < page.length; i++) {
          if (page[i].id < pageMin) pageMin = page[i].id;
          if (page[i].id > pageMax) pageMax = page[i].id;
        }
        setMinHistoricalId((prev) => prev === undefined ? pageMin : Math.min(prev, pageMin));
        if (pageMax > maxKnownId.current) maxKnownId.current = pageMax;
      }
      setHasMore(resp.hasMore);
    } catch (err) {
      console.error('Failed to load records:', err);
    } finally {
      setLoadingMore(false);
    }
  }, []);

  // Initial load: fetch first page, then enable SSE with the max known ID.
  useEffect(() => {
    if (enabled && !initialLoaded) {
      setInitialLoaded(true);
      loadPage().then(() => setSseReady(true));
    }
  }, [enabled, initialLoaded, loadPage]);

  const handleLoadMore = useCallback(() => {
    if (minHistoricalId === undefined) return;
    loadPage(minHistoricalId);
  }, [minHistoricalId, loadPage]);

  const onRecord = useCallback((rec: RequestRecord) => {
    if (rec.id > maxKnownId.current) maxKnownId.current = rec.id;
    setLiveRecords((prev) => [...prev, rec]);
    setNewIds((prev) => {
      const next = new Set(prev);
      next.add(rec.id);
      return next;
    });

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

  const onRecordUpdate = useCallback((rec: RequestRecord) => {
    setLiveRecords((prev) => prev.map((r) => (r.id === rec.id ? rec : r)));
    setHistoricalRecords((prev) => prev.map((r) => (r.id === rec.id ? rec : r)));
  }, []);

  const getLastId = useCallback(() => maxKnownId.current, []);

  const closeSSE = useSSE({
    onRecord,
    onRecordUpdate,
    onAuthExpired,
    onStatusChange: setSseStatus,
    getLastId,
    enabled: sseReady,
  });

  // Cleanup timers on unmount
  useEffect(() => {
    return () => {
      for (const timer of newIdTimers.current.values()) {
        clearTimeout(timer);
      }
      newIdTimers.current.clear();
    };
  }, []);

  const reset = useCallback(() => {
    closeSSE();
    setHistoricalRecords([]);
    setLiveRecords([]);
    setNewIds(new Set());
    setSseStatus('disconnected');
    setSseReady(false);
    setInitialLoaded(false);
    setMinHistoricalId(undefined);
    maxKnownId.current = 0;
    for (const timer of newIdTimers.current.values()) {
      clearTimeout(timer);
    }
    newIdTimers.current.clear();
  }, [closeSSE]);

  return {
    allRecords,
    newIds,
    sseStatus,
    hasMore,
    loadingMore,
    handleLoadMore,
    reset,
  };
}
