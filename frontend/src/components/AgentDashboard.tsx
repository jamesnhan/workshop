import { useEffect, useState, useCallback, useMemo } from 'react';
import { get } from '../api/client';
import AnsiToHtml from 'ansi-to-html';
import DOMPurify from 'dompurify';
import { ChibiAvatar, variantFromName, type ChibiState } from './ChibiAvatar';

interface PaneInfo {
  id: string;
  target: string;
  windowName: string;
  command: string;
  path: string;
  width: number;
  height: number;
  active: boolean;
}

interface ConsensusRun {
  id: string;
  prompt: string;
  status: string;
  agentOutputs: {
    name: string;
    model: string;
    provider: string;
    target: string;
    status: string;
    needsInput: boolean;
    inputPrompt: string;
  }[];
  coordinatorPane: string;
}

interface AgentInfo {
  target: string;
  name: string;
  command: string;
  provider: string;
  path: string;
  status: 'working' | 'idle' | 'needs_input' | 'done' | 'unknown';
  lastOutput: string;
  consensusId?: string;
  model?: string;
}

interface Props {
  onNavigateToPane: (target: string) => void;
  sfwMode?: boolean;
  nsfwProjects?: string[];
}

const ansiConverter = new AnsiToHtml({ fg: '#cdd6f4', bg: 'transparent' });

function statusColor(status: string): string {
  switch (status) {
    case 'working': return 'var(--accent)';
    case 'idle': return 'var(--text-muted)';
    case 'needs_input': return 'var(--warning)';
    case 'done': return 'var(--success)';
    default: return 'var(--text-dim)';
  }
}

export function AgentDashboard({ onNavigateToPane, sfwMode = false, nsfwProjects = [] }: Props) {
  const [agents, setAgents] = useState<AgentInfo[]>([]);
  const [loading, setLoading] = useState(true);

  const refresh = useCallback(async () => {
    try {
      // Get all panes
      const panes = await get<PaneInfo[]>('/panes') ?? [];

      // Get consensus runs
      let consensusRuns: ConsensusRun[] = [];
      try {
        consensusRuns = await get<ConsensusRun[]>('/consensus') ?? [];
      } catch {}

      // Build consensus agent map
      const consensusAgents = new Map<string, { consensusId: string; status: string; needsInput: boolean; model: string; provider: string }>();
      for (const run of consensusRuns) {
        for (const agent of (run.agentOutputs || [])) {
          if (agent.target) {
            consensusAgents.set(agent.target, {
              consensusId: run.id,
              status: agent.status,
              needsInput: agent.needsInput,
              model: agent.model,
              provider: agent.provider || 'claude',
            });
          }
        }
        if (run.coordinatorPane) {
          consensusAgents.set(run.coordinatorPane, {
            consensusId: run.id,
            status: run.status === 'done' ? 'completed' : 'running',
            needsInput: false,
            model: 'opus',
            provider: 'claude',
          });
        }
      }

      // Filter to agent-like panes (running claude, gemini, or codex)
      const agentCommands = ['claude', 'gemini', 'codex'];
      const agentPanes = panes.filter((p) =>
        agentCommands.includes(p.command) || agentCommands.includes(p.windowName) || consensusAgents.has(p.target)
      );

      // Capture last output for each (batched with concurrency limit)
      // TODO: move to WS push for agent state instead of polling+capture
      const captureResults = new Map<string, string>();
      const BATCH_SIZE = 6;
      for (let i = 0; i < agentPanes.length; i += BATCH_SIZE) {
        const batch = agentPanes.slice(i, i + BATCH_SIZE);
        const results = await Promise.all(
          batch.map(async (pane) => {
            try {
              const session = pane.target.split(':')[0];
              const res = await get<{ output: string }>(`/sessions/${session}/capture?target=${encodeURIComponent(pane.target)}&lines=8`);
              return { target: pane.target, output: res?.output ?? '' };
            } catch {
              return { target: pane.target, output: '' };
            }
          })
        );
        for (const r of results) captureResults.set(r.target, r.output);
      }

      const agentInfos: AgentInfo[] = agentPanes.map((pane) => {
          const lastOutput = captureResults.get(pane.target) ?? '';

          const consensus = consensusAgents.get(pane.target);
          // Determine provider from command name or consensus data
          const provider = consensus?.provider ?? (['gemini', 'codex'].includes(pane.command) ? pane.command : 'claude');
          let status: AgentInfo['status'] = 'unknown';

          if (consensus) {
            if (consensus.needsInput) status = 'needs_input';
            else if (consensus.status === 'completed') status = 'done';
            else status = 'working';
          } else {
            // Strip ANSI escape codes for cleaner matching
            const plain = lastOutput.replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '');
            const lower = plain.toLowerCase();
            // Check last few lines for prompt patterns (tmux borders may follow the prompt)
            const lines = plain.split('\n').map((l) => l.trim()).filter((l) => l && !/^[─━┄┈═]+$/.test(l));
            const lastLines = lines.slice(-3).join(' ').toLowerCase();

            // Common input patterns (all providers)
            if (lower.includes('do you want to proceed') || lower.includes('(y/n)') || lower.includes('approve')) {
              status = 'needs_input';
            } else if (provider === 'claude') {
              // Claude-specific patterns
              if (lower.includes('worked for') || lower.includes('baked for') || lower.includes('sautéed for') || lower.includes('crunched for')) {
                status = 'done';
              } else if (lastLines.includes('accept edits') || lastLines.includes('esc to cancel') || lastLines.includes('ctrl+e to explain')) {
                status = 'idle';
              } else if (lines.some((l) => /❯\s*$/.test(l))) {
                status = 'idle';
              } else if (plain.includes('…') || lower.includes('thinking') || lower.includes('running')) {
                status = 'working';
              } else {
                status = 'working';
              }
            } else if (provider === 'gemini') {
              // Gemini CLI patterns
              if (lower.includes('✦')) {
                status = 'done';
              } else if (lines.some((l) => /^>\s*$/.test(l))) {
                status = 'idle';
              } else {
                status = 'working';
              }
            } else if (provider === 'codex') {
              // Codex CLI patterns
              if (lower.includes('completed in') || lower.includes('done in')) {
                status = 'done';
              } else if (lines.some((l) => /[>$#]\s*$/.test(l))) {
                status = 'idle';
              } else {
                status = 'working';
              }
            } else {
              // Generic fallback
              if (lines.some((l) => /[❯>$#]\s*$/.test(l))) {
                status = 'idle';
              } else {
                status = 'working';
              }
            }
          }

          return {
            target: pane.target,
            name: pane.target.split(':')[0],
            command: pane.command,
            provider,
            path: pane.path,
            status,
            lastOutput,
            consensusId: consensus?.consensusId,
            model: consensus?.model,
          };
      });

      setAgents(agentInfos);
    } catch (err) {
      console.error('Dashboard refresh failed:', err);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
    const interval = setInterval(refresh, 5000);
    return () => clearInterval(interval);
  }, [refresh]);

  const nsfwSet = new Set(nsfwProjects.map((p) => p.toLowerCase()));
  const visibleAgents = sfwMode ? agents.filter((a) => !nsfwSet.has(a.name.toLowerCase())) : agents;
  const needsInput = visibleAgents.filter((a) => a.status === 'needs_input');
  const working = visibleAgents.filter((a) => a.status === 'working');
  const idle = visibleAgents.filter((a) => a.status === 'idle');
  const done = visibleAgents.filter((a) => a.status === 'done');

  return (
    <div className="dashboard-overlay">
      <div className="dashboard-header">
        <div className="dashboard-stats">
          {needsInput.length > 0 && <span className="dash-stat needs-input">⚠️ {needsInput.length} needs input</span>}
          {working.length > 0 && <span className="dash-stat working">⏳ {working.length} working</span>}
          {idle.length > 0 && <span className="dash-stat idle">💤 {idle.length} idle</span>}
          {done.length > 0 && <span className="dash-stat done">✅ {done.length} done</span>}
        </div>
        <button className="btn-small" onClick={refresh}>Refresh</button>
      </div>

      <div className="dashboard-grid">
        {loading && <div className="dashboard-empty">Loading agents...</div>}
        {!loading && agents.length === 0 && (
          <div className="dashboard-empty">No agents running. Launch one via Ctrl+P → Agent tab.</div>
        )}

        {/* Needs input first */}
        {needsInput.map((agent) => (
          <AgentCard key={agent.target} agent={agent} onNavigate={onNavigateToPane} />
        ))}
        {working.map((agent) => (
          <AgentCard key={agent.target} agent={agent} onNavigate={onNavigateToPane} />
        ))}
        {idle.map((agent) => (
          <AgentCard key={agent.target} agent={agent} onNavigate={onNavigateToPane} />
        ))}
        {done.map((agent) => (
          <AgentCard key={agent.target} agent={agent} onNavigate={onNavigateToPane} />
        ))}
      </div>
    </div>
  );
}

function AgentCard({ agent, onNavigate }: { agent: AgentInfo; onNavigate: (target: string) => void }) {
  const outputHtml = useMemo(
    () => DOMPurify.sanitize(ansiConverter.toHtml(agent.lastOutput.slice(-500))),
    [agent.lastOutput]
  );

  return (
    <div
      className={`dashboard-card status-${agent.status}`}
      onClick={() => onNavigate(agent.target)}
    >
      <div className="dashboard-card-header">
        <ChibiAvatar state={(agent.status === 'unknown' ? 'idle' : agent.status) as ChibiState} variant={variantFromName(agent.name)} size="md" />
        <span className="dashboard-card-name">{agent.name}</span>
        {agent.provider && agent.provider !== 'claude' && <span className="dashboard-card-provider">{agent.provider}</span>}
        {agent.model && <span className="dashboard-card-model">{agent.model}</span>}
        <span className="dashboard-card-status" style={{ color: statusColor(agent.status) }}>
          {agent.status.replace('_', ' ')}
        </span>
      </div>
      <div className="dashboard-card-target">{agent.target}</div>
      {agent.path && <div className="dashboard-card-path">{agent.path}</div>}
      {agent.consensusId && <div className="dashboard-card-consensus">Consensus: {agent.consensusId}</div>}
      <pre
        className="dashboard-card-output"
        dangerouslySetInnerHTML={{ __html: outputHtml }}
      />
    </div>
  );
}
