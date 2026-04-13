package v1

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type linkPreviewResult struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Favicon     string `json:"favicon"`
	FinalURL    string `json:"finalUrl"`
}

var (
	titleRe = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	descRe  = regexp.MustCompile(`(?i)<meta\s[^>]*name=["']description["'][^>]*content=["']([^"']+)["']`)
	descRe2 = regexp.MustCompile(`(?i)<meta\s[^>]*content=["']([^"']+)["'][^>]*name=["']description["']`)
	ogDescRe = regexp.MustCompile(`(?i)<meta\s[^>]*property=["']og:description["'][^>]*content=["']([^"']+)["']`)
	ogTitleRe = regexp.MustCompile(`(?i)<meta\s[^>]*property=["']og:title["'][^>]*content=["']([^"']+)["']`)
)

func isPrivateIP(host string) bool {
	ips, err := net.LookupIP(host)
	if err != nil {
		return false
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
			return true
		}
	}
	return false
}

func (a *API) handleLinkPreview(w http.ResponseWriter, r *http.Request) {
	rawURL := r.URL.Query().Get("url")
	if rawURL == "" {
		a.jsonError(w, "url parameter required", http.StatusBadRequest)
		return
	}

	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		a.jsonError(w, "invalid URL", http.StatusBadRequest)
		return
	}

	// Block private/local IPs
	if isPrivateIP(parsed.Hostname()) {
		a.jsonError(w, "private URLs not allowed", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		a.jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	req.Header.Set("User-Agent", "Workshop/1.0 LinkPreview")
	req.Header.Set("Accept", "text/html")

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		a.jsonError(w, "fetch failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Only parse HTML
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		a.jsonOK(w, linkPreviewResult{FinalURL: resp.Request.URL.String()})
		return
	}

	// Read up to 64KB — enough for <head>
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	html := string(body)

	result := linkPreviewResult{
		FinalURL: resp.Request.URL.String(),
	}

	// Title
	if m := ogTitleRe.FindStringSubmatch(html); len(m) > 1 {
		result.Title = strings.TrimSpace(m[1])
	} else if m := titleRe.FindStringSubmatch(html); len(m) > 1 {
		result.Title = strings.TrimSpace(m[1])
	}

	// Description
	if m := ogDescRe.FindStringSubmatch(html); len(m) > 1 {
		result.Description = strings.TrimSpace(m[1])
	} else if m := descRe.FindStringSubmatch(html); len(m) > 1 {
		result.Description = strings.TrimSpace(m[1])
	} else if m := descRe2.FindStringSubmatch(html); len(m) > 1 {
		result.Description = strings.TrimSpace(m[1])
	}

	// Favicon
	result.Favicon = parsed.Scheme + "://" + parsed.Host + "/favicon.ico"

	a.jsonOK(w, result)
}
