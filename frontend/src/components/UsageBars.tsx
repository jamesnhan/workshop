import { useState, useEffect, useCallback } from 'react';
import { get } from '../api/client';
import { isHoverPinned, onHoverPinChange } from '../hooks/useHoverPin';

interface WeeklyUsage {
  totalOutput: number;
  opusOutput: number;
  sonnetOutput: number;
  haikuOutput: number;
  weekStart: string;
  weekEnd: string;
  opusResetAt?: string;
  sonnetResetAt?: string;
  sessions: { sessionId: string; slug: string; outputTokens: number; model: string; turnCount: number; lastActivity: string }[];
}

// Claude Max (20x) weekly limits — approximate from observed behavior
const ALL_MODELS_LIMIT = 62_500_000;
const SONNET_ONLY_LIMIT = 250_000_000;

function formatTokens(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(0)}k`;
  return String(n);
}

function formatResetTime(iso: string | undefined): string {
  if (!iso) return 'Unknown';
  const d = new Date(iso);
  const now = new Date();
  const diffMs = d.getTime() - now.getTime();
  if (diffMs <= 0) return 'Now';

  const hours = Math.floor(diffMs / 3600_000);
  const mins = Math.floor((diffMs % 3600_000) / 60_000);

  if (hours >= 24) {
    return d.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric', hour: 'numeric', minute: '2-digit' });
  }
  if (hours > 0) return `${hours}h ${mins}m`;
  return `${mins}m`;
}

function barColor(pct: number): string {
  if (pct >= 90) return 'var(--error, #f38ba8)';
  if (pct >= 70) return '#f9e2af';
  return 'var(--accent, #89b4fa)';
}

export function UsageBars() {
  const [usage, setUsage] = useState<WeeklyUsage | null>(null);
  const [hovered, setHovered] = useState(false);
  const [pinned, setPinned] = useState(false);
  const [, setTick] = useState(0);

  // Sync with global hover pin state
  useEffect(() => onHoverPinChange(() => {
    if (!isHoverPinned()) { setPinned(false); setHovered(false); }
  }), []);

  const refresh = useCallback(async () => {
    try {
      const data = await get<WeeklyUsage>('/session-usage?weekly=true');
      setUsage(data);
    } catch { /* ignore */ }
  }, []);

  useEffect(() => { refresh(); const id = setInterval(refresh, 60_000); return () => clearInterval(id); }, [refresh]);
  useEffect(() => { const id = setInterval(() => setTick((t) => t + 1), 60_000); return () => clearInterval(id); }, []);

  if (!usage) return null;

  // "All models" = total output (dominated by Opus on Max plan)
  const allPct = Math.min(100, (usage.totalOutput / ALL_MODELS_LIMIT) * 100);
  const sonnetPct = Math.min(100, (usage.sonnetOutput / SONNET_ONLY_LIMIT) * 100);

  return (
    <div className="usage-bars" onMouseEnter={() => { setHovered(true); if (isHoverPinned()) setPinned(true); }} onMouseLeave={() => { if (!isHoverPinned()) setHovered(false); }}>
      <div className="usage-bar-row" title={`All models: ${formatTokens(usage.totalOutput)} — ${allPct.toFixed(0)}%`}>
        <span className="usage-bar-label">A</span>
        <div className="usage-bar-track">
          <div className="usage-bar-fill" style={{ width: `${allPct}%`, background: barColor(allPct) }} />
        </div>
        <span className="usage-bar-pct">{allPct.toFixed(0)}%</span>
      </div>
      <div className="usage-bar-row" title={`Sonnet only: ${formatTokens(usage.sonnetOutput)} — ${sonnetPct.toFixed(0)}%`}>
        <span className="usage-bar-label">S</span>
        <div className="usage-bar-track">
          <div className="usage-bar-fill" style={{ width: `${sonnetPct}%`, background: barColor(sonnetPct) }} />
        </div>
        <span className="usage-bar-pct">{sonnetPct.toFixed(0)}%</span>
      </div>

      {(hovered || pinned) && (
        <div className={`usage-tooltip${isHoverPinned() ? ' hover-pinned-inline' : ''}`}>
          <h4>Weekly Usage (local estimate)</h4>
          <div className="usage-tooltip-row">
            <span>All models</span>
            <span>{allPct.toFixed(0)}% — {formatTokens(usage.totalOutput)}</span>
          </div>
          <div className="usage-tooltip-row muted">
            <span>Resets</span>
            <span>{formatResetTime(usage.opusResetAt)}</span>
          </div>
          <div className="usage-tooltip-divider" />
          <div className="usage-tooltip-row">
            <span>Sonnet only</span>
            <span>{sonnetPct.toFixed(0)}% — {formatTokens(usage.sonnetOutput)}</span>
          </div>
          <div className="usage-tooltip-row muted">
            <span>Resets</span>
            <span>{formatResetTime(usage.sonnetResetAt)}</span>
          </div>
          <div className="usage-tooltip-divider" />
          <div className="usage-tooltip-row">
            <span>Sessions this week</span>
            <span>{usage.sessions.length}</span>
          </div>
          <div className="usage-tooltip-row muted" style={{ fontSize: '0.65rem', opacity: 0.6 }}>
            <span>Estimates from local JSONL — check /usage for exact figures</span>
          </div>
        </div>
      )}
    </div>
  );
}
