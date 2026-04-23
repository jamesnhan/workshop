import { useState, useEffect, type ReactNode, type FormEvent } from 'react';
import { getApiKey, setApiKey } from '../api/client';

/**
 * AuthGate checks whether the Workshop server requires an API key.
 * - If the server responds 200 to /api/v1/health without auth, no key is needed.
 * - If a key is already stored in localStorage, it's validated against /api/v1/cards?limit=1.
 * - Otherwise, a login prompt is shown.
 */
export function AuthGate({ children }: { children: ReactNode }) {
  const [state, setState] = useState<'checking' | 'ok' | 'needs-key'>('checking');
  const [error, setError] = useState('');
  const [input, setInput] = useState('');

  useEffect(() => {
    checkAuth();
  }, []);

  async function checkAuth() {
    const stored = getApiKey();
    // Try a real API call to see if auth is required.
    const headers: Record<string, string> = { 'Content-Type': 'application/json' };
    if (stored) headers['Authorization'] = `Bearer ${stored}`;

    try {
      const res = await fetch('/api/v1/cards?limit=1', { headers });
      if (res.ok) {
        setState('ok');
        return;
      }
      if (res.status === 401) {
        setState('needs-key');
        return;
      }
      // Other error — assume auth is fine, let the app handle it
      setState('ok');
    } catch {
      // Network error — server might be down, let the app handle it
      setState('ok');
    }
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError('');
    const key = input.trim();
    if (!key) return;

    try {
      const res = await fetch('/api/v1/cards?limit=1', {
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${key}`,
        },
      });
      if (res.ok) {
        setApiKey(key);
        setState('ok');
      } else if (res.status === 401) {
        setError('Invalid API key');
      } else {
        setError(`Unexpected response: ${res.status}`);
      }
    } catch {
      setError('Could not connect to Workshop server');
    }
  }

  if (state === 'checking') {
    return (
      <div style={styles.container}>
        <div style={styles.card}>
          <div style={styles.spinner} />
          <p style={styles.text}>Connecting...</p>
        </div>
      </div>
    );
  }

  if (state === 'needs-key') {
    return (
      <div style={styles.container}>
        <div style={styles.card}>
          <h2 style={styles.title}>Workshop</h2>
          <p style={styles.text}>Enter your API key to continue</p>
          <form onSubmit={handleSubmit} style={styles.form}>
            <input
              type="password"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="API key"
              autoFocus
              style={styles.input}
            />
            <button type="submit" style={styles.button}>Connect</button>
          </form>
          {error && <p style={styles.error}>{error}</p>}
        </div>
      </div>
    );
  }

  return <>{children}</>;
}

const styles: Record<string, React.CSSProperties> = {
  container: {
    display: 'flex',
    alignItems: 'center',
    justifyContent: 'center',
    height: '100vh',
    width: '100vw',
    background: 'var(--bg-primary, #1a1b26)',
  },
  card: {
    display: 'flex',
    flexDirection: 'column',
    alignItems: 'center',
    gap: '12px',
    padding: '32px',
    borderRadius: '12px',
    background: 'var(--bg-secondary, #24283b)',
    border: '1px solid var(--border-color, #414868)',
    minWidth: '320px',
  },
  title: {
    margin: 0,
    color: 'var(--text-primary, #c0caf5)',
    fontSize: '1.5rem',
  },
  text: {
    margin: 0,
    color: 'var(--text-secondary, #a9b1d6)',
    fontSize: '0.9rem',
  },
  form: {
    display: 'flex',
    gap: '8px',
    width: '100%',
  },
  input: {
    flex: 1,
    padding: '8px 12px',
    borderRadius: '6px',
    border: '1px solid var(--border-color, #414868)',
    background: 'var(--bg-primary, #1a1b26)',
    color: 'var(--text-primary, #c0caf5)',
    fontSize: '0.9rem',
    outline: 'none',
  },
  button: {
    padding: '8px 16px',
    borderRadius: '6px',
    border: 'none',
    background: 'var(--accent-color, #7aa2f7)',
    color: '#1a1b26',
    fontWeight: 600,
    cursor: 'pointer',
    fontSize: '0.9rem',
  },
  error: {
    margin: 0,
    color: 'var(--error-color, #f7768e)',
    fontSize: '0.85rem',
  },
  spinner: {
    width: '24px',
    height: '24px',
    border: '3px solid var(--border-color, #414868)',
    borderTopColor: 'var(--accent-color, #7aa2f7)',
    borderRadius: '50%',
    animation: 'spin 0.8s linear infinite',
  },
};
