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
      onStatusChange('connecting');

      const lastId = getLastId();
      const url = lastId > 0 ? `/api/events/stream?after_id=${lastId}` : '/api/events/stream';
      const es = new EventSource(url);
      esRef.current = es;

      es.addEventListener('connected', () => {
        onStatusChange('connected');
      });

      es.addEventListener('request', (e) => {
        const rec: RequestRecord = JSON.parse(e.data);
        onRecord(rec);
      });

      es.addEventListener('request_update', (e) => {
        const rec: RequestRecord = JSON.parse(e.data);
        onRecordUpdate(rec);
      });

      es.addEventListener('auth_expired', () => {
        es.close();
        esRef.current = null;
        onStatusChange('disconnected');
        onAuthExpired();
      });

      es.addEventListener('error', () => {
        es.close();
        esRef.current = null;
        onStatusChange('disconnected');
        reconnectTimer.current = setTimeout(connect, 2000);
      });
    };

    connect();
    return close;
  }, [enabled, onRecord, onRecordUpdate, onAuthExpired, onStatusChange, getLastId, close]);

  return close;
}
