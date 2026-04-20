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
  const connIdRef = useRef<string | null>(null);
  const reconnectTimer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  const enabledRef = useRef(enabled);

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
  enabledRef.current = enabled;

  const notifyDisconnect = useCallback((connId: string | null) => {
    if (!connId) {
      return;
    }

    const url = `/api/events/disconnect?cid=${encodeURIComponent(connId)}`;

    if (navigator.sendBeacon) {
      navigator.sendBeacon(url, new Blob([], { type: 'application/json' }));
      return;
    }

    void fetch(url, {
      method: 'POST',
      keepalive: true,
      credentials: 'same-origin',
    });
  }, []);

  const close = useCallback(() => {
    if (reconnectTimer.current) {
      clearTimeout(reconnectTimer.current);
      reconnectTimer.current = undefined;
    }
    if (esRef.current) {
      esRef.current.close();
      esRef.current = null;
    }
    notifyDisconnect(connIdRef.current);
    connIdRef.current = null;
  }, [notifyDisconnect]);

  useEffect(() => {
    const connect = () => {
      if (!enabledRef.current || document.visibilityState !== 'visible') {
        return;
      }
      if (esRef.current) {
        return;
      }

      onStatusChangeRef.current('connecting');

      const lastId = getLastIdRef.current();
      const connId = crypto.randomUUID();
      connIdRef.current = connId;
      const qs = new URLSearchParams({ cid: connId });
      if (lastId > 0) {
        qs.set('after_id', String(lastId));
      }
      const url = `/api/events/stream?${qs.toString()}`;
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
        notifyDisconnect(connIdRef.current);
        connIdRef.current = null;
        onStatusChangeRef.current('disconnected');
        onAuthExpiredRef.current();
      });

      es.addEventListener('error', () => {
        es.close();
        esRef.current = null;
        notifyDisconnect(connIdRef.current);
        connIdRef.current = null;
        onStatusChangeRef.current('disconnected');
        if (enabledRef.current && document.visibilityState === 'visible') {
          reconnectTimer.current = setTimeout(connect, 2000);
        }
      });
    };

    if (!enabled) {
      close();
      return;
    }

    const handleVisibilityChange = () => {
      if (document.visibilityState === 'hidden') {
        close();
        return;
      }

      if (document.visibilityState === 'visible') {
        connect();
      }
    };

    const handlePageHide = () => {
      close();
    };

    document.addEventListener('visibilitychange', handleVisibilityChange);
    window.addEventListener('pagehide', handlePageHide);

    if (document.visibilityState === 'visible') {
      connect();
    }

    return () => {
      document.removeEventListener('visibilitychange', handleVisibilityChange);
      window.removeEventListener('pagehide', handlePageHide);
      close();
    };
  }, [enabled, close, notifyDisconnect]);

  return close;
}
