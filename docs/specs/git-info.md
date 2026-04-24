# Git info API

## Overview
Lightweight git metadata endpoint used by the Sessions sidebar to show
per-session branch, dirty state, ahead/behind counts, and recent commits.
Also powers the hover preview (#440) and will power future previews (#440
related: #456 commit hash hover, #454 URL preview hover).

## Data model

Response shape (`GitInfo` in `internal/api/v1/git.go`):
```go
type GitInfo struct {
  RepoName   string   `json:"repoName"`
  Branch     string   `json:"branch"`
  Dirty      bool     `json:"dirty"`
  Ahead      int      `json:"ahead"`
  Behind     int      `json:"behind"`
  Changed    int      `json:"changed"`
  Untracked  int      `json:"untracked"`
  RecentLogs []string `json:"recentLogs"`
}
```

## API surface

- `GET /api/v1/git/info?dir=<path>` — returns `GitInfo` or 404 if not a
  git repo

Path expansion: `~/` prefix is expanded to the running user's home.

## Invariants

1. Non-repo directories return 404, not an empty `GitInfo`.
2. `Branch` is the short name (e.g. `main`), not a full ref.
3. `RepoName` is extracted from the origin remote URL (SSH or HTTPS),
   stripping `.git` suffix. Missing remote → empty string.
4. `Dirty` is true iff `git status --porcelain` has any lines.
5. `Ahead` / `Behind` are counted against `@{upstream}`; missing upstream
   leaves both zero.
6. `RecentLogs` is the oneline form of the last 5 commits.

## Known edge cases

- **Detached HEAD**: `rev-parse --abbrev-ref HEAD` returns `HEAD`. Decide
  whether to surface that literally or substitute something friendlier.
- **No commits yet**: `git log` fails; `RecentLogs` should be empty.
- **No upstream**: `rev-list` errors; `Ahead`/`Behind` stay zero.
- **Submodules**: we don't walk into submodules.

## Test matrix

Legend: ✅ covered, ◻ planned, 🐛 test caught a real bug.

| # | Scenario | Unit | Integration | Status | Notes |
|---|----------|------|-------------|--------|-------|
| 1 | Missing `dir` param → 400 | ✅ | | done | |
| 2 | Non-repo dir → 404 | ✅ | | done | |
| 3 | Clean repo reports branch + zero counts | ✅ | | done | |
| 4 | Dirty repo with staged + unstaged | ✅ | | done | |
| 5 | Untracked files counted separately | ✅ | | done | |
| 6 | Ahead counted against upstream | ✅ | | done | bare clone as origin |
| 7 | No upstream → ahead/behind stay zero | ✅ | | done | |
| 8 | RepoName parsed from SSH URL | ✅ | | done | |
| 9 | RepoName parsed from HTTPS URL | ✅ | | done | |
| 10 | RepoName empty with no remote 🐛 | ✅ | | done | caught: stderr from `git remote get-url` was being parsed as URL |
| 11 | RecentLogs truncated to 5 | ✅ | | done | |
| 12 | `~/` path expansion | ✅ | | done | 404 on non-existent path under home |

Backend unit coverage is landed in `internal/api/v1/git_test.go` and uses
the new `testhelpers.NewGitRepo` fixture for ephemeral git repos.
`testhelpers.GitRepo` is available for any future test that needs a
scratch git repo.
