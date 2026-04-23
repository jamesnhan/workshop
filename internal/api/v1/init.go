package v1

import (
	"net/http"
	"sync"

	"github.com/jamesnhan/workshop/internal/tmux"
)

// handleInit returns a batch response with sessions, panes, and projects
// in a single round-trip. This dramatically cuts page-load time over
// K8s/HTTPS where each request pays TLS + proxy latency.
func (a *API) handleInit(w http.ResponseWriter, r *http.Request) {
	type initResponse struct {
		Sessions []tmux.Session `json:"sessions"`
		Panes    []tmux.Pane    `json:"panes"`
		Projects []string       `json:"projects"`
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		resp    initResponse
		hasTmux = true
	)

	// Check if tmux is available (not headless without proxy).
	if _, noBridge := a.tmux.(*tmux.NoBridge); noBridge && a.tmuxProxy == nil {
		hasTmux = false
	}

	// Fetch sessions + panes in parallel with DB queries.
	if hasTmux {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sessions, err := a.tmux.ListSessions()
			if err != nil {
				return
			}
			var allPanes []tmux.Pane
			for _, s := range sessions {
				panes, err := a.tmux.ListPanes(s.Name)
				if err != nil {
					continue
				}
				allPanes = append(allPanes, panes...)
			}
			mu.Lock()
			resp.Sessions = sessions
			resp.Panes = allPanes
			mu.Unlock()
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		projects, err := a.db.ListProjects()
		if err != nil {
			return
		}
		mu.Lock()
		resp.Projects = projects
		mu.Unlock()
	}()

	wg.Wait()

	// Ensure nil slices become empty arrays in JSON.
	if resp.Sessions == nil {
		resp.Sessions = []tmux.Session{}
	}
	if resp.Panes == nil {
		resp.Panes = []tmux.Pane{}
	}
	if resp.Projects == nil {
		resp.Projects = []string{}
	}

	a.jsonOK(w, resp)
}
