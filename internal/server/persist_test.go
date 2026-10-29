package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

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

// getHands reads the hand history as a newly registered viewer.
func getHands(t *testing.T, srv *httptest.Server, path string) (int, []handView) {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: register(t, srv, fmt.Sprintf("viewer%d", hostCount.Add(1)))})
	resp, err := http.DefaultClient.Do(req)
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
	st := store.NewMemory()
	srv, l := newStoreServer(t, st)
	code, tokens, folder := playFold(t, srv)
	l.rec.flush()

	rooms, _ := st.LoadRooms(context.Background())
	if len(rooms) != 1 {
		t.Fatalf("saved rooms = %+v", rooms)
	}
	r := rooms[0]
	if r.Code != code || r.SmallBlind != 5 || r.BigBlind != 10 || r.MinBuyIn != 100 || r.MaxBuyIn != 1000 || !r.Public || r.LastHand != 1 {
		t.Errorf("saved room = %+v", r)
	}
	if len(r.Players) != 2 || r.Players[0].Name != "Ann" || r.Players[1].Name != "Bob" {
		t.Fatalf("saved players = %+v", r.Players)
	}
	for i, p := range r.Players {
		if u, err := st.SessionUser(context.Background(), hashToken(tokens[i]), time.Now()); err != nil || u.ID != p.UserID {
			t.Errorf("player %d: user %d, session user %+v %v", i, p.UserID, u, err)
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
	st := store.NewMemory()
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

	viewer := register(t, srv, "Viewer")
	if status, _ := request(t, "GET", srv.URL+"/api/rooms/"+code+"/hands", "", ""); status != http.StatusUnauthorized {
		t.Errorf("hands without login = %d, want 401", status)
	}
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
		if status, _ := request(t, "GET", srv.URL+tt.path, viewer, ""); status != tt.status {
			t.Errorf("GET %s = %d, want %d", tt.path, status, tt.status)
		}
	}

	// Without a Store the history is empty, not null.
	status, hands := getHands(t, srv, "/api/rooms/"+code+"/hands")
	if status != http.StatusOK || hands == nil || len(hands) != 0 {
		t.Errorf("hands without a store = %d %#v", status, hands)
	}
}

func TestHandsLimitIsPassedToStore(t *testing.T) {
	st := store.NewMemory()
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
