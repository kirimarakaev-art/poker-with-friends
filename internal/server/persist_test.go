package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

// memStore is a Store in memory, like store.Postgres but without SQL.
type memStore struct {
	mu    sync.Mutex
	rooms []store.Room
	hands map[string][]store.Hand
}

func newMemStore() *memStore {
	return &memStore{hands: map[string][]store.Hand{}}
}

func (m *memStore) find(code string) *store.Room {
	for i := range m.rooms {
		if m.rooms[i].Code == code {
			return &m.rooms[i]
		}
	}
	return nil
}

func (m *memStore) CreateRoom(_ context.Context, r store.Room) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rooms = append(m.rooms, r)
	return nil
}

func (m *memStore) AddPlayer(_ context.Context, code string, p store.Player) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(code)
	if r == nil {
		return errors.New("no room " + code)
	}
	r.Players = append(r.Players, p)
	return nil
}

func (m *memStore) RemovePlayer(_ context.Context, code string, seat int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(code)
	if r == nil {
		return errors.New("no room " + code)
	}
	r.Players = slices.DeleteFunc(r.Players, func(p store.Player) bool { return p.Seat == seat })
	return nil
}

func (m *memStore) SaveHand(_ context.Context, code string, h store.Hand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.find(code)
	if r == nil {
		return errors.New("no room " + code)
	}
	for _, s := range h.Seats {
		if i := slices.IndexFunc(r.Players, func(p store.Player) bool { return p.Seat == s.Seat }); i >= 0 {
			r.Players[i].Stack = s.Stack
		}
	}
	r.LastHand = h.Number
	m.hands[code] = append(m.hands[code], h)
	return nil
}

func (m *memStore) LoadRooms(context.Context) ([]store.Room, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]store.Room, len(m.rooms))
	for i, r := range m.rooms {
		r.Players = slices.Clone(r.Players)
		out[i] = r
	}
	return out, nil
}

func (m *memStore) Hands(_ context.Context, code string, limit int) ([]store.Hand, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	h := slices.Clone(m.hands[code])
	slices.Reverse(h)
	return h[:min(limit, len(h))], nil
}

func newStoreServer(t *testing.T, st Store) (*httptest.Server, *lobby) {
	t.Helper()
	l := newLobby(Options{Store: st, NextHandDelay: time.Minute})
	if err := l.restore(context.Background()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(newHandler(l))
	t.Cleanup(srv.Close)
	return srv, l
}

func getHands(t *testing.T, srv *httptest.Server, path string) (int, []handView) {
	t.Helper()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Hands []handView `json:"hands"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, out.Hands
}

// playFold seats Ann and Bob, connects them and lets the first to act fold.
func playFold(t *testing.T, srv *httptest.Server) (code string, tokens [2]string, folder int) {
	t.Helper()
	code = createRoom(t, srv)
	tokens[0] = joinRoom(t, srv, code, "Ann")
	tokens[1] = joinRoom(t, srv, code, "Bob")
	conns := [2]*testConn{connect(t, srv, code, tokens[0]), connect(t, srv, code, tokens[1])}
	s := conns[0].waitState("hand 1", func(s *stateView) bool { return s.Hand == 1 && s.ToAct >= 0 })
	folder = s.ToAct
	conns[folder].send(`{"type":"act","action":"fold"}`)
	conns[0].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	for _, c := range conns {
		c.conn.CloseNow()
	}
	return code, tokens, folder
}

func TestRoomsPlayersAndHandsAreSaved(t *testing.T) {
	st := newMemStore()
	srv, l := newStoreServer(t, st)
	code, tokens, folder := playFold(t, srv)
	l.rec.flush()

	rooms, _ := st.LoadRooms(context.Background())
	if len(rooms) != 1 {
		t.Fatalf("saved rooms = %+v", rooms)
	}
	r := rooms[0]
	if r.Code != code || r.SmallBlind != 5 || r.BigBlind != 10 || r.Stack != 1000 || r.LastHand != 1 {
		t.Errorf("saved room = %+v", r)
	}
	if len(r.Players) != 2 || r.Players[0].Name != "Ann" || r.Players[1].Name != "Bob" {
		t.Fatalf("saved players = %+v", r.Players)
	}
	for i, p := range r.Players {
		if p.TokenHash != hashToken(tokens[i]) {
			t.Errorf("player %d: token hash does not match", i)
		}
		if want := map[bool]int{true: 995, false: 1005}[i == folder]; p.Stack != want {
			t.Errorf("player %d: stack %d, want %d", i, p.Stack, want)
		}
	}

	status, hands := getHands(t, srv, "/api/rooms/"+code+"/hands")
	if status != http.StatusOK || len(hands) != 1 {
		t.Fatalf("hands = %d %+v", status, hands)
	}
	h := hands[0]
	if h.Hand != 1 || h.Pot != 15 || len(h.Board) != 0 || len(h.Players) != 2 || h.FinishedAt.IsZero() {
		t.Errorf("hand = %+v", h)
	}
	for _, p := range h.Players {
		if len(p.Cards) != 0 {
			t.Errorf("%s's cards are in the history although nobody showed: %v", p.Name, p.Cards)
		}
	}
	if w := h.Players[1-folder]; w.Won != 15 || w.Stack != 1005 {
		t.Errorf("winner = %+v", w)
	}
}

func TestRoomsSurviveRestart(t *testing.T) {
	st := newMemStore()
	srv, l := newStoreServer(t, st)
	code, tokens, _ := playFold(t, srv)
	l.rec.flush()
	srv.Close()

	srv2, _ := newStoreServer(t, st)
	c := connect(t, srv2, code, tokens[1])
	connect(t, srv2, code, tokens[0])
	s := c.waitState("hand 2 after restart", func(s *stateView) bool { return s.Hand == 2 && s.Street == "Preflop" })
	if s.You != 1 || s.Players[0].Name != "Ann" || s.Players[1].Name != "Bob" {
		t.Errorf("restored players = %+v (you %d)", s.Players, s.You)
	}
	if stacks := s.Players[0].Stack + s.Players[1].Stack + s.Pot; stacks != 2000 {
		t.Errorf("chips after restart = %d, want 2000", stacks)
	}
}

func TestHandsEndpoint(t *testing.T) {
	srv := newTestServer(t, Options{})
	code := createRoom(t, srv)

	tests := []struct {
		path   string
		status int
	}{
		{"/api/rooms/" + code + "/hands", http.StatusOK},
		{"/api/rooms/" + code + "/hands?limit=100", http.StatusOK},
		{"/api/rooms/NOPE00/hands", http.StatusNotFound},
		{"/api/rooms/" + code + "/hands?limit=0", http.StatusBadRequest},
		{"/api/rooms/" + code + "/hands?limit=101", http.StatusBadRequest},
		{"/api/rooms/" + code + "/hands?limit=x", http.StatusBadRequest},
	}
	for _, tt := range tests {
		resp, err := http.Get(srv.URL + tt.path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tt.status {
			t.Errorf("GET %s = %d, want %d", tt.path, resp.StatusCode, tt.status)
		}
	}

	// Without a Store the history is empty, not null.
	status, hands := getHands(t, srv, "/api/rooms/"+code+"/hands")
	if status != http.StatusOK || hands == nil || len(hands) != 0 {
		t.Errorf("hands without a store = %d %#v", status, hands)
	}
}

func TestHandsLimitIsPassedToStore(t *testing.T) {
	st := newMemStore()
	srv, l := newStoreServer(t, st)
	code := createRoom(t, srv)
	l.rec.flush()
	for n := 1; n <= 3; n++ {
		st.SaveHand(context.Background(), code, store.Hand{Number: n})
	}
	_, hands := getHands(t, srv, "/api/rooms/"+code+"/hands?limit=2")
	var got []int
	for _, h := range hands {
		got = append(got, h.Hand)
	}
	if want := []int{3, 2}; !reflect.DeepEqual(got, want) {
		t.Errorf("hands = %v, want %v", got, want)
	}
}
