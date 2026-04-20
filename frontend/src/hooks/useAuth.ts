import { useState, useEffect, useCallback } from 'react';
import { checkAuth, logout as apiLogout } from '../api';

export type AuthState = 'checking' | 'login' | 'authenticated' | 'noauth';

export function useAuth() {
  const [authState, setAuthState] = useState<AuthState>('checking');
  const [loginError, setLoginError] = useState('');

  useEffect(() => {
    checkAuth().then((result) => {
      if (!result.authRequired) {
        setAuthState('noauth');
      } else if (!result.error) {
        setAuthState('authenticated');
      } else {
        setAuthState('login');
      }
    }).catch(() => {
      setAuthState('noauth');
    });
  }, []);

  const handleLoginSuccess = useCallback(() => {
    setAuthState('authenticated');
    setLoginError('');
  }, []);

  const handleLogout = useCallback(() => {
    setAuthState('login');
    apiLogout().catch(() => {});
  }, []);

  const handleAuthExpired = useCallback(() => {
    setAuthState('login');
    setLoginError('Your session has expired. Please log in again.');
  }, []);

  return { authState, loginError, handleLoginSuccess, handleLogout, handleAuthExpired };
}
