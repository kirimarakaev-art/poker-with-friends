package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/kirimarakaev-art/poker-with-friends/engine"
	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

const (
	codeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O, 1/I
	codeLen      = 6
	maxStack     = 1_000_000
	maxBodySize  = 1 << 10

	defaultHandsLimit = 20
	maxHandsLimit     = 100
	handsTimeout      = 5 * time.Second
	maxOwnRooms       = 3         // active tables per owner
	freshRoom         = time.Hour // an empty table counts as active for this long
)

var (
	errRoomNotFound = errors.New("room not found")
	errTooManyRooms = fmt.Errorf("you already have %d active tables; an empty one stops counting an hour after it was made", maxOwnRooms)
)

// lobby keeps all rooms by their invite code.
type lobby struct {
	opts    Options
	rec     *recorder
	auth    *limiter // failed logins and registrations
	signups *limiter // accounts created
	// creating serializes room creation, so an owner cannot pass the
	// limit with parallel requests.
	creating sync.Mutex
	mu       sync.Mutex
	rooms    map[string]*table
}

func newLobby(opts Options) *lobby {
	if opts.TurnTimeout <= 0 {
		opts.TurnTimeout = defaultTurnTimeout
	}
	if opts.NextHandDelay <= 0 {
		opts.NextHandDelay = defaultNextHandDelay
	}
	if opts.AwayTimeout <= 0 {
		opts.AwayTimeout = defaultAwayTimeout
	}
	if opts.Store == nil {
		opts.Store = store.NewMemory()
	}
	return &lobby{
		opts:    opts,
		rec:     newRecorder(opts.Store),
		auth:    newLimiter(maxAuthFailures, loginWindow),
		signups: newLimiter(maxSignups, signupWindow),
		rooms:   map[string]*table{},
	}
}

func (l *lobby) tableConfig(r store.Room) tableConfig {
	return tableConfig{
		code:          r.Code,
		owner:         r.Owner,
		ownerID:       r.OwnerID,
		public:        r.Public,
		minBuyIn:      r.MinBuyIn,
		maxBuyIn:      r.MaxBuyIn,
		rec:           l.rec,
		game:          engine.Config{SmallBlind: r.SmallBlind, BigBlind: r.BigBlind},
		turnTimeout:   l.opts.TurnTimeout,
		nextHandDelay: l.opts.NextHandDelay,
		awayTimeout:   l.opts.AwayTimeout,
	}
}

// restore loads the saved rooms from the Store.
func (l *lobby) restore(ctx context.Context) error {
	rooms, err := l.opts.Store.LoadRooms(ctx)
	if err != nil {
		return fmt.Errorf("restore rooms: %w", err)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, r := range rooms {
		l.rooms[r.Code] = restoreTable(l.tableConfig(r), r)
	}
	return nil
}

func (l *lobby) room(code string) *table {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rooms[strings.ToUpper(code)]
}

// tables returns every room, sorted by code.
func (l *lobby) tables() []*table {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]*table, 0, len(l.rooms))
	for _, t := range l.rooms {
		out = append(out, t)
	}
	slices.SortFunc(out, func(a, b *table) int { return strings.Compare(a.cfg.code, b.cfg.code) })
	return out
}

type createRequest struct {
	SmallBlind int  `json:"smallBlind"`
	BigBlind   int  `json:"bigBlind"`
	MinBuyIn   int  `json:"minBuyIn"`
	MaxBuyIn   int  `json:"maxBuyIn"`
	Public     bool `json:"public"`
}

func (r createRequest) validate() error {
	if r.SmallBlind <= 0 || r.BigBlind < r.SmallBlind || r.MinBuyIn < r.BigBlind || r.MaxBuyIn < r.MinBuyIn || r.MaxBuyIn > maxStack {
		return errors.New("need 0 < smallBlind <= bigBlind <= minBuyIn <= maxBuyIn <= 1000000")
	}
	return nil
}

// handleCreate: POST /api/rooms {"smallBlind","bigBlind","minBuyIn","maxBuyIn","public"} -> 201 {"code":"K7MQ2X"}.
// The owner does not sit down; the client calls join next.
func (l *lobby) handleCreate(w http.ResponseWriter, r *http.Request) {
	u, ok := l.requireUser(w, r)
	if !ok {
		return
	}
	var req createRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	if err := req.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	room := store.Room{
		OwnerID: u.ID, Owner: u.Username, Public: req.Public,
		SmallBlind: req.SmallBlind, BigBlind: req.BigBlind, MinBuyIn: req.MinBuyIn, MaxBuyIn: req.MaxBuyIn,
	}
	l.creating.Lock()
	defer l.creating.Unlock()
	now := time.Now()
	active := 0
	for _, t := range l.tables() {
		if t.activeOf(u.ID, now) {
			active++
		}
	}
	if active >= maxOwnRooms {
		writeError(w, http.StatusTooManyRequests, errTooManyRooms)
		return
	}
	cfg := l.tableConfig(room)
	cfg.created = now
	l.mu.Lock()
	room.Code = newCode()
	for l.rooms[room.Code] != nil {
		room.Code = newCode()
	}
	cfg.code = room.Code
	l.rooms[room.Code] = newTable(cfg)
	// Recorded under the lock, so it is queued before any player can join.
	l.rec.record(func(ctx context.Context, s Store) error { return s.CreateRoom(ctx, room) })
	l.mu.Unlock()
	writeJSON(w, http.StatusCreated, map[string]string{"code": room.Code})
}

// roomInfo is a room in GET /api/rooms, /api/me/rooms and /api/rooms/{code}.
type roomInfo struct {
	Code       string `json:"code"`
	Owner      string `json:"owner"`
	Public     bool   `json:"public"`
	SmallBlind int    `json:"smallBlind"`
	BigBlind   int    `json:"bigBlind"`
	MinBuyIn   int    `json:"minBuyIn"`
	MaxBuyIn   int    `json:"maxBuyIn"`
	Players    int    `json:"players"`
	MaxPlayers int    `json:"maxPlayers"`
	Seated     bool   `json:"seated"`
}

// handleRooms: GET /api/rooms -> {"rooms":[...]}, public rooms, fullest first.
func (l *lobby) handleRooms(w http.ResponseWriter, r *http.Request) {
	l.listRooms(w, r, func(i roomInfo) bool { return i.Public })
}

// handleMyRooms: GET /api/me/rooms -> {"rooms":[...]}, rooms where the user sits.
func (l *lobby) handleMyRooms(w http.ResponseWriter, r *http.Request) {
	l.listRooms(w, r, func(i roomInfo) bool { return i.Seated })
}

func (l *lobby) listRooms(w http.ResponseWriter, r *http.Request, keep func(roomInfo) bool) {
	u, ok := l.requireUser(w, r)
	if !ok {
		return
	}
	rooms := []roomInfo{}
	for _, t := range l.tables() {
		if info := t.info(u.ID); keep(info) {
			rooms = append(rooms, info)
		}
	}
	slices.SortStableFunc(rooms, func(a, b roomInfo) int { return b.Players - a.Players })
	writeJSON(w, http.StatusOK, map[string]any{"rooms": rooms})
}

// handleRoom: GET /api/rooms/{code} -> the room, also a private one.
func (l *lobby) handleRoom(w http.ResponseWriter, r *http.Request) {
	u, ok := l.requireUser(w, r)
	if !ok {
		return
	}
	t := l.room(r.PathValue("code"))
	if t == nil {
		writeError(w, http.StatusNotFound, errRoomNotFound)
		return
	}
	writeJSON(w, http.StatusOK, t.info(u.ID))
}

// handleJoin: POST /api/rooms/{code}/join {"buyIn":500} -> {"seat":0,"balance":9500}.
// The buy-in moves from the balance to the seat; a player who already sits
// at the table just gets the seat back.
func (l *lobby) handleJoin(w http.ResponseWriter, r *http.Request) {
	u, ok := l.requireUser(w, r)
	if !ok {
		return
	}
	t := l.room(r.PathValue("code"))
	if t == nil {
		writeError(w, http.StatusNotFound, errRoomNotFound)
		return
	}
	var req struct {
		BuyIn int `json:"buyIn"`
	}
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err)
		return
	}
	seat, seated, err := t.reserve(u.ID)
	switch {
	case err != nil:
		writeError(w, http.StatusConflict, err)
		return
	case seated:
		writeJSON(w, http.StatusOK, map[string]int{"seat": seat, "balance": u.Balance})
		return
	case req.BuyIn < t.cfg.minBuyIn || req.BuyIn > t.cfg.maxBuyIn:
		t.cancelReserve(seat)
		writeError(w, http.StatusBadRequest, errors.New("buy-in must be minBuyIn to maxBuyIn"))
		return
	}
	// In the recorder, so it runs after anything already queued for this
	// seat, such as the previous player standing up.
	var balance int
	p := store.Player{Seat: seat, UserID: u.ID, Name: u.Username, Stack: req.BuyIn}
	err = l.rec.wait(func(ctx context.Context, s Store) (err error) {
		balance, err = s.SitDown(ctx, t.cfg.code, p)
		return err
	})
	if err != nil {
		t.cancelReserve(seat)
		if errors.Is(err, store.ErrNotEnoughChips) {
			writeError(w, http.StatusConflict, store.ErrNotEnoughChips)
		} else {
			l.internalError(w, "sit down", err)
		}
		return
	}
	t.sit(seat, u.ID, u.Username, req.BuyIn)
	writeJSON(w, http.StatusOK, map[string]int{"seat": seat, "balance": balance})
}

// handleHands: GET /api/rooms/{code}/hands?limit=20 -> {"hands":[...]}, newest first.
func (l *lobby) handleHands(w http.ResponseWriter, r *http.Request) {
	if _, ok := l.requireUser(w, r); !ok {
		return
	}
	code := strings.ToUpper(r.PathValue("code"))
	if l.room(code) == nil {
		writeError(w, http.StatusNotFound, errRoomNotFound)
		return
	}
	limit := defaultHandsLimit
	if s := r.URL.Query().Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > maxHandsLimit {
			writeError(w, http.StatusBadRequest, errors.New("limit must be 1 to 100"))
			return
		}
		limit = n
	}
	ctx, cancel := context.WithTimeout(r.Context(), handsTimeout)
	defer cancel()
	saved, err := l.opts.Store.Hands(ctx, code, limit)
	if err != nil {
		log.Printf("hands %s: %v", code, err)
		writeError(w, http.StatusInternalServerError, errors.New("could not load hands"))
		return
	}
	hands := make([]handView, 0, len(saved))
	for _, h := range saved {
		hands = append(hands, newHandView(h))
	}
	writeJSON(w, http.StatusOK, map[string]any{"hands": hands})
}

// handlePlay: GET /ws?room=CODE upgrades to the game connection of the
// session's user, who must sit at the table. Connecting again replaces the
// old connection.
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
	u, err := l.currentUser(r)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, errTextLogInFirst)
		return
	}
	c := newClient(conn)
	defer c.close()
	if err := t.attach(u.ID, c); err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	ctx := r.Context()
	go c.writeLoop(ctx)
	c.readLoop(ctx, t, u.ID)
	t.detach(u.ID, c)
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
