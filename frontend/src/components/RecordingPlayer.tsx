import { useEffect, useRef, useState, useCallback } from 'react';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { get } from '../api/client';
import { getActiveTheme } from '../themes';

interface Recording {
  id: number;
  name: string;
  target: string;
  cols: number;
  rows: number;
  startedAt: string;
  durationMs: number;
  status: string;
}

interface Frame {
  offsetMs: number;
  data: string;
}

interface Props {
  onClose: () => void;
}

export function RecordingPlayer({ onClose }: Props) {
  const [recordings, setRecordings] = useState<Recording[]>([]);
  const [selectedId, setSelectedId] = useState<number | null>(null);
  const [frames, setFrames] = useState<Frame[]>([]);
  const [playing, setPlaying] = useState(false);
  const [progress, setProgress] = useState(0);
  const [speed, setSpeed] = useState(1);
  const containerRef = useRef<HTMLDivElement>(null);
  const termRef = useRef<Terminal | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const frameIdxRef = useRef(0);

  // Load recordings list
  useEffect(() => {
    get<Recording[]>('/recordings').then((r) => setRecordings(r ?? [])).catch(() => {});
  }, []);

  // Load frames when a recording is selected
  const loadRecording = useCallback(async (id: number) => {
    setSelectedId(id);
    setPlaying(false);
    setProgress(0);
    frameIdxRef.current = 0;
    try {
      const res = await get<{ recording: Recording; frames: Frame[] }>(`/recordings/${id}`);
      setFrames(res?.frames ?? []);
    } catch {
      setFrames([]);
    }
  }, []);

  // Create terminal when frames are loaded
  useEffect(() => {
    if (!containerRef.current || frames.length === 0) return;

    // Clean up old terminal
    if (termRef.current) {
      termRef.current.dispose();
    }

    const theme = getActiveTheme();
    const rec = recordings.find((r) => r.id === selectedId);
    const term = new Terminal({
      cols: rec?.cols || 80,
      rows: rec?.rows || 24,
      fontSize: 14,
      fontFamily: "'CaskaydiaCove Nerd Font Propo', 'JetBrains Mono', monospace",
      theme: { ...theme.terminal },
      disableStdin: true,
      cursorBlink: false,
    });

    const fit = new FitAddon();
    term.loadAddon(fit);
    term.open(containerRef.current);
    fit.fit();
    termRef.current = term;

    return () => {
      term.dispose();
      termRef.current = null;
    };
  }, [frames, selectedId, recordings]);

  // Playback logic
  const playFrame = useCallback(() => {
    const idx = frameIdxRef.current;
    if (idx >= frames.length) {
      setPlaying(false);
      return;
    }

    const frame = frames[idx];
    termRef.current?.write(frame.data);
    frameIdxRef.current = idx + 1;

    const totalDuration = frames[frames.length - 1]?.offsetMs || 1;
    setProgress(Math.round((frame.offsetMs / totalDuration) * 100));

    // Schedule next frame
    if (idx + 1 < frames.length) {
      const delay = (frames[idx + 1].offsetMs - frame.offsetMs) / speed;
      timerRef.current = setTimeout(playFrame, Math.max(1, delay));
    } else {
      setPlaying(false);
    }
  }, [frames, speed]);

  const handlePlay = useCallback(() => {
    if (playing) {
      // Pause
      if (timerRef.current) clearTimeout(timerRef.current);
      setPlaying(false);
      return;
    }

    // If at end, restart
    if (frameIdxRef.current >= frames.length) {
      frameIdxRef.current = 0;
      termRef.current?.write('\x1b[2J\x1b[H');
      setProgress(0);
    }

    setPlaying(true);
    playFrame();
  }, [playing, frames, playFrame]);

  const handleRestart = useCallback(() => {
    if (timerRef.current) clearTimeout(timerRef.current);
    frameIdxRef.current = 0;
    setProgress(0);
    setPlaying(false);
    termRef.current?.write('\x1b[2J\x1b[H');
  }, []);

  const handleDelete = useCallback(async (id: number) => {
    if (!confirm('Delete this recording?')) return;
    await fetch(`/api/v1/recordings/${id}`, { method: 'DELETE' });
    setRecordings((prev) => prev.filter((r) => r.id !== id));
    if (selectedId === id) {
      setSelectedId(null);
      setFrames([]);
    }
  }, [selectedId]);

  // Cleanup on unmount
  useEffect(() => {
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, []);

  const selectedRec = recordings.find((r) => r.id === selectedId);

  return (
    <div className="player-overlay">
      <div className="player-header">
        <h3>Recordings</h3>
        <button className="search-close" onClick={onClose}>x</button>
      </div>
      <div className="player-body">
        <div className="player-list">
          {recordings.length === 0 && <p className="muted">No recordings yet.</p>}
          {recordings.map((r) => (
            <div
              key={r.id}
              className={`player-item${r.id === selectedId ? ' active' : ''}`}
              onClick={() => loadRecording(r.id)}
            >
              <div className="player-item-name">{r.name}</div>
              <div className="player-item-meta">
                {r.target} · {Math.round(r.durationMs / 1000)}s · {r.status}
              </div>
              <button className="notif-dismiss" onClick={(e) => { e.stopPropagation(); handleDelete(r.id); }}>x</button>
            </div>
          ))}
        </div>
        <div className="player-view">
          {!selectedId && (
            <div className="player-empty">Select a recording to replay</div>
          )}
          {selectedId && (
            <>
              <div className="player-controls">
                <button className="btn-create" onClick={handlePlay}>
                  {playing ? '⏸ Pause' : '▶ Play'}
                </button>
                <button className="btn-small" onClick={handleRestart}>⏮ Restart</button>
                <select className="theme-select" value={speed} onChange={(e) => setSpeed(Number(e.target.value))}>
                  <option value={0.5}>0.5x</option>
                  <option value={1}>1x</option>
                  <option value={2}>2x</option>
                  <option value={5}>5x</option>
                  <option value={10}>10x</option>
                </select>
                <div className="player-progress-bar">
                  <div className="player-progress-fill" style={{ width: `${progress}%` }} />
                </div>
                <span className="player-progress-text">{progress}%</span>
                {selectedRec && (
                  <span className="player-duration">{Math.round(selectedRec.durationMs / 1000)}s</span>
                )}
              </div>
              <div ref={containerRef} className="player-terminal" />
            </>
          )}
        </div>
      </div>
    </div>
  );
}
