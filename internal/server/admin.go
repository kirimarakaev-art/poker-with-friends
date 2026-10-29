package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"mime"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	adminCookie      = "admin_session"
	adminSessionTTL  = 12 * time.Hour
	maxLoginFailures = 5
	loginWindow      = time.Minute
)

// admin guards the admin API with the password from Options.AdminPassword.
// A login gets a random session token in an HttpOnly, SameSite=Strict
// cookie; only its hash is kept, in memory, so a restart logs everyone out.
type admin struct {
	password [32]byte // SHA-256, compared in constant time

	mu       sync.Mutex
	sessions map[[32]byte]time.Time // token hash -> expiry
	failures map[string][]time.Time // client IP -> recent failed logins
}

func newAdmin(password string) *admin {
	return &admin{
		password: sha256.Sum256([]byte(password)),
		sessions: map[[32]byte]time.Time{},
		failures: map[string][]time.Time{},
	}
}

// routes adds the admin API to mux. Every route but login needs a session.
func (a *admin) routes(mux *http.ServeMux, l *lobby) {
	mux.HandleFunc("POST /api/admin/login", a.handleLogin)
	mux.HandleFunc("POST /api/admin/logout", a.handleLogout)
	mux.Handle("GET /api/admin/rooms", a.guard(l.handleAdminRooms))
	mux.Handle("POST /api/admin/rooms/{code}/kick", a.guard(l.handleAdminKick))
	mux.Handle("POST /api/admin/rooms/{code}/stack", a.guard(l.handleAdminStack))
	mux.Handle("DELETE /api/admin/rooms/{code}", a.guard(l.handleAdminClose))
}

// handleLogin: POST /api/admin/login {"password":"..."} sets the session cookie.
func (a *admin) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	now := time.Now()
	if a.tooManyFailures(ip, now) {
		writeError(w, http.StatusTooManyRequests, errors.New("too many attempts, try again in a minute"))
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	given := sha256.Sum256([]byte(req.Password))
	if subtle.ConstantTimeCompare(given[:], a.password[:]) != 1 {
		a.fail(ip, now)
		writeError(w, http.StatusUnauthorized, errors.New("wrong password"))
		return
	}
	token := rand.Text()
	a.mu.Lock()
	a.sessions[hashToken(token)] = now.Add(adminSessionTTL)
	a.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     adminCookie,
		Value:    token,
		Path:     "/api/admin",
		MaxAge:   int(adminSessionTTL.Seconds()),
		HttpOnly: true,
		Secure:   isHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *admin) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(adminCookie); err == nil {
		a.mu.Lock()
		delete(a.sessions, hashToken(c.Value))
		a.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: adminCookie, Path: "/api/admin", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// guard lets through requests with a live session. Changes must be JSON:
// a cross-site form cannot send that without a CORS preflight, which this
// server never allows.
func (a *admin) guard(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !a.loggedIn(r) {
			writeError(w, http.StatusUnauthorized, errors.New("log in first"))
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodDelete {
			if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
				writeError(w, http.StatusUnsupportedMediaType, errors.New("send application/json"))
				return
			}
		}
		next(w, r)
	})
}

func (a *admin) loggedIn(r *http.Request) bool {
	c, err := r.Cookie(adminCookie)
	if err != nil {
		return false
	}
	h := hashToken(c.Value)
	a.mu.Lock()
	defer a.mu.Unlock()
	expiry, ok := a.sessions[h]
	if ok && time.Now().After(expiry) {
		delete(a.sessions, h)
		return false
	}
	return ok
}

// tooManyFailures also forgets failures older than loginWindow.
func (a *admin) tooManyFailures(ip string, now time.Time) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	recent := slices.DeleteFunc(a.failures[ip], func(t time.Time) bool { return now.Sub(t) > loginWindow })
	if len(recent) == 0 {
		delete(a.failures, ip)
	} else {
		a.failures[ip] = recent
	}
	return len(recent) >= maxLoginFailures
}

func (a *admin) fail(ip string, now time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.failures[ip] = append(a.failures[ip], now)
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

// adminRoomView is one room in GET /api/admin/rooms.
type adminRoomView struct {
	Code       string             `json:"code"`
	SmallBlind int                `json:"smallBlind"`
	BigBlind   int                `json:"bigBlind"`
	Hand       int                `json:"hand"`
	Street     string             `json:"street"`
	Players    []*adminPlayerView `json:"players"` // by seat, null is a free seat
}

type adminPlayerView struct {
	Seat         int    `json:"seat"`
	Name         string `json:"name"`
	Stack        int    `json:"stack"`
	Bet          int    `json:"bet"`
	Connected    bool   `json:"connected"`
	InHand       bool   `json:"inHand"`
	Leaving      bool   `json:"leaving"`
	PendingStack *int   `json:"pendingStack,omitempty"` // set by the admin, applied when the hand ends
}

// handleAdminRooms: GET /api/admin/rooms -> {"rooms":[...]}, sorted by code.
func (l *lobby) handleAdminRooms(w http.ResponseWriter, _ *http.Request) {
	l.mu.Lock()
	codes := make([]string, 0, len(l.rooms))
	tables := make(map[string]*table, len(l.rooms))
	for code, t := range l.rooms {
		codes = append(codes, code)
		tables[code] = t
	}
	l.mu.Unlock()
	slices.Sort(codes)
	rooms := make([]adminRoomView, len(codes))
	for i, code := range codes {
		rooms[i] = tables[code].adminView()
	}
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

type seatRequest struct {
	Seat  int `json:"seat"`
	Stack int `json:"stack"`
}

// handleAdminKick: POST /api/admin/rooms/{code}/kick {"seat":0} frees the seat
// like a "leave" message from that player.
func (l *lobby) handleAdminKick(w http.ResponseWriter, r *http.Request) {
	t, req, ok := l.adminSeatRequest(w, r)
	if !ok {
		return
	}
	if err := t.kick(req.Seat); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// handleAdminStack: POST /api/admin/rooms/{code}/stack {"seat":0,"stack":1000}.
// A player in the current hand gets the new stack when the hand ends.
func (l *lobby) handleAdminStack(w http.ResponseWriter, r *http.Request) {
	t, req, ok := l.adminSeatRequest(w, r)
	if !ok {
		return
	}
	if req.Stack < 0 || req.Stack > maxStack {
		writeError(w, http.StatusBadRequest, errors.New("stack must be 0 to 1000000"))
		return
	}
	if err := t.setStack(req.Seat, req.Stack); err != nil {
		writeError(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (l *lobby) adminSeatRequest(w http.ResponseWriter, r *http.Request) (*table, seatRequest, bool) {
	var req seatRequest
	t := l.room(r.PathValue("code"))
	if t == nil {
		writeError(w, http.StatusNotFound, errors.New("room not found"))
		return nil, req, false
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return nil, req, false
	}
	return t, req, true
}

// handleAdminClose: DELETE /api/admin/rooms/{code} disconnects everyone with
// CloseRoomNotFound and deletes the room with its history.
func (l *lobby) handleAdminClose(w http.ResponseWriter, r *http.Request) {
	code := strings.ToUpper(r.PathValue("code"))
	l.mu.Lock()
	t := l.rooms[code]
	delete(l.rooms, code)
	if t != nil {
		// Recorded under the lock, like CreateRoom, so it keeps its order.
		l.rec.record(func(ctx context.Context, s Store) error { return s.DeleteRoom(ctx, code) })
	}
	l.mu.Unlock()
	if t == nil {
		writeError(w, http.StatusNotFound, errors.New("room not found"))
		return
	}
	t.close()
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
