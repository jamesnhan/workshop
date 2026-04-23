package v1

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxUploadSize = 1 << 30 // 1 GB

// allowedImageTypes maps MIME types to file extensions for uploaded images.
// Only these types are accepted — everything else is rejected.
var allowedImageTypes = map[string]string{
	"image/png":     ".png",
	"image/jpeg":    ".jpg",
	"image/gif":     ".gif",
	"image/webp":    ".webp",
	"image/svg+xml": ".svg",
}

// getUploadDir returns the upload directory, falling back to /tmp/workshop-uploads.
func (a *API) getUploadDir() string {
	if a.uploadDir != "" {
		return a.uploadDir
	}
	return "/tmp/workshop-uploads"
}

// handleUpload accepts an image file upload, saves it to the uploads directory,
// and returns the file path. The file is never executed — it's only written
// to disk with 0644 permissions for agents to read via the Read tool.
func (a *API) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadSize)

	if err := r.ParseMultipartForm(32 << 20); err != nil {
		a.jsonError(w, fmt.Sprintf("upload failed: %v", err), http.StatusRequestEntityTooLarge)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		a.jsonError(w, "missing file field", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate content type by sniffing the first 512 bytes.
	buf := make([]byte, 512)
	n, _ := file.Read(buf)
	detected := http.DetectContentType(buf[:n])

	_, ok := allowedImageTypes[detected]
	if !ok {
		a.jsonError(w, fmt.Sprintf("file type %q not allowed — only images (png, jpg, gif, webp, svg)", detected), http.StatusUnsupportedMediaType)
		return
	}

	// Seek back to start after sniffing.
	if seeker, ok := file.(io.Seeker); ok {
		seeker.Seek(0, io.SeekStart)
	}

	// Generate a safe filename: UUID prefix + sanitized original name.
	randBytes := make([]byte, 8)
	rand.Read(randBytes)
	prefix := hex.EncodeToString(randBytes)

	origName := filepath.Base(header.Filename)
	origName = strings.ReplaceAll(origName, "..", "")
	origName = strings.ReplaceAll(origName, "/", "")
	origName = strings.ReplaceAll(origName, "\\", "")
	if origName == "" || origName == "." {
		origName = "upload.png"
	}

	dir := a.getUploadDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		a.serverErr(w, "failed to create upload directory", err)
		return
	}

	destName := prefix + "-" + origName
	destPath := filepath.Join(dir, destName)

	absDir, _ := filepath.Abs(dir)
	absPath, err := filepath.Abs(destPath)
	if err != nil || !strings.HasPrefix(absPath, absDir) {
		a.jsonError(w, "invalid filename", http.StatusBadRequest)
		return
	}

	dest, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		a.serverErr(w, "failed to save file", err)
		return
	}
	defer dest.Close()

	if _, err := io.Copy(dest, file); err != nil {
		a.serverErr(w, "failed to write file", err)
		return
	}

	a.logger.Info("file uploaded", "path", absPath, "size", header.Size, "type", detected)

	w.WriteHeader(http.StatusCreated)
	a.jsonOK(w, map[string]string{"path": absPath})
}

// StartUploadReaper runs a background goroutine that deletes uploaded files
// older than maxAge. Runs every hour.
func StartUploadReaper(dir string, maxAge time.Duration, logger *slog.Logger, stop <-chan struct{}) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				reapOldUploads(dir, maxAge, logger)
			}
		}
	}()
}

func reapOldUploads(dir string, maxAge time.Duration, logger *slog.Logger) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return // directory may not exist yet
	}
	cutoff := time.Now().Add(-maxAge)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			if err := os.Remove(path); err == nil {
				logger.Info("reaped old upload", "path", path, "age", time.Since(info.ModTime()).Round(time.Hour))
			}
		}
	}
}
