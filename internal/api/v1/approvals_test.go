package v1

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/jamesnhan/workshop/internal/db"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ListApprovals ---

func TestHandleListApprovals_empty(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodGet, "/approvals", nil)
	w := httptest.NewRecorder()
	api.handleListApprovals(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var approvals []db.ApprovalRequest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&approvals))
	assert.NotNil(t, approvals)
	assert.Empty(t, approvals)
}

func TestHandleListApprovals_withFilter(t *testing.T) {
	api := newDBAPI(t)

	// Create two approvals, resolve one
	req1 := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	_, err := api.db.CreateApproval(req1)
	require.NoError(t, err)

	req2 := &db.ApprovalRequest{Action: "deploy", Details: "v2"}
	id2, err := api.db.CreateApproval(req2)
	require.NoError(t, err)
	require.NoError(t, api.db.ResolveApproval(id2, "approved"))

	// Filter pending only
	req := httptest.NewRequest(http.MethodGet, "/approvals?status=pending", nil)
	w := httptest.NewRecorder()
	api.handleListApprovals(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var approvals []db.ApprovalRequest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&approvals))
	assert.Len(t, approvals, 1)
	assert.Equal(t, "pending", approvals[0].Status)
}

func TestHandleListApprovals_noFilter(t *testing.T) {
	api := newDBAPI(t)

	req1 := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	_, err := api.db.CreateApproval(req1)
	require.NoError(t, err)

	req2 := &db.ApprovalRequest{Action: "deploy", Details: "v2"}
	id2, err := api.db.CreateApproval(req2)
	require.NoError(t, err)
	require.NoError(t, api.db.ResolveApproval(id2, "approved"))

	req := httptest.NewRequest(http.MethodGet, "/approvals", nil)
	w := httptest.NewRecorder()
	api.handleListApprovals(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var approvals []db.ApprovalRequest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&approvals))
	assert.Len(t, approvals, 2)
}

func TestHandleListApprovals_customLimit(t *testing.T) {
	api := newDBAPI(t)

	for i := 0; i < 5; i++ {
		r := &db.ApprovalRequest{Action: "deploy", Details: "v"}
		_, err := api.db.CreateApproval(r)
		require.NoError(t, err)
	}

	req := httptest.NewRequest(http.MethodGet, "/approvals?limit=2", nil)
	w := httptest.NewRecorder()
	api.handleListApprovals(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var approvals []db.ApprovalRequest
	require.NoError(t, json.NewDecoder(w.Body).Decode(&approvals))
	assert.Len(t, approvals, 2)
}

// --- ResolveApproval ---

func TestHandleResolveApproval_approved(t *testing.T) {
	api := newDBAPI(t)

	ar := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	id, err := api.db.CreateApproval(ar)
	require.NoError(t, err)

	body := `{"decision":"approved"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals/1/resolve", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleResolveApproval(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(id), resp["id"])
	assert.Equal(t, "approved", resp["status"])
}

func TestHandleResolveApproval_denied(t *testing.T) {
	api := newDBAPI(t)

	ar := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	_, err := api.db.CreateApproval(ar)
	require.NoError(t, err)

	body := `{"decision":"denied"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals/1/resolve", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleResolveApproval(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, "denied", resp["status"])
}

func TestHandleResolveApproval_invalidDecision(t *testing.T) {
	api := newDBAPI(t)

	ar := &db.ApprovalRequest{Action: "deploy", Details: "v1"}
	_, err := api.db.CreateApproval(ar)
	require.NoError(t, err)

	body := `{"decision":"maybe"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals/1/resolve", strings.NewReader(body))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleResolveApproval(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "decision must be")
}

func TestHandleResolveApproval_invalidID(t *testing.T) {
	api := newDBAPI(t)

	body := `{"decision":"approved"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals/abc/resolve", strings.NewReader(body))
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	api.handleResolveApproval(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleResolveApproval_invalidJSON(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/approvals/1/resolve", strings.NewReader("{bad"))
	req.SetPathValue("id", "1")
	w := httptest.NewRecorder()
	api.handleResolveApproval(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleResolveApproval_nonExistent(t *testing.T) {
	api := newDBAPI(t)

	body := `{"decision":"approved"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals/999/resolve", strings.NewReader(body))
	req.SetPathValue("id", "999")
	w := httptest.NewRecorder()
	api.handleResolveApproval(w, req)

	// No approval hub and ID doesn't exist in DB — should 404
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// --- RequestApproval ---

func TestHandleRequestApproval_missingAction(t *testing.T) {
	api := newDBAPI(t)

	body := `{"details":"some details"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleRequestApproval(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "action is required")
}

func TestHandleRequestApproval_invalidJSON(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/approvals", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	api.handleRequestApproval(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleRequestApproval_noApprovalHub(t *testing.T) {
	api := newDBAPI(t)
	// approvals hub is nil by default in newDBAPI

	body := `{"action":"deploy","details":"v1"}`
	req := httptest.NewRequest(http.MethodPost, "/approvals", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleRequestApproval(w, req)

	// Should return 503 when approval hub is not available
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.Contains(t, w.Body.String(), "approval hub not available")
}
