package v1

import (
	"net/http"
	"os"
	"os/exec"
	"strings"
)

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

func (a *API) handleGitInfo(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	if dir == "" {
		a.jsonError(w, "dir is required", http.StatusBadRequest)
		return
	}

	// Expand ~
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home + dir[1:]
		}
	}

	info := GitInfo{}

	// Branch — if not a git repo, return empty info (not 404)
	out, err := runGit(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		a.jsonOK(w, nil)
		return
	}
	info.Branch = out

	// Repo name from remote URL (e.g. git@github.com:user/workshop.git → workshop).
	// Ignore the output entirely if git returned an error — CombinedOutput
	// mixes in stderr, so on missing remote we'd otherwise try to parse
	// "No such remote 'origin'" as a URL.
	remoteURL, remoteErr := runGit(dir, "remote", "get-url", "origin")
	if remoteErr == nil && remoteURL != "" {
		name := remoteURL
		// Strip .git suffix
		name = strings.TrimSuffix(name, ".git")
		// Get last path component (handles both SSH and HTTPS URLs)
		if idx := strings.LastIndex(name, "/"); idx >= 0 {
			name = name[idx+1:]
		} else if idx := strings.LastIndex(name, ":"); idx >= 0 {
			name = name[idx+1:]
		}
		info.RepoName = name
	}

	// Status (porcelain for parsing)
	statusOut, _ := runGit(dir, "status", "--porcelain")
	if statusOut != "" {
		info.Dirty = true
		for _, line := range strings.Split(statusOut, "\n") {
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "??") {
				info.Untracked++
			} else {
				info.Changed++
			}
		}
	}

	// Ahead/behind
	abOut, _ := runGit(dir, "rev-list", "--left-right", "--count", "HEAD...@{upstream}")
	if abOut != "" {
		parts := strings.Fields(abOut)
		if len(parts) == 2 {
			for _, c := range parts[0] {
				info.Ahead = info.Ahead*10 + int(c-'0')
			}
			for _, c := range parts[1] {
				info.Behind = info.Behind*10 + int(c-'0')
			}
		}
	}

	// Recent commits
	logOut, _ := runGit(dir, "log", "--oneline", "-5", "--no-decorate")
	if logOut != "" {
		info.RecentLogs = strings.Split(strings.TrimSpace(logOut), "\n")
	}

	a.jsonOK(w, info)
}

func runGit(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}
