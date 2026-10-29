package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// waitClosed reads until the server closes c and returns the close error.
func (c *testConn) waitClosed() websocket.CloseError {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	for {
		_, _, err := c.conn.Read(ctx)
		if err == nil {
			continue
		}
		var ce websocket.CloseError
		if !errors.As(err, &ce) {
			c.t.Fatalf("connection ended with %v, want a close frame", err)
		}
		return ce
	}
}

func TestLeaveBetweenHandsFreesSeat(t *testing.T) {
	srv, code, conns, _, states := startHeadsUp(t, Options{NextHandDelay: time.Minute})
	conns[states[0].ToAct].send(`{"type":"act","action":"fold"}`)
	conns[1].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })

	conns[0].send(`{"type":"leave"}`)
	if ce := conns[0].waitClosed(); ce.Code != CloseLeft {
		t.Errorf("leaver closed with %v, want code %d", ce, CloseLeft)
	}
	s := conns[1].waitState("Ann gone", func(s *stateView) bool { return s.Players[0] == nil })
	if s.Players[1].Name != "Bob" || s.You != 1 {
		t.Errorf("after Ann left: %+v (you %d)", s.Players, s.You)
	}

	if seat := joinSeat(t, srv, code, "Cat"); seat != 0 {
		t.Errorf("Cat got seat %d, want the free seat 0", seat)
	}
	s = conns[1].waitState("Cat seated", func(s *stateView) bool { return s.Players[0] != nil })
	if s.Players[0].Name != "Cat" || s.Players[0].Stack != 1000 {
		t.Errorf("new player at seat 0 = %+v", s.Players[0])
	}
}

func TestLeaveOnYourTurnFoldsAtOnce(t *testing.T) {
	_, _, conns, _, states := startHeadsUp(t, Options{NextHandDelay: time.Minute})
	actor := states[0].ToAct
	other := 1 - actor
	conns[actor].send(`{"type":"leave"}`)
	conns[actor].waitClosed()
	s := conns[other].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	if s.Players[other].Won != 15 {
		t.Errorf("the player who stayed must win the blinds: %+v", s.Players[other])
	}
	if actor < len(s.Players) && s.Players[actor] != nil {
		t.Errorf("leaver still seated after the hand: %+v", s.Players[actor])
	}
}

func TestLeaveWaitsForTurnAndHandEnd(t *testing.T) {
	_, _, conns, _, states := startHeadsUp(t, Options{NextHandDelay: time.Minute})
	actor := states[0].ToAct
	waiter := 1 - actor
	conns[waiter].send(`{"type":"leave"}`)
	// Still seated: the hand goes on until it is the leaver's turn.
	conns[actor].send(`{"type":"chat","text":"ping"}`)
	conns[actor].waitFor("chat", func(m serverMsg) bool { return m.Type == "chat" })

	conns[actor].send(`{"type":"act","action":"call"}`)
	if ce := conns[waiter].waitClosed(); ce.Code != CloseLeft {
		t.Errorf("leaver closed with %v, want code %d", ce, CloseLeft)
	}
	s := conns[actor].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	if s.Players[actor].Won != 20 {
		t.Errorf("leaver must fold when the turn comes: %+v", s.Players[actor])
	}
}

func TestAwayPlayerLosesSeat(t *testing.T) {
	srv := newTestServer(t, Options{AwayTimeout: 100 * time.Millisecond, TurnTimeout: 100 * time.Millisecond, NextHandDelay: time.Minute})
	code := createRoom(t, srv)
	ann := connect(t, srv, code, joinRoom(t, srv, code, "Ann"))
	joinRoom(t, srv, code, "Bob") // Bob never connects
	ann.waitState("hand 1", func(s *stateView) bool { return s.Hand == 1 })
	s := ann.waitState("Bob removed", func(s *stateView) bool { return len(s.Players) == 1 })
	if s.Players[0].Name != "Ann" {
		t.Errorf("players = %+v", s.Players)
	}
	// Ann stays: she is connected.
	time.Sleep(200 * time.Millisecond)
	ann.send(`{"type":"chat","text":"still here"}`)
	ann.waitFor("chat", func(m serverMsg) bool { return m.Type == "chat" && m.From == "Ann" })
}

func TestReconnectCancelsAwayTimer(t *testing.T) {
	const away = 200 * time.Millisecond
	srv, code, conns, tokens, _ := startHeadsUp(t, Options{AwayTimeout: away})
	conns[0].conn.Close(websocket.StatusNormalClosure, "")
	conns[1].waitState("Ann offline", func(s *stateView) bool { return !s.Players[0].Connected })
	again := connect(t, srv, code, tokens[0])
	again.waitState("Ann back", func(s *stateView) bool { return s.Players[0].Connected })
	time.Sleep(2 * away)
	again.send(`{"type":"chat","text":"hi"}`)
	if m := again.waitFor("chat", func(m serverMsg) bool { return m.Type == "chat" }); m.From != "Ann" {
		t.Errorf("chat from %q: Ann lost her seat", m.From)
	}
}

func joinSeat(t *testing.T, srv *httptest.Server, code, name string) int {
	t.Helper()
	status, out := postJSON(t, srv.URL+"/api/rooms/"+code+"/join", fmt.Sprintf(`{"name":%q}`, name))
	if status != http.StatusOK {
		t.Fatalf("join %s: %d %v", name, status, out)
	}
	return int(out["seat"].(float64))
}

func TestLeftSeatIsFreeAfterRestart(t *testing.T) {
	st := newMemStore()
	srv, l := newStoreServer(t, st)
	code := createRoom(t, srv)
	tokens := []string{joinRoom(t, srv, code, "Ann"), joinRoom(t, srv, code, "Bob"), joinRoom(t, srv, code, "Cat")}
	bob := connect(t, srv, code, tokens[1])
	bob.waitState("hand 1", func(s *stateView) bool { return s.Hand == 1 })
	bob.send(`{"type":"leave"}`) // Bob is in the hand: he leaves when it ends
	ann := connect(t, srv, code, tokens[0])
	s := ann.waitState("hand 1", func(s *stateView) bool { return s.Hand == 1 && s.ToAct >= 0 })
	for s.Street != "Showdown" {
		if s.ToAct == 0 {
			ann.send(`{"type":"act","action":"fold"}`)
		}
		s = ann.waitChange(s)
	}
	if s.Players[1] != nil {
		t.Fatalf("Bob still seated after the hand: %+v", s.Players[1])
	}
	l.rec.flush()
	srv.Close()

	srv2, _ := newStoreServer(t, st)
	cat := connect(t, srv2, code, tokens[2])
	s = cat.waitState("restored", func(s *stateView) bool { return len(s.Players) == 3 })
	if s.Players[0].Name != "Ann" || s.Players[1] != nil || s.Players[2].Name != "Cat" || s.You != 2 {
		t.Errorf("restored seats = %+v (you %d)", s.Players, s.You)
	}
	if seat := joinSeat(t, srv2, code, "Dan"); seat != 1 {
		t.Errorf("Dan got seat %d, want the free seat 1", seat)
	}
}
