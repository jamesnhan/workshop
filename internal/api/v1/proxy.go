package v1

import (
	"net/http"
	"net/http/httputil"
	"net/url"
)

// tmuxProxy wraps a reverse proxy to a desktop Workshop instance that owns tmux.
// Used in headless mode to forward tmux-dependent API requests.
type tmuxProxy struct {
	proxy  *httputil.ReverseProxy
	target *url.URL
}

func newTmuxProxy(targetURL string) (*tmuxProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	return &tmuxProxy{proxy: proxy, target: target}, nil
}

// ServeHTTP forwards the request to the desktop Workshop. The incoming path has
// already been stripped of /api/v1 by the server's StripPrefix, so we prepend
// it back before forwarding to the desktop's /api/v1/* routes.
func (p *tmuxProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r2 := r.Clone(r.Context())
	r2.URL.Path = "/api/v1" + r.URL.Path
	r2.URL.RawPath = ""
	p.proxy.ServeHTTP(w, r2)
}

// SetTmuxProxy configures a reverse proxy target for tmux-dependent routes.
// Only meaningful in headless mode.
func (a *API) SetTmuxProxy(targetURL string) error {
	p, err := newTmuxProxy(targetURL)
	if err != nil {
		return err
	}
	a.tmuxProxy = p
	return nil
}

// TmuxProxyURL returns the configured proxy target URL, or empty string if
// no proxy is configured.
func (a *API) TmuxProxyURL() string {
	if a.tmuxProxy == nil {
		return ""
	}
	return a.tmuxProxy.target.String()
}
