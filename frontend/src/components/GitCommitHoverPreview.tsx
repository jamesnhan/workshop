import { useEffect, useState } from 'react';
import { get } from '../api/client';
import { HoverPreview } from './HoverPreview';

interface CommitPreview {
  shortSha: string;
  fullSha: string;
  subject: string;
  body: string;
  author: string;
  date: string;
  diffStat: string;
}

interface Props {
  sha: string;
  repoDir: string;
  x: number;
  y: number;
  pinned?: boolean;
}

const previewCache = new Map<string, CommitPreview | null>();

function relativeDate(dateStr: string): string {
  try {
    const ms = Date.now() - new Date(dateStr).getTime();
    const mins = Math.floor(ms / 60000);
    if (mins < 1) return 'just now';
    if (mins < 60) return `${mins}m ago`;
    const hrs = Math.floor(mins / 60);
    if (hrs < 24) return `${hrs}h ago`;
    const days = Math.floor(hrs / 24);
    if (days < 30) return `${days}d ago`;
    const months = Math.floor(days / 30);
    return `${months}mo ago`;
  } catch { return dateStr; }
}

export function GitCommitHoverPreview({ sha, repoDir, x, y, pinned }: Props) {
  const [preview, setPreview] = useState<CommitPreview | null | undefined>(previewCache.get(sha));

  useEffect(() => {
    if (previewCache.has(sha)) {
      setPreview(previewCache.get(sha));
      return;
    }
    let cancelled = false;
    const timer = setTimeout(() => {
      get<CommitPreview>(`/git/commit?dir=${encodeURIComponent(repoDir)}&sha=${encodeURIComponent(sha)}`)
        .then((p) => {
          previewCache.set(sha, p);
          if (!cancelled) setPreview(p);
        })
        .catch(() => {
          previewCache.set(sha, null);
          if (!cancelled) setPreview(null);
        });
    }, 200);
    return () => { cancelled = true; clearTimeout(timer); };
  }, [sha, repoDir]);

  if (!preview?.subject) return null;

  const bodyLines = preview.body ? preview.body.split('\n').slice(0, 3).join('\n') : '';

  return (
    <HoverPreview x={x} y={y} width={400} maxHeight={350} className={`git-commit-hover-preview${pinned ? ' hover-pinned-inline' : ''}`}>
      <div className="git-commit-header">
        <span className="git-commit-sha">{preview.shortSha}</span>
        <span className="git-commit-date">{relativeDate(preview.date)}</span>
      </div>
      <div className="git-commit-subject">{preview.subject}</div>
      <div className="git-commit-author">by {preview.author}</div>
      {bodyLines && <div className="git-commit-body">{bodyLines}</div>}
      {preview.diffStat && <pre className="git-commit-diff">{preview.diffStat}</pre>}
    </HoverPreview>
  );
}
