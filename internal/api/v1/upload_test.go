package v1

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newUploadAPI(t *testing.T) *API {
	t.Helper()
	api := newDBAPI(t)
	dir := t.TempDir()
	api.SetUploadDir(dir)
	return api
}

// makePNG creates a minimal valid PNG in memory.
func makePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

func uploadRequest(t *testing.T, fieldName, filename string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, filename)
	require.NoError(t, err)
	_, err = part.Write(content)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestHandleUpload_ValidPNG(t *testing.T) {
	api := newUploadAPI(t)
	pngData := makePNG(t)

	req := uploadRequest(t, "file", "screenshot.png", pngData)
	w := httptest.NewRecorder()
	api.handleUpload(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["path"], "screenshot.png")

	// Verify file actually exists on disk.
	_, err := os.Stat(resp["path"])
	assert.NoError(t, err)
}

func TestHandleUpload_RejectsNonImage(t *testing.T) {
	api := newUploadAPI(t)

	// Plain text content — not an image MIME type.
	req := uploadRequest(t, "file", "evil.txt", []byte("this is not an image"))
	w := httptest.NewRecorder()
	api.handleUpload(w, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
	assert.Contains(t, w.Body.String(), "not allowed")
}

func TestHandleUpload_RejectsHTMLContent(t *testing.T) {
	api := newUploadAPI(t)

	// HTML sniffed as text/html — should be rejected.
	html := []byte("<html><body>hello</body></html>")
	req := uploadRequest(t, "file", "page.html", html)
	w := httptest.NewRecorder()
	api.handleUpload(w, req)

	assert.Equal(t, http.StatusUnsupportedMediaType, w.Code)
}

func TestHandleUpload_MissingFileField(t *testing.T) {
	api := newUploadAPI(t)

	// Use a wrong field name so "file" is missing.
	req := uploadRequest(t, "wrong_field", "test.png", makePNG(t))
	w := httptest.NewRecorder()
	api.handleUpload(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "missing file field")
}

func TestHandleUpload_FilenameSanitization(t *testing.T) {
	api := newUploadAPI(t)
	pngData := makePNG(t)

	tests := []struct {
		name     string
		filename string
	}{
		{"path traversal", "../../etc/passwd.png"},
		{"backslash", "..\\..\\evil.png"},
		{"double dots", "....test.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := uploadRequest(t, "file", tt.filename, pngData)
			w := httptest.NewRecorder()
			api.handleUpload(w, req)

			assert.Equal(t, http.StatusCreated, w.Code)

			var resp map[string]string
			require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

			// The saved file must be inside the upload dir, not escaped.
			dir := api.getUploadDir()
			absDir, _ := filepath.Abs(dir)
			absPath, _ := filepath.Abs(resp["path"])
			assert.True(t, filepath.HasPrefix(absPath, absDir),
				"file %q should be inside %q", absPath, absDir)
		})
	}
}

func TestHandleUpload_DotFilename(t *testing.T) {
	api := newUploadAPI(t)
	pngData := makePNG(t)

	// A filename that sanitizes to "." should fallback to "upload.png".
	req := uploadRequest(t, "file", "...", pngData)
	w := httptest.NewRecorder()
	api.handleUpload(w, req)

	assert.Equal(t, http.StatusCreated, w.Code)

	var resp map[string]string
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Contains(t, resp["path"], "upload.png")
}
