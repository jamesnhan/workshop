package v1

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/jamesnhan/workshop/internal/tmux"
)

func (a *API) handleLaunchAgent(w http.ResponseWriter, r *http.Request) {
	var cfg tmux.AgentConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		a.jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := a.tmux.LaunchAgent(cfg)
	if err != nil {
		a.serverErr(w, "agent launch failed", err)
		return
	}

	// Record the dispatch if this was launched from a card
	if cfg.CardID > 0 {
		provider := cfg.Provider
		if provider == "" {
			provider = "claude"
		}
		disp, err := a.db.CreateDispatch(cfg.CardID, result.SessionName, result.Target, provider, true)
		if err != nil {
			a.logger.Warn("failed to record dispatch", "err", err)
		} else {
			go a.superviseDispatch(*disp)
		}
	}

	// Pre-register so the monitor doesn't double-broadcast.
	a.status.MarkSeen(result.Target)
	a.status.Broadcast("session_created", map[string]any{
		"target":     result.Target,
		"session":    result.SessionName,
		"background": cfg.Background,
	})

	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, result)
}

func (a *API) handleListDispatches(w http.ResponseWriter, r *http.Request) {
	idStr := r.PathValue("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		a.jsonError(w, "invalid card id", http.StatusBadRequest)
		return
	}
	dispatches, err := a.db.ListDispatches(id)
	if err != nil {
		a.serverErr(w, "list dispatches failed", err)
		return
	}
	if dispatches == nil {
		dispatches = []db.Dispatch{}
	}
	a.jsonOK(w, dispatches)
}

func (a *API) handleListActiveDispatches(w http.ResponseWriter, r *http.Request) {
	dispatches, err := a.db.GetActiveDispatches()
	if err != nil {
		a.serverErr(w, "list active dispatches failed", err)
		return
	}
	if dispatches == nil {
		dispatches = []db.Dispatch{}
	}
	a.jsonOK(w, dispatches)
}

// superviseDispatch polls an agent session, detects when it goes idle (done),
// then updates the dispatch record, adds a note to the card, moves the card to
// review, and kills the session if autoCleanup is set.
func (a *API) superviseDispatch(disp db.Dispatch) {
	target := disp.Target
	sessionName := disp.SessionName
	provider := disp.Provider

	// Phase 1: wait for agent to start working (up to 3 minutes).
	// We know it's working when isAgentReady becomes false.
	started := false
	for i := 0; i < 36; i++ {
		time.Sleep(5 * time.Second)
		out, err := a.tmux.CapturePanePlain(target, 20)
		if err != nil {
			// Session gone already — treat as error
			a.completeDispatch(disp, "error", "Session ended unexpectedly before agent started")
			return
		}
		if !tmux.IsAgentReady(out, provider) {
			started = true
			break
		}
	}
	if !started {
		a.completeDispatch(disp, "error", "Agent never started working")
		return
	}

	// Phase 2: wait for agent to go idle again (up to 3 hours).
	for i := 0; i < 1080; i++ {
		time.Sleep(10 * time.Second)
		out, err := a.tmux.CapturePanePlain(target, 20)
		if err != nil {
			// Session gone — treat as done (agent may have cleaned up itself)
			a.completeDispatch(disp, "done", fmt.Sprintf("Agent session ended (card #%d)", disp.CardID))
			return
		}
		if tmux.IsAgentReady(out, provider) {
			a.completeDispatch(disp, "done", fmt.Sprintf("Agent finished work on card #%d", disp.CardID))
			if disp.AutoCleanup {
				a.tmux.KillSession(sessionName)
			}
			return
		}
	}

	// Timeout — agent ran for 3 hours without completing
	a.completeDispatch(disp, "error", fmt.Sprintf("Agent timed out on card #%d", disp.CardID))
}

// completeDispatch finalises a dispatch record, adds a note to the card,
// moves the card to review (if done), and broadcasts a WS event.
func (a *API) completeDispatch(disp db.Dispatch, status, note string) {
	if err := a.db.CompleteDispatch(disp.ID, status); err != nil {
		a.logger.Warn("complete dispatch failed", "err", err)
	}

	if note != "" {
		a.db.AddNote(disp.CardID, fmt.Sprintf("[agent] %s", note))
	}

	// Move card to review when done (unless already done)
	if status == "done" {
		if card, err := a.db.GetCard(disp.CardID); err == nil && card.Column != "done" {
			a.db.MoveCard(disp.CardID, "review", 0)
			a.db.LogCardEvent(disp.CardID, "moved", card.Column, "review", "agent")
		}
	}

	a.status.Broadcast("dispatch_updated", map[string]any{
		"id":      disp.ID,
		"cardId":  disp.CardID,
		"status":  status,
		"target":  disp.Target,
		"session": disp.SessionName,
	})
}
