package v1

import (
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
)

type CommitPreview struct {
	ShortSha string `json:"shortSha"`
	FullSha  string `json:"fullSha"`
	Subject  string `json:"subject"`
	Body     string `json:"body"`
	Author   string `json:"author"`
	Date     string `json:"date"`
	DiffStat string `json:"diffStat"`
}

var shaRegex = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

func (a *API) handleGitCommit(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	sha := r.URL.Query().Get("sha")

	if dir == "" {
		a.jsonError(w, "dir is required", http.StatusBadRequest)
		return
	}
	if !shaRegex.MatchString(sha) {
		a.jsonError(w, "invalid commit hash format", http.StatusBadRequest)
		return
	}

	// Expand ~
	if strings.HasPrefix(dir, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			dir = home + dir[1:]
		}
	}

	// Verify commit exists
	if _, err := runGit(dir, "cat-file", "-e", sha); err != nil {
		a.jsonError(w, "commit not found", http.StatusNotFound)
		return
	}

	// Use a delimiter to safely parse fields (body can contain newlines)
	const sep = "|||SEP|||"
	format := "%h" + sep + "%H" + sep + "%s" + sep + "%b" + sep + "%an" + sep + "%ai"
	logOut, err := runGit(dir, "log", "-1", "--format="+format, sha)
	if err != nil {
		a.jsonError(w, "failed to retrieve commit log", http.StatusInternalServerError)
		return
	}

	fields := strings.SplitN(logOut, sep, 6)
	if len(fields) < 6 {
		a.jsonError(w, "failed to parse commit data", http.StatusInternalServerError)
		return
	}

	// Get diff stat (sha^ fails for root commits, fallback to empty)
	diffStat, _ := runGit(dir, "diff", "--stat", fmt.Sprintf("%s^..%s", sha, sha))

	result := CommitPreview{
		ShortSha: strings.TrimSpace(fields[0]),
		FullSha:  strings.TrimSpace(fields[1]),
		Subject:  strings.TrimSpace(fields[2]),
		Body:     strings.TrimSpace(fields[3]),
		Author:   strings.TrimSpace(fields[4]),
		Date:     strings.TrimSpace(fields[5]),
		DiffStat: strings.TrimSpace(diffStat),
	}

	a.jsonOK(w, result)
}
