import { useEffect, useState } from 'react';
import { get } from '../api/client';

// Embedded at build time via VITE_BUILD_VERSION (see Makefile). Falls back
// to "dev" for local dev servers without a make build.
const EMBEDDED = (import.meta.env.VITE_BUILD_VERSION as string | undefined) || 'dev';

const POLL_MS = 5 * 60 * 1000; // 5 minutes — long-lived tabs stay reachable without hammering the API

interface VersionResponse {
  version: string;
}

// Returns true once the backend's version differs from the one this tab
// was built against. Stays true once tripped — an upgrade is sticky until
// the user reloads, so we don't flicker the banner on transient responses.
export function useVersionCheck(): { updateAvailable: boolean; embedded: string; latest: string | null } {
  const [updateAvailable, setUpdateAvailable] = useState(false);
  const [latest, setLatest] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;

    const check = async () => {
      try {
        const res = await get<VersionResponse>('/version');
        if (cancelled || !res?.version) return;
        setLatest(res.version);
        if (res.version !== EMBEDDED && EMBEDDED !== 'dev' && res.version !== 'dev') {
          setUpdateAvailable(true);
        }
      } catch {
        // Network blip — try again on the next poll.
      }
    };

    check();
    const id = setInterval(check, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  return { updateAvailable, embedded: EMBEDDED, latest };
}
