import { describe, it, expect } from 'vitest';
import { groupSessions, OTHER_GROUP } from './Sidebar';

function session(name: string) {
  return { name, windows: 1, attached: false };
}

describe('groupSessions', () => {
  it('groups sessions by git repo name', () => {
    const sessions = [session('workshop'), session('workshop-voice'), session('roblox')];
    const gitInfo = {
      workshop: { repoName: 'workshop' },
      'workshop-voice': { repoName: 'workshop' },
      roblox: { repoName: 'roblox' },
    };
    const result = groupSessions(sessions, gitInfo);
    expect(result).toEqual([
      { project: 'roblox', sessions: [session('roblox')] },
      { project: 'workshop', sessions: [session('workshop'), session('workshop-voice')] },
    ]);
  });

  it('puts sessions without gitInfo under Other', () => {
    const sessions = [session('workshop'), session('scratch')];
    const gitInfo = { workshop: { repoName: 'workshop' } };
    const result = groupSessions(sessions, gitInfo);
    expect(result).toEqual([
      { project: 'workshop', sessions: [session('workshop')] },
      { project: OTHER_GROUP, sessions: [session('scratch')] },
    ]);
  });

  it('sorts projects alphabetically with Other last', () => {
    const sessions = [session('a'), session('b'), session('c'), session('d')];
    const gitInfo = {
      a: { repoName: 'zeta' },
      b: { repoName: 'alpha' },
      c: { repoName: 'mu' },
      // d has no gitInfo → Other
    };
    const result = groupSessions(sessions, gitInfo);
    expect(result.map((g) => g.project)).toEqual(['alpha', 'mu', 'zeta', OTHER_GROUP]);
  });

  it('returns empty array for empty input', () => {
    expect(groupSessions([], {})).toEqual([]);
  });

  it('returns a single group when all sessions share a project', () => {
    const sessions = [session('a'), session('b')];
    const gitInfo = {
      a: { repoName: 'workshop' },
      b: { repoName: 'workshop' },
    };
    const result = groupSessions(sessions, gitInfo);
    expect(result).toHaveLength(1);
    expect(result[0].project).toBe('workshop');
    expect(result[0].sessions).toHaveLength(2);
  });

  it('treats empty repoName string as no gitInfo', () => {
    const sessions = [session('a')];
    const gitInfo = { a: { repoName: '' } };
    const result = groupSessions(sessions, gitInfo);
    expect(result[0].project).toBe(OTHER_GROUP);
  });

  it('preserves session order within a group', () => {
    const sessions = [session('z'), session('a'), session('m')];
    const gitInfo = {
      z: { repoName: 'proj' },
      a: { repoName: 'proj' },
      m: { repoName: 'proj' },
    };
    const result = groupSessions(sessions, gitInfo);
    expect(result[0].sessions.map((s) => s.name)).toEqual(['z', 'a', 'm']);
  });
});
