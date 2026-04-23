package v1

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SessionUsage struct {
	SessionID    string         `json:"sessionId"`
	Slug         string         `json:"slug"`
	InputTokens  int64          `json:"inputTokens"`
	OutputTokens int64          `json:"outputTokens"`
	CacheRead    int64          `json:"cacheRead"`
	CacheCreate  int64          `json:"cacheCreate"`
	TurnCount    int            `json:"turnCount"`
	Model        string         `json:"model"`
	StartedAt    string         `json:"startedAt"`
	LastActivity string         `json:"lastActivity"`
	ByModel          map[string]int64  `json:"byModel,omitempty"`          // model → output tokens
	EarliestByModel  map[string]string `json:"earliestByModel,omitempty"`  // model → earliest timestamp
}

type WeeklyUsage struct {
	Sessions       []SessionUsage `json:"sessions"`
	TotalOutput    int64          `json:"totalOutput"`
	OpusOutput     int64          `json:"opusOutput"`
	SonnetOutput   int64          `json:"sonnetOutput"`
	HaikuOutput    int64          `json:"haikuOutput"`
	WeekStart      string         `json:"weekStart"`
	WeekEnd        string         `json:"weekEnd"`
	// Rolling window resets: computed from earliest usage in each tier's 7-day window
	OpusResetAt    string         `json:"opusResetAt,omitempty"`
	SonnetResetAt  string         `json:"sonnetResetAt,omitempty"`
}

// handleSessionUsage returns token usage for the current or specified session.
func (a *API) handleSessionUsage(w http.ResponseWriter, r *http.Request) {
	sessionID := r.URL.Query().Get("session")
	weekly := r.URL.Query().Get("weekly") == "true"

	home, _ := os.UserHomeDir()
	projectsDir := filepath.Join(home, ".claude", "projects")

	var sessions []SessionUsage
	windowStart := time.Now().Add(-7 * 24 * time.Hour)

	filepath.Walk(projectsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".jsonl") {
			return nil
		}
		if sessionID != "" {
			base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
			if base != sessionID {
				return nil
			}
		}
		if weekly {
			// Only include files modified in the last 7 days
			if time.Since(info.ModTime()) > 7*24*time.Hour {
				return nil
			}
		}

		su := parseSessionUsage(path, windowStart)
		if su.TurnCount > 0 {
			sessions = append(sessions, su)
		}
		return nil
	})

	sort.Slice(sessions, func(i, j int) bool {
		return sessions[i].LastActivity > sessions[j].LastActivity
	})

	if !weekly {
		if sessions == nil {
			sessions = []SessionUsage{}
		}
		a.jsonOK(w, sessions)
		return
	}

	// Aggregate weekly — use the rolling 7-day window
	now := time.Now()

	wu := WeeklyUsage{
		WeekStart: windowStart.Format(time.RFC3339),
		WeekEnd:   now.Format(time.RFC3339),
	}

	// Aggregate usage and find earliest within-window activity per tier.
	// EarliestByModel stores the first turn per model in the entire session,
	// which may predate the rolling window. We clamp to windowStart.
	var earliestOpus, earliestSonnet time.Time

	for _, s := range sessions {
		wu.TotalOutput += s.OutputTokens
		for model, tokens := range s.ByModel {
			isOpus := strings.Contains(model, "opus")
			isSonnet := strings.Contains(model, "sonnet")

			switch {
			case isOpus:
				wu.OpusOutput += tokens
			case isSonnet:
				wu.SonnetOutput += tokens
			case strings.Contains(model, "haiku"):
				wu.HaikuOutput += tokens
			}

			// EarliestByModel is already filtered to within-window turns by the parser.
			if ts, ok := s.EarliestByModel[model]; ok {
				t, _ := time.Parse(time.RFC3339Nano, ts)
				if t.IsZero() {
					t, _ = time.Parse(time.RFC3339, ts)
				}
				if t.IsZero() {
					continue
				}
				if isOpus && (earliestOpus.IsZero() || t.Before(earliestOpus)) {
					earliestOpus = t
				}
				if isSonnet && (earliestSonnet.IsZero() || t.Before(earliestSonnet)) {
					earliestSonnet = t
				}
			}
		}
	}

	// Rolling reset = earliest usage in window + 7 days
	if !earliestOpus.IsZero() {
		wu.OpusResetAt = earliestOpus.Add(7 * 24 * time.Hour).Format(time.RFC3339)
	}
	if !earliestSonnet.IsZero() {
		wu.SonnetResetAt = earliestSonnet.Add(7 * 24 * time.Hour).Format(time.RFC3339)
	}

	wu.Sessions = sessions
	if wu.Sessions == nil {
		wu.Sessions = []SessionUsage{}
	}

	a.jsonOK(w, wu)
}

func parseSessionUsage(path string, windowStart time.Time) SessionUsage {
	f, err := os.Open(path)
	if err != nil {
		return SessionUsage{}
	}
	defer f.Close()

	su := SessionUsage{
		SessionID:       strings.TrimSuffix(filepath.Base(path), ".jsonl"),
		ByModel:         make(map[string]int64),
		EarliestByModel: make(map[string]string),
	}

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		typ, _ := entry["type"].(string)

		// Grab slug and session metadata from any entry that has it
		if s, ok := entry["slug"].(string); ok && s != "" {
			su.Slug = s
		}
		if ts, ok := entry["timestamp"].(string); ok && ts != "" {
			if su.StartedAt == "" {
				su.StartedAt = ts
			}
			su.LastActivity = ts
		}

		if typ == "assistant" {
			msg, _ := entry["message"].(map[string]any)
			if msg == nil {
				continue
			}
			usage, _ := msg["usage"].(map[string]any)
			if usage == nil {
				continue
			}

			model, _ := msg["model"].(string)
			if model != "" {
				su.Model = model
			}

			input, _ := usage["input_tokens"].(float64)
			output, _ := usage["output_tokens"].(float64)
			cacheRead, _ := usage["cache_read_input_tokens"].(float64)
			cacheCreate, _ := usage["cache_creation_input_tokens"].(float64)

			su.InputTokens += int64(input)
			su.OutputTokens += int64(output)
			su.CacheRead += int64(cacheRead)
			su.CacheCreate += int64(cacheCreate)
			su.TurnCount++

			if model != "" {
				su.ByModel[model] += int64(output)
				// Track earliest timestamp per model that's within the rolling window
				ts, _ := entry["timestamp"].(string)
				if ts != "" {
					t, _ := time.Parse(time.RFC3339Nano, ts)
					if t.IsZero() {
						t, _ = time.Parse(time.RFC3339, ts)
					}
					if !t.IsZero() && !t.Before(windowStart) {
						if existing, ok := su.EarliestByModel[model]; !ok || ts < existing {
							su.EarliestByModel[model] = ts
						}
					}
				}
			}
		}
	}

	return su
}
