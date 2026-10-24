package server

import (
	"errors"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// Options tune timings; zero values mean the defaults.
type Options struct {
	TurnTimeout   time.Duration
	NextHandDelay time.Duration
}

const (
	defaultTurnTimeout   = 30 * time.Second
	defaultNextHandDelay = 3 * time.Second
)

// New returns the HTTP handler of the poker server.
func New(opts Options) http.Handler {
	if opts.TurnTimeout <= 0 {
		opts.TurnTimeout = defaultTurnTimeout
	}
	if opts.NextHandDelay <= 0 {
		opts.NextHandDelay = defaultNextHandDelay
	}
	l := newLobby(opts)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", handleIndex)
	mux.HandleFunc("GET /echo", handleEcho)
	mux.HandleFunc("POST /api/rooms", l.handleCreate)
	mux.HandleFunc("POST /api/rooms/{code}/join", l.handleJoin)
	mux.HandleFunc("GET /ws", l.handlePlay)
	return mux
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
	case websocket.StatusNormalClosure, websocket.StatusGoingAway, websocket.StatusNoStatusRcvd, CloseReplaced:
		return true
	}
	return errors.Is(err, io.EOF)
}
