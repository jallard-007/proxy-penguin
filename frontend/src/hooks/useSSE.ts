import { useEffect, useRef, useCallback } from 'react';
import type { RequestRecord } from '../types';

type SSEStatus = 'connecting' | 'connected' | 'disconnected';

interface UseSSEOptions {
  onRecord: (rec: RequestRecord) => void;
  onAuthExpired: () => void;
  onStatusChange: (status: SSEStatus) => void;
  enabled: boolean;
}

export function useSSE({ onRecord, onAuthExpired, onStatusChange, enabled }: UseSSEOptions) {
  const esRef = useRef<EventSource | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout>>();

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

      const es = new EventSource('/api/events/stream');
      esRef.current = es;

      es.addEventListener('connected', () => {
        onStatusChange('connected');
      });

      es.addEventListener('request', (e) => {
        const rec: RequestRecord = JSON.parse(e.data);
        onRecord(rec);
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
  }, [enabled, onRecord, onAuthExpired, onStatusChange, close]);

  return close;
}
