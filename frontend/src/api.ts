import type { PageResponse } from './types';

export async function fetchRequests(beforeId?: number, limit = 50): Promise<PageResponse> {
  const params = new URLSearchParams();
  if (beforeId !== undefined) {
    params.set('before_id', String(beforeId));
  }
  params.set('limit', String(limit));

  const res = await fetch(`/api/requests?${params}`);
  if (!res.ok) {
    throw new Error(`Failed to fetch requests: ${res.status}`);
  }
  return res.json();
}

export async function checkAuth(): Promise<{ authRequired: boolean; error?: string }> {
  const res = await fetch('/api/auth/check');
  const data = await res.json();
  return { authRequired: data.authRequired, error: data.error };
}

export async function login(password: string): Promise<{ ok: boolean; error?: string }> {
  const res = await fetch('/api/auth/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password }),
  });
  const data = await res.json();
  if (res.ok) {
    return { ok: true };
  }
  return { ok: false, error: data.error || 'Login failed.' };
}

export async function logout(): Promise<void> {
  await fetch('/api/auth/logout', { method: 'POST' });
}
