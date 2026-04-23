import { HoverPreview } from './HoverPreview';

export interface GitInfo {
  repoName?: string;
  branch: string;
  dirty: boolean;
  ahead: number;
  behind: number;
  changed: number;
  untracked: number;
  recentLogs?: string[];
}

interface Props {
  info: GitInfo;
  x: number;
  y: number;
  pinned?: boolean;
}

export function GitInfoHoverPreview({ info, x, y, pinned }: Props) {
  return (
    <HoverPreview x={x} y={y} width={380} maxHeight={320} className={`git-hover-preview${pinned ? ' hover-pinned-inline' : ''}`}>
      <div className="git-hover-header">
        <span className="git-hover-branch">{info.branch}</span>
        {info.repoName && <span className="git-hover-repo">{info.repoName}</span>}
        <span className="git-hover-state">{info.dirty ? 'dirty' : 'clean'}</span>
      </div>
      <div className="git-hover-meta">
        {info.ahead > 0 && <span title="commits ahead of upstream">↑ {info.ahead} ahead</span>}
        {info.behind > 0 && <span title="commits behind upstream">↓ {info.behind} behind</span>}
        {info.changed > 0 && <span title="tracked file changes">~ {info.changed} changed</span>}
        {info.untracked > 0 && <span title="untracked files">+ {info.untracked} untracked</span>}
        {!info.dirty && info.ahead === 0 && info.behind === 0 && <span>up to date</span>}
      </div>
      {info.recentLogs && info.recentLogs.length > 0 && (
        <div className="git-hover-log">
          <div className="git-hover-log-title">Recent commits</div>
          {info.recentLogs.slice(0, 5).map((line, i) => (
            <div key={i} className="git-hover-log-line">{line}</div>
          ))}
        </div>
      )}
    </HoverPreview>
  );
}
