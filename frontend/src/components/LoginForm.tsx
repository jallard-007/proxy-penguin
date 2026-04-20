import { useState, type FormEvent } from 'react';
import { login } from '../api';

interface LoginFormProps {
  onSuccess: () => void;
  initialError?: string;
}

export default function LoginForm({ onSuccess, initialError }: LoginFormProps) {
  const [password, setPassword] = useState('');
  const [error, setError] = useState(initialError ?? '');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const result = await login(password);
      if (result.ok) {
        setPassword('');
        onSuccess();
      } else {
        setError(result.error ?? 'Login failed.');
      }
    } catch {
      setError('Connection error.');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="fixed inset-0 bg-gray-950 flex items-center justify-center z-50">
      <div className="bg-surface border border-border-subtle rounded-xl p-10 w-full max-w-sm">
        <h2 className="text-center text-xl font-semibold mb-6">&#x1F427; Proxy Penguin</h2>
        <form onSubmit={handleSubmit}>
          <input
            type="password"
            placeholder="Password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            className="w-full bg-gray-950 border border-border-subtle text-gray-100 px-3.5 py-2.5 rounded-md text-sm outline-none focus:border-accent mb-3"
            autoFocus
          />
          <button
            type="submit"
            disabled={loading}
            className="w-full bg-accent text-white font-semibold py-2.5 rounded-md text-sm cursor-pointer hover:opacity-90 disabled:opacity-50 disabled:cursor-not-allowed"
          >
            {loading ? 'Authenticating...' : 'Authenticate'}
          </button>
          {error && (
            <p className="text-danger text-sm text-center mt-3">{error}</p>
          )}
        </form>
      </div>
    </div>
  );
}
