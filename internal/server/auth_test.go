package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHandler is a trivial 200-OK handler for testing the middleware.
var stubHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
})

func TestAuthMiddleware_NoKey_AllRequestsPassThrough(t *testing.T) {
	h := authMiddleware("", stubHandler)

	req := httptest.NewRequest("GET", "/api/v1/cards", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Key_MissingToken_Returns401(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	req := httptest.NewRequest("GET", "/api/v1/cards", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)

	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "unauthorized", body["error"])
}

func TestAuthMiddleware_Key_WrongToken_Returns401(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	req := httptest.NewRequest("GET", "/api/v1/cards", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_Key_CorrectBearerToken_Returns200(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	req := httptest.NewRequest("GET", "/api/v1/cards", nil)
	req.Header.Set("Authorization", "Bearer secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Key_CorrectQueryParam_Returns200(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	req := httptest.NewRequest("GET", "/api/v1/cards?token=secret", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Key_HealthExempt(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_Key_StaticFilesExempt(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	for _, path := range []string{"/", "/assets/index.js", "/favicon.ico"} {
		req := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "path %s should be exempt", path)
	}
}

func TestAuthMiddleware_Key_WebSocket_RequiresToken(t *testing.T) {
	h := authMiddleware("secret", stubHandler)

	// Without token — 401
	req := httptest.NewRequest("GET", "/ws", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)

	// With query param — 200
	req = httptest.NewRequest("GET", "/ws?token=secret", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
