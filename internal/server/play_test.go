package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const waitTimeout = 3 * time.Second

func newTestServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	if opts.NextHandDelay == 0 {
		opts.NextHandDelay = 50 * time.Millisecond
	}
	srv := httptest.NewServer(New(opts))
	t.Cleanup(srv.Close)
	return srv
}

func postJSON(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return resp.StatusCode, out
}

func createRoom(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	status, out := postJSON(t, srv.URL+"/api/rooms", `{"smallBlind":5,"bigBlind":10,"stack":1000}`)
	if status != http.StatusCreated {
		t.Fatalf("create room: %d %v", status, out)
	}
	return out["code"].(string)
}

func joinRoom(t *testing.T, srv *httptest.Server, code, name string) string {
	t.Helper()
	status, out := postJSON(t, srv.URL+"/api/rooms/"+code+"/join", fmt.Sprintf(`{"name":%q}`, name))
	if status != http.StatusOK {
		t.Fatalf("join %s: %d %v", name, status, out)
	}
	return out["token"].(string)
}

type testConn struct {
	t    *testing.T
	conn *websocket.Conn
}

func connect(t *testing.T, srv *httptest.Server, code, token string) *testConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + code + "&token=" + token
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { c.CloseNow() })
	return &testConn{t: t, conn: c}
}

func (c *testConn) send(msg string) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	if err := c.conn.Write(ctx, websocket.MessageText, []byte(msg)); err != nil {
		c.t.Fatalf("send %s: %v", msg, err)
	}
}

// waitFor reads messages until one matches ok.
func (c *testConn) waitFor(what string, ok func(serverMsg) bool) serverMsg {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			c.t.Fatalf("waiting for %s: %v", what, err)
		}
		var msg serverMsg
		if err := json.Unmarshal(data, &msg); err != nil {
			c.t.Fatalf("bad server message %s: %v", data, err)
		}
		if ok(msg) {
			return msg
		}
	}
}

func (c *testConn) waitState(what string, ok func(*stateView) bool) *stateView {
	c.t.Helper()
	return c.waitFor(what, func(m serverMsg) bool { return m.Type == "state" && ok(m.State) }).State
}

func (c *testConn) waitError() string {
	c.t.Helper()
	return c.waitFor("error", func(m serverMsg) bool { return m.Type == "error" }).Error
}

// startHeadsUp creates a room with two connected players and waits for hand 1.
func startHeadsUp(t *testing.T, opts Options) (srv *httptest.Server, code string, conns [2]*testConn, tokens [2]string, states [2]*stateView) {
	t.Helper()
	srv = newTestServer(t, opts)
	code = createRoom(t, srv)
	tokens[0] = joinRoom(t, srv, code, "Ann")
	tokens[1] = joinRoom(t, srv, code, "Bob")
	for i := range conns {
		conns[i] = connect(t, srv, code, tokens[i])
	}
	for i, c := range conns {
		states[i] = c.waitState("hand 1, both connected", func(s *stateView) bool {
			return s.Hand == 1 && s.Street == "Preflop" && s.Players[0].Connected && s.Players[1].Connected
		})
	}
	return srv, code, conns, tokens, states
}

func stateKey(s *stateView) string {
	return fmt.Sprint(s.Hand, s.Street, s.ToAct, s.Pot, len(s.Board))
}

// waitChange waits for the next state that differs from prev in the game itself.
func (c *testConn) waitChange(prev *stateView) *stateView {
	c.t.Helper()
	key := stateKey(prev)
	return c.waitState("state change", func(s *stateView) bool { return stateKey(s) != key })
}

func TestCreateAndJoinErrors(t *testing.T) {
	srv := newTestServer(t, Options{})
	bad := []string{
		`{"smallBlind":0,"bigBlind":10,"stack":1000}`,
		`{"smallBlind":10,"bigBlind":5,"stack":1000}`,
		`{"smallBlind":5,"bigBlind":10,"stack":5}`,
		`{"smallBlind":5,"bigBlind":10,"stack":2000000}`,
		`not json`,
	}
	for _, body := range bad {
		if status, _ := postJSON(t, srv.URL+"/api/rooms", body); status != http.StatusBadRequest {
			t.Errorf("create %s: status %d, want 400", body, status)
		}
	}
	if status, _ := postJSON(t, srv.URL+"/api/rooms/NOPE00/join", `{"name":"Ann"}`); status != http.StatusNotFound {
		t.Errorf("join unknown room: status %d, want 404", status)
	}
	code := createRoom(t, srv)
	for _, body := range []string{`{"name":"  "}`, `{"name":"123456789012345678901"}`} {
		if status, _ := postJSON(t, srv.URL+"/api/rooms/"+code+"/join", body); status != http.StatusBadRequest {
			t.Errorf("join %s: status %d, want 400", body, status)
		}
	}
	for i := range 9 {
		joinRoom(t, srv, strings.ToLower(code), fmt.Sprintf("p%d", i))
	}
	if status, _ := postJSON(t, srv.URL+"/api/rooms/"+code+"/join", `{"name":"late"}`); status != http.StatusConflict {
		t.Errorf("join full table: status %d, want 409", status)
	}
}

func TestRoomCodeFormat(t *testing.T) {
	srv := newTestServer(t, Options{})
	for range 20 {
		code := createRoom(t, srv)
		if len(code) != codeLen || strings.Trim(code, codeAlphabet) != "" {
			t.Errorf("bad room code %q", code)
		}
	}
}

func TestPlayersSeeOnlyTheirOwnCards(t *testing.T) {
	_, _, _, _, states := startHeadsUp(t, Options{})
	for i, s := range states {
		other := 1 - i
		if s.You != i || len(s.Players[i].Cards) != 2 || len(s.Players[other].Cards) != 0 {
			t.Errorf("player %d sees own %v and other %v", i, s.Players[i].Cards, s.Players[other].Cards)
		}
		if (s.Options != nil) != (s.ToAct == i) {
			t.Errorf("player %d: options %+v while ToAct=%d", i, s.Options, s.ToAct)
		}
		if s.Pot != 15 || s.Deadline == 0 || s.Players[i].Name == "" {
			t.Errorf("player %d: pot %d deadline %d", i, s.Pot, s.Deadline)
		}
	}
}

func TestFoldEndsHandAndNextHandStarts(t *testing.T) {
	_, _, conns, _, states := startHeadsUp(t, Options{})
	actor := states[0].ToAct
	other := 1 - actor
	conns[actor].send(`{"type":"act","action":"fold"}`)
	for i, c := range conns {
		s := c.waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
		if s.Players[other].Won != 15 || s.Players[actor].Stack != 995 || s.Players[other].Stack != 1005 {
			t.Errorf("player %d sees %+v", i, s.Players)
		}
		if len(s.Players[1-i].Cards) != 0 {
			t.Errorf("cards must stay hidden when everyone folded: %v", s.Players[1-i].Cards)
		}
		s = c.waitState("hand 2", func(s *stateView) bool { return s.Hand == 2 })
		if s.Button == states[0].Button || s.Street != "Preflop" {
			t.Errorf("hand 2: button %d (was %d), street %s", s.Button, states[0].Button, s.Street)
		}
	}
}

func TestShowdownRevealsCards(t *testing.T) {
	_, _, conns, _, s := startHeadsUp(t, Options{})
	for steps := 0; s[0].Street != "Showdown"; steps++ {
		if steps > 10 {
			t.Fatalf("hand did not reach showdown, state %+v", s[0])
		}
		actor := s[0].ToAct
		if s[actor].Options.CanCheck {
			conns[actor].send(`{"type":"act","action":"check"}`)
		} else {
			conns[actor].send(`{"type":"act","action":"call"}`)
		}
		for i := range conns {
			s[i] = conns[i].waitChange(s[i])
		}
	}
	for i := range s {
		won, stacks := 0, 0
		for _, p := range s[i].Players {
			won += p.Won
			stacks += p.Stack
			if len(p.Cards) != 2 {
				t.Errorf("player %d does not see %s's cards at showdown", i, p.Name)
			}
		}
		if won != 20 || stacks != 2000 || len(s[i].Board) != 5 {
			t.Errorf("player %d: won %d, stacks %d, board %v", i, won, stacks, s[i].Board)
		}
	}
}

func TestTurnTimerFoldsSlowPlayer(t *testing.T) {
	srv := newTestServer(t, Options{TurnTimeout: 100 * time.Millisecond, NextHandDelay: time.Minute})
	code := createRoom(t, srv)
	ann := connect(t, srv, code, joinRoom(t, srv, code, "Ann"))
	joinRoom(t, srv, code, "Bob") // Bob never connects
	s := ann.waitState("hand 1", func(s *stateView) bool { return s.Hand == 1 })
	first := s.ToAct
	s = ann.waitState("auto action", func(s *stateView) bool { return s.Street == "Showdown" })
	if !s.Players[first].Folded || s.Players[1-first].Won != 15 {
		t.Errorf("seat %d must be auto-folded: %+v", first, s.Players)
	}
	if s.Players[1].Connected {
		t.Error("Bob is shown as connected")
	}
}

func TestBadMessagesGetErrors(t *testing.T) {
	_, _, conns, _, states := startHeadsUp(t, Options{})
	idle := conns[1-states[0].ToAct]
	tests := []struct{ msg, want string }{
		{`{"type":"act","action":"call"}`, "not your turn"},
		{`{"type":"act","action":"dance"}`, "unknown action"},
		{`{"type":"jump"}`, "unknown message type"},
		{`not json`, "bad message"},
		{`{"type":"chat","text":"` + strings.Repeat("x", 201) + `"}`, "chat message"},
	}
	for _, tt := range tests {
		idle.send(tt.msg)
		if got := idle.waitError(); !strings.Contains(got, tt.want) {
			t.Errorf("%s: error %q, want it to contain %q", tt.msg, got, tt.want)
		}
	}
}

func TestChatReachesEveryone(t *testing.T) {
	_, _, conns, _, _ := startHeadsUp(t, Options{})
	conns[0].send(`{"type":"chat","text":"  удачи всем  "}`)
	for i, c := range conns {
		m := c.waitFor("chat", func(m serverMsg) bool { return m.Type == "chat" })
		if m.From != "Ann" || m.Text != "удачи всем" {
			t.Errorf("player %d got chat %+v", i, m)
		}
	}
}

func TestReconnectKeepsSeatAndCards(t *testing.T) {
	srv, code, conns, tokens, states := startHeadsUp(t, Options{})
	conns[0].conn.Close(websocket.StatusNormalClosure, "")
	conns[1].waitState("Ann offline", func(s *stateView) bool { return !s.Players[0].Connected })

	again := connect(t, srv, code, tokens[0])
	s := again.waitState("back", func(s *stateView) bool { return s.Players[0].Connected })
	if s.Hand != 1 || s.You != 0 || strings.Join(s.Players[0].Cards, " ") != strings.Join(states[0].Players[0].Cards, " ") {
		t.Errorf("after reconnect: hand %d, you %d, cards %v (were %v)", s.Hand, s.You, s.Players[0].Cards, states[0].Players[0].Cards)
	}
	conns[1].waitState("Ann online", func(s *stateView) bool { return s.Players[0].Connected })
}

func TestNewConnectionReplacesOld(t *testing.T) {
	srv, code, conns, tokens, _ := startHeadsUp(t, Options{})
	connect(t, srv, code, tokens[0])
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	for {
		if _, _, err := conns[0].conn.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("old connection was not closed")
			}
			return
		}
	}
}

func TestUnknownTokenIsRejected(t *testing.T) {
	srv := newTestServer(t, Options{})
	code := createRoom(t, srv)
	c := connect(t, srv, code, "not-a-token")
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	_, _, err := c.conn.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Errorf("read error = %v, want policy violation close", err)
	}
}

func TestTurnTimerRestartsAfterEachAction(t *testing.T) {
	const timeout = 400 * time.Millisecond
	_, _, conns, _, s := startHeadsUp(t, Options{TurnTimeout: timeout})
	time.Sleep(timeout / 2) // the first player uses half of the time
	actor := s[0].ToAct
	conns[actor].send(`{"type":"act","action":"call"}`)
	for i := range conns {
		s[i] = conns[i].waitChange(s[i])
	}
	deadline := time.UnixMilli(s[0].Deadline)
	auto := conns[0].waitChange(s[0]) // the second player never acts
	if now := time.Now(); now.Before(deadline.Add(-20 * time.Millisecond)) {
		t.Errorf("auto action %v before the deadline", deadline.Sub(now))
	}
	if auto.Street != "Flop" {
		t.Errorf("big blind must auto-check into the flop, got %s", auto.Street)
	}
}
