package server

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

func rebuy(t *testing.T, srvURL, code, session string, amount int) (int, map[string]any) {
	t.Helper()
	return request(t, "POST", srvURL+"/api/rooms/"+code+"/rebuy", session, fmt.Sprintf(`{"amount":%d}`, amount))
}

func TestRebuyBetweenHands(t *testing.T) {
	srv, code, conns, tokens, states := startHeadsUp(t, Options{NextHandDelay: time.Minute})
	folder := states[0].ToAct
	conns[folder].send(`{"type":"act","action":"fold"}`)
	s := conns[folder].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	stack := s.Players[folder].Stack
	lost := 1000 - stack

	if status, _ := rebuy(t, srv.URL, code, tokens[folder], lost+1); status != http.StatusBadRequest {
		t.Errorf("rebuy over maxBuyIn: %d, want 400", status)
	}
	if status, _ := rebuy(t, srv.URL, code, tokens[folder], 0); status != http.StatusBadRequest {
		t.Errorf("rebuy of 0: %d, want 400", status)
	}
	status, out := rebuy(t, srv.URL, code, tokens[folder], lost)
	if status != http.StatusOK || out["balance"].(float64) != float64(9000-lost) {
		t.Fatalf("rebuy: %d %v", status, out)
	}
	conns[folder].waitState("stack topped up", func(s *stateView) bool { return s.Players[folder].Stack == 1000 })
	if b := balanceOf(t, srv.URL, tokens[folder]); b != 9000-lost {
		t.Errorf("balance = %d, want %d", b, 9000-lost)
	}
}

func TestRebuyErrors(t *testing.T) {
	srv, code, _, tokens, _ := startHeadsUp(t, adminOptions(Options{NextHandDelay: time.Minute}))
	if status, _ := rebuy(t, srv.URL, code, "", 10); status != http.StatusUnauthorized {
		t.Errorf("without login: %d, want 401", status)
	}
	if status, _ := rebuy(t, srv.URL, "NOPE00", tokens[0], 10); status != http.StatusNotFound {
		t.Errorf("missing room: %d, want 404", status)
	}
	if status, _ := rebuy(t, srv.URL, code, register(t, srv, "Cat"), 10); status != http.StatusConflict {
		t.Errorf("not seated: %d, want 409", status)
	}
	// The table allows it, the balance does not.
	admin := registerAdmin(t, srv)
	if status, _ := request(t, "POST", fmt.Sprintf("%s/api/admin/users/%d/balance", srv.URL, userID(t, srv.URL, tokens[0])), admin, `{"balance":1}`); status != http.StatusOK {
		t.Fatalf("set balance: %d", status)
	}
	if status, out := rebuy(t, srv.URL, code, tokens[0], 5); status != http.StatusConflict || out["error"] != "not enough chips" {
		t.Errorf("rebuy without chips: %d %v, want 409", status, out)
	}
}

func TestRebuyInHandWaitsForTheEnd(t *testing.T) {
	srv, code, conns, tokens, states := startHeadsUp(t, Options{NextHandDelay: time.Minute})
	// Seat 0 has posted a blind, so it has room under maxBuyIn.
	behind := states[0].Players[0].Stack
	if status, out := rebuy(t, srv.URL, code, tokens[0], 1000-behind); status != http.StatusOK {
		t.Fatalf("rebuy in hand: %d %v", status, out)
	}
	if status, _ := rebuy(t, srv.URL, code, tokens[0], 1); status != http.StatusBadRequest {
		t.Errorf("second rebuy over the limit: %d, want 400", status)
	}

	folder := states[0].ToAct
	conns[folder].send(`{"type":"act","action":"fold"}`)
	// Seat 0 gets the hand's result plus the rebuy.
	conns[0].waitState("rebuy applied after the hand", func(s *stateView) bool {
		return s.Street == "Showdown" && s.Players[0].Stack+s.Players[1].Stack == 2000+(1000-behind)
	})
}

func TestLeaveWaitsForRebuy(t *testing.T) {
	st := store.NewMemory()
	srv, l := newStoreServer(t, st)
	code := createRoom(t, srv)
	ann := register(t, srv, "Ann")
	sit(t, srv, code, ann, 500)
	id := userID(t, srv.URL, ann)
	tbl := l.room(code)

	seat, err := tbl.startRebuy(id, 300)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddChips(t.Context(), code, seat, id, 300); err != nil {
		t.Fatal(err)
	}
	if err := tbl.kick(seat); err != nil {
		t.Fatal(err)
	}
	if info := tbl.info(id); !info.Seated {
		t.Fatal("the seat was freed while the rebuy was being saved")
	}
	tbl.finishRebuy(id, 300, true)
	if info := tbl.info(id); info.Seated {
		t.Fatal("the player stayed after the rebuy finished")
	}
	l.rec.flush()
	if b := balanceOf(t, srv.URL, ann); b != 10000 {
		t.Errorf("balance = %d, want all 10000 back", b)
	}
}

func userID(t *testing.T, srvURL, session string) int64 {
	t.Helper()
	return int64(me(t, srvURL, session)["id"].(float64))
}
