package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
)

// Options tune the server; zero values mean the defaults.
type Options struct {
	TurnTimeout   time.Duration
	NextHandDelay time.Duration
	// AwayTimeout is how long a disconnected player keeps the seat.
	AwayTimeout time.Duration
	// StaticDir is the built frontend (frontend/dist) to serve at /.
	// When empty, / answers with a plain text banner.
	StaticDir string
	// AdminUsername is the account that gets the admin role (the admin API
	// under /api/admin and the /admin page). When empty, nobody new does.
	AdminUsername string
	// Store keeps accounts, rooms and hand history. When nil, a
	// store.Memory is used and everything is lost on restart.
	Store Store
}

const (
	defaultTurnTimeout   = 30 * time.Second
	defaultNextHandDelay = 3 * time.Second
	defaultAwayTimeout   = 5 * time.Minute
)

// New returns the HTTP handler of the poker server.
func New(opts Options) http.Handler {
	return newHandler(newLobby(opts))
}

// Open is New that first restores the rooms saved in opts.Store and gives
// opts.AdminUsername the admin role.
func Open(ctx context.Context, opts Options) (http.Handler, error) {
	l := newLobby(opts)
	if err := l.restore(ctx); err != nil {
		return nil, err
	}
	if err := l.grantAdmin(ctx); err != nil {
		return nil, fmt.Errorf("grant admin: %w", err)
	}
	return newHandler(l), nil
}

// newHandler wires the routes; tests use it to reach the lobby directly.
func newHandler(l *lobby) http.Handler {
	mux := http.NewServeMux()
	if l.opts.StaticDir != "" {
		mux.Handle("GET /", staticHandler(l.opts.StaticDir))
	} else {
		mux.HandleFunc("GET /{$}", handleIndex)
	}
	mux.HandleFunc("GET /echo", handleEcho)
	mux.HandleFunc("POST /api/auth/register", l.handleRegister)
	mux.HandleFunc("POST /api/auth/login", l.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", l.handleLogout)
	mux.HandleFunc("GET /api/me", l.handleMe)
	mux.HandleFunc("POST /api/me/bonus", l.handleBonus)
	mux.HandleFunc("GET /api/me/rooms", l.handleMyRooms)
	mux.HandleFunc("POST /api/rooms", l.handleCreate)
	mux.HandleFunc("GET /api/rooms", l.handleRooms)
	mux.HandleFunc("GET /api/rooms/{code}", l.handleRoom)
	mux.HandleFunc("POST /api/rooms/{code}/join", l.handleJoin)
	mux.HandleFunc("GET /api/rooms/{code}/hands", l.handleHands)
	mux.HandleFunc("GET /ws", l.handlePlay)
	l.adminRoutes(mux)
	if l.opts.StaticDir != "" {
		mux.Handle("GET /admin", adminPage(l.opts.StaticDir))
	}
	return requireJSON(mux)
}

// requireJSON refuses API changes that are not JSON. A cross-site form can
// post text/plain without a CORS preflight, which this server never allows,
// so this together with SameSite cookies keeps other sites from acting for
// a logged-in user.
func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodPost || r.Method == http.MethodDelete) && strings.HasPrefix(r.URL.Path, "/api/") {
			mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
			if mt != "application/json" && !(r.Method == http.MethodDelete && r.ContentLength <= 0) {
				writeError(w, http.StatusUnsupportedMediaType, errors.New("send application/json"))
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func handleIndex(w http.ResponseWriter, _ *http.Request) {
	io.WriteString(w, "poker server\n")
}

// handleEcho sends every message back to the client that sent it.
func handleEcho(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("echo: accept: %v", err)
		return
	}
	defer c.CloseNow()
	ctx := r.Context()
	for {
		typ, data, err := c.Read(ctx)
		if err != nil {
			logUnexpectedClose("echo: read", err)
			return
		}
		if err := c.Write(ctx, typ, data); err != nil {
			logUnexpectedClose("echo: write", err)
			return
		}
	}
}

func logUnexpectedClose(prefix string, err error) {
	if !expectedClose(err) {
		log.Printf("%s: %v", prefix, err)
	}
}

// expectedClose reports whether err is an ordinary end of a connection.
// Browsers send no status code when a page calls ws.close() without one.
func expectedClose(err error) bool {
	switch websocket.CloseStatus(err) {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd, CloseReplaced, CloseLeft, CloseRoomNotFound:
		return true
	}
	return errors.Is(err, io.EOF)
}
