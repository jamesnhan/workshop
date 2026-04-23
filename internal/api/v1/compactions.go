package v1

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CompactionEvent struct {
	SessionID  string   `json:"sessionId"`
	Timestamp  string   `json:"timestamp"`
	Trigger    string   `json:"trigger"`
	PreTokens  int64    `json:"preTokens"`
	Tools      []string `json:"tools,omitempty"`
	Version    string   `json:"version"`
	Slug       string   `json:"slug"`
}

type TurnSummary struct {
	Timestamp    string `json:"timestamp"`
	DurationMs   int64  `json:"durationMs"`
	MessageCount int    `json:"messageCount"`
}

type SessionCompactions struct {
	SessionID   string            `json:"sessionId"`
	Compactions []CompactionEvent `json:"compactions"`
	Turns       []TurnSummary     `json:"turns"`
}

// handleListCompactions scans Claude Code session JSONL files for compact_boundary events.
// GET /compactions?session=<id> — returns compactions for a specific session
// GET /compactions — returns compactions across all sessions
func (a *API) handleListCompactions(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")

	home, err := os.UserHomeDir()
	if err != nil {
		a.serverErr(w, "home dir", err)
		return
	}

	// Scan all project dirs under ~/.claude/projects/
	projectsDir := filepath.Join(home, ".claude", "projects")
	var allCompactions []CompactionEvent
	var allTurns []TurnSummary

	err = filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		// If filtering by session, check filename matches
		if sessionID != "" {
			base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if base != sessionID {
				return nil
			}
		}

		compactions, turns := parseSessionFile(path)
		allCompactions = append(allCompactions, compactions...)
		allTurns = append(allTurns, turns...)
		return nil
	})
	if err != nil {
		a.serverErr(w, "scan sessions", err)
		return
	}

	// Sort by timestamp desc
	sort.Slice(allCompactions, func(i, j int) bool {
		return allCompactions[i].Timestamp > allCompactions[j].Timestamp
	})
	sort.Slice(allTurns, func(i, j int) bool {
		return allTurns[i].Timestamp > allTurns[j].Timestamp
	})

	// Limit turns to keep response size reasonable
	if len(allTurns) > 200 {
		allTurns = allTurns[:200]
	}

	if allCompactions == nil {
		allCompactions = []CompactionEvent{}
	}
	if allTurns == nil {
		allTurns = []TurnSummary{}
	}

	a.jsonOK(w, SessionCompactions{
		SessionID:   sessionID,
		Compactions: allCompactions,
		Turns:       allTurns,
	})
}

func parseSessionFile(path string) ([]CompactionEvent, []TurnSummary) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	var compactions []CompactionEvent
	var turns []TurnSummary

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		subtype, _ := entry["subtype"].(string)

		if subtype == "compact_boundary" {
			ce := CompactionEvent{
				Timestamp: strVal(entry, "timestamp"),
				Version:   strVal(entry, "version"),
				Slug:      strVal(entry, "slug"),
				SessionID: strVal(entry, "sessionId"),
			}
			if meta, ok := entry["compactMetadata"].(map[string]any); ok {
				ce.Trigger, _ = meta["trigger"].(string)
				if pt, ok := meta["preTokens"].(float64); ok {
					ce.PreTokens = int64(pt)
				}
				if tools, ok := meta["preCompactDiscoveredTools"].([]any); ok {
					for _, t := range tools {
						if s, ok := t.(string); ok {
							ce.Tools = append(ce.Tools, s)
						}
					}
				}
			}
			compactions = append(compactions, ce)
		}

		if subtype == "turn_duration" {
			ts := TurnSummary{
				Timestamp: strVal(entry, "timestamp"),
			}
			if d, ok := entry["durationMs"].(float64); ok {
				ts.DurationMs = int64(d)
			}
			if mc, ok := entry["messageCount"].(float64); ok {
				ts.MessageCount = int(mc)
			}
			turns = append(turns, ts)
		}
	}

	return compactions, turns
}

func strVal(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}
