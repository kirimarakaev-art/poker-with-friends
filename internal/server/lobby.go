package server

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/coder/websocket"
	"github.com/kirimarakaev-art/poker-with-friends/engine"
)

const (
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I
	codeLen      = 6
	maxNameLen   = 20
	maxStack     = 1_000_000
	maxBodySize  = 1 << 10
)

// lobby keeps all rooms by their invite code.
type lobby struct {
	opts  Options
	mu    sync.Mutex
	rooms map[string]*table
}

func newLobby(opts Options) *lobby {
	if opts.TurnTimeout <= 0 {
		opts.TurnTimeout = defaultTurnTimeout
	}
	if opts.NextHandDelay <= 0 {
		opts.NextHandDelay = defaultNextHandDelay
	}
	return &lobby{opts: opts, rooms: map[string]*table{}}
}

func (l *lobby) room(code string) *table {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rooms[strings.ToUpper(code)]
}

type createRequest struct {
	SmallBlind int `json:"smallBlind"`
	BigBlind   int `json:"bigBlind"`
	Stack      int `json:"stack"`
}

func (r createRequest) validate() error {
	switch {
	case r.SmallBlind <= 0 || r.BigBlind < r.SmallBlind:
		return errors.New("need 0 < smallBlind <= bigBlind")
	case r.Stack < r.BigBlind || r.Stack > maxStack:
		return errors.New("need bigBlind <= stack <= 1000000")
	}
	return nil
}

// handleCreate: POST /api/rooms {"smallBlind":5,"bigBlind":10,"stack":1000} -> {"code":"K7MQ2X"}
func (l *lobby) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req createRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	t := newTable(tableConfig{
		game:          engine.Config{SmallBlind: req.SmallBlind, BigBlind: req.BigBlind},
		stack:         req.Stack,
		turnTimeout:   l.opts.TurnTimeout,
		nextHandDelay: l.opts.NextHandDelay,
	})
	l.mu.Lock()
	code := newCode()
	for l.rooms[code] != nil {
		code = newCode()
	}
	l.rooms[code] = t
	l.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{"code": code})
}

// handleJoin: POST /api/rooms/{code}/join {"name":"Kerim"} -> {"seat":0,"token":"..."}
func (l *lobby) handleJoin(w http.ResponseWriter, r *http.Request) {
	t := l.room(r.PathValue("code"))
	if t == nil {
		writeError(w, http.StatusNotFound, errors.New("room not found"))
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		writeError(w, http.StatusBadRequest, errors.New("name must be 1 to 20 characters"))
		return
	}
	seat, token, err := t.join(name)
	if err != nil {
		writeError(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"seat": seat, "token": token})
}

// handlePlay: GET /ws?room=CODE&token=TOKEN upgrades to the game connection.
// Connecting again with the same token replaces the old connection.
// An unknown room is reported with a close code, not an HTTP 404, because
// browsers cannot see the status of a failed WebSocket handshake.
func (l *lobby) handlePlay(w http.ResponseWriter, r *http.Request) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		log.Printf("play: accept: %v", err)
		return
	}
	t := l.room(r.URL.Query().Get("room"))
	if t == nil {
		conn.Close(CloseRoomNotFound, "room not found")
		return
	}
	token := r.URL.Query().Get("token")
	c := newClient(conn)
	defer c.close()
	if err := t.attach(token, c); err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	ctx := r.Context()
	go c.writeLoop(ctx)
	c.readLoop(ctx, t, token)
	t.detach(token, c)
}

func newCode() string {
	b := make([]byte, codeLen)
	rand.Read(b) // never returns an error since Go 1.24
	for i := range b {
		b[i] = codeAlphabet[int(b[i])%len(codeAlphabet)]
	}
	return string(b)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errors.New("bad JSON body")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
