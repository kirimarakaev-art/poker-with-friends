package server

import (
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

// limiter counts attempts per client address (wrong passwords, new
// accounts) and refuses more than max of them within window.
type limiter struct {
	max    int
	window time.Duration

	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLimiter(max int, window time.Duration) *limiter {
	return &limiter{max: max, window: window, failures: map[string][]time.Time{}}
}

// blocked reports whether ip used up its attempts. It also forgets
// attempts older than the window.
func (l *limiter) blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := slices.DeleteFunc(l.failures[ip], func(t time.Time) bool { return now.Sub(t) > l.window })
	if len(recent) == 0 {
		delete(l.failures, ip)
	} else {
		l.failures[ip] = recent
	}
	return len(recent) >= l.max
}

func (l *limiter) add(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[ip] = append(l.failures[ip], now)
}

// clientIP is the address the request came from. In production the server
// is reachable only through Caddy, which replaces X-Forwarded-For with the
// real client address, so the header can be trusted there.
func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		first, _, _ := strings.Cut(fwd, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
}
