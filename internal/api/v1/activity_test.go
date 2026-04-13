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

func TestRecordActivity_happyPath(t *testing.T) {
	api := newDBAPI(t)
	body := `{"actionType":"file_write","summary":"Created main.go","project":"workshop","paneTarget":"workshop:1.1"}`
	req := httptest.NewRequest(http.MethodPost, "/activity", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleRecordActivity(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)
	var entry db.ActivityEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&entry))
	assert.Equal(t, "file_write", entry.ActionType)
	assert.Equal(t, "Created main.go", entry.Summary)
	assert.Equal(t, "workshop", entry.Project)
}

func TestRecordActivity_missingFields(t *testing.T) {
	api := newDBAPI(t)
	body := `{"actionType":"","summary":""}`
	req := httptest.NewRequest(http.MethodPost, "/activity", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleRecordActivity(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListActivity_filters(t *testing.T) {
	api := newDBAPI(t)

	// Record two entries in different projects
	for _, p := range []string{"alpha", "beta"} {
		body := `{"actionType":"command","summary":"did stuff","project":"` + p + `"}`
		req := httptest.NewRequest(http.MethodPost, "/activity", strings.NewReader(body))
		w := httptest.NewRecorder()
		api.handleRecordActivity(w, req)
		require.Equal(t, http.StatusCreated, w.Code)
	}

	// List all
	req := httptest.NewRequest(http.MethodGet, "/activity", nil)
	w := httptest.NewRecorder()
	api.handleListActivity(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var all []db.ActivityEntry
	require.NoError(t, json.NewDecoder(w.Body).Decode(&all))
	assert.Len(t, all, 2)

	// Filter by project
	req2 := httptest.NewRequest(http.MethodGet, "/activity?project=alpha", nil)
	w2 := httptest.NewRecorder()
	api.handleListActivity(w2, req2)
	var filtered []db.ActivityEntry
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&filtered))
	assert.Len(t, filtered, 1)
	assert.Equal(t, "alpha", filtered[0].Project)
}
