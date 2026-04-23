package v1

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHandleDebugLog_ValidJSON(t *testing.T) {
	api := newDBAPI(t)

	body := `{"msg":"test error","component":"sidebar","stack":"Error at line 42"}`
	req := httptest.NewRequest(http.MethodPost, "/debug/log", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleDebugLog(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleDebugLog_NoMsgField(t *testing.T) {
	api := newDBAPI(t)

	// No "msg" key — should default to "frontend-debug" internally but still 204.
	body := `{"level":"error","detail":"something broke"}`
	req := httptest.NewRequest(http.MethodPost, "/debug/log", strings.NewReader(body))
	w := httptest.NewRecorder()
	api.handleDebugLog(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleDebugLog_EmptyObject(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/debug/log", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	api.handleDebugLog(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
}

func TestHandleDebugLog_InvalidJSON(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/debug/log", strings.NewReader(`not json`))
	w := httptest.NewRecorder()
	api.handleDebugLog(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleDebugLog_EmptyBody(t *testing.T) {
	api := newDBAPI(t)

	req := httptest.NewRequest(http.MethodPost, "/debug/log", strings.NewReader(``))
	w := httptest.NewRecorder()
	api.handleDebugLog(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
