package server

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// newWSProxy creates a WebSocket reverse proxy that hijacks the connection
// and tunnels raw TCP between the client and the desktop Workshop instance.
// Standard httputil.ReverseProxy doesn't handle WebSocket upgrades properly
// because it tries to buffer the response body.
func newWSProxy(targetURL string) http.Handler {
	target, _ := url.Parse(targetURL)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Connect to the desktop Workshop
		host := target.Host
		if !strings.Contains(host, ":") {
			if target.Scheme == "https" {
				host += ":443"
			} else {
				host += ":80"
			}
		}

		backendConn, err := net.DialTimeout("tcp", host, 10*time.Second)
		if err != nil {
			http.Error(w, "desktop unreachable", http.StatusBadGateway)
			return
		}
		defer backendConn.Close()

		// Rewrite the request to the backend
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host

		// Forward the original request (including Upgrade headers) to the backend
		if err := r.Write(backendConn); err != nil {
			http.Error(w, "failed to forward request", http.StatusBadGateway)
			return
		}

		// Hijack the client connection
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacking not supported", http.StatusInternalServerError)
			return
		}
		clientConn, _, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, "hijack failed", http.StatusInternalServerError)
			return
		}
		defer clientConn.Close()

		// Bidirectional tunnel: client <-> backend
		done := make(chan struct{}, 2)
		go func() {
			io.Copy(clientConn, backendConn)
			done <- struct{}{}
		}()
		go func() {
			io.Copy(backendConn, clientConn)
			done <- struct{}{}
		}()
		<-done
	})
}
