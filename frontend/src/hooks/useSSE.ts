import { useEffect, useRef, useCallback } from 'react';
import type { RequestRecord } from '../types';

type SSEStatus = 'connecting' | 'connected' | 'disconnected';

interface UseSSEOptions {
  onRecord: (rec: RequestRecord) => void;
  onRecordUpdate: (rec: RequestRecord) => void;
  onAuthExpired: () => void;
  onStatusChange: (status: SSEStatus) => void;
  getLastId: () => number;
  enabled: boolean;
}

export function useSSE({ onRecord, onRecordUpdate, onAuthExpired, onStatusChange, getLastId, enabled }: UseSSEOptions) {
  const esRef = useRef<EventSource | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);

  // Store callbacks in refs so the SSE connection doesn't cycle when they change
  const onRecordRef = useRef(onRecord);
  const onRecordUpdateRef = useRef(onRecordUpdate);
  const onAuthExpiredRef = useRef(onAuthExpired);
  const onStatusChangeRef = useRef(onStatusChange);
  const getLastIdRef = useRef(getLastId);
  onRecordRef.current = onRecord;
  onRecordUpdateRef.current = onRecordUpdate;
  onAuthExpiredRef.current = onAuthExpired;
  onStatusChangeRef.current = onStatusChange;
  getLastIdRef.current = getLastId;

  const close = useCallback(() => {
    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current);
      reconnectTimer.current = undefined;
    }
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
  }, []);

  useEffect(() => {
    if (!enabled) {
      close();
      return;
    }

    const connect = () => {
      close();
      onStatusChangeRef.current('connecting');

      const lastId = getLastIdRef.current();
      const url = lastId > 0 ? `/api/events/stream?after_id=${lastId}` : '/api/events/stream';
      const es = new EventSource(url);
      esRef.current = es;

      es.addEventListener('connected', () => {
        onStatusChangeRef.current('connected');
      });

      es.addEventListener('request', (e) => {
        const rec: RequestRecord = JSON.parse(e.data);
        onRecordRef.current(rec);
      });

      es.addEventListener('request_update', (e) => {
        const rec: RequestRecord = JSON.parse(e.data);
        onRecordUpdateRef.current(rec);
      });

      es.addEventListener('auth_expired', () => {
        es.close();
        esRef.current = null;
        onStatusChangeRef.current('disconnected');
        onAuthExpiredRef.current();
      });

      es.addEventListener('error', () => {
        es.close();
        esRef.current = null;
        onStatusChangeRef.current('disconnected');
        reconnectTimer.current = setTimeout(connect, 2000);
      });
    };

    connect();
    return close;
  }, [enabled, close]);

  return close;
}
