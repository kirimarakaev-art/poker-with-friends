package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/crypto/bcrypt"
)

const waitTimeout = 3 * time.Second

func newTestServer(t *testing.T, opts Options) *httptest.Server {
	t.Helper()
	if opts.NextHandDelay == 0 {
		opts.NextHandDelay = 50 * time.Millisecond
	}
	l := newLobby(opts)
	srv := httptest.NewServer(newHandler(l))
	t.Cleanup(srv.Close)
	testLobbies.Store(srv.URL, l)
	return srv
}

// testLobbies lets tests reach the lobby behind a test server.
var testLobbies sync.Map // URL -> *lobby

// registerAdmin registers Options.AdminUsername and grants the role, as a
// server start would.
func registerAdmin(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	v, _ := testLobbies.Load(srv.URL)
	l := v.(*lobby)
	session := register(t, srv, l.opts.AdminUsername)
	if err := l.grantAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	return session
}

const testPassword = "password123"

func init() {
	bcryptCost = bcrypt.MinCost // the default cost makes every test login slow
}

// request sends a JSON request with the session token as a cookie, if any,
// and decodes a JSON object answer.
func request(t *testing.T, method, url, session, body string) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if session != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: session})
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode response of %s %s: %v", method, url, err)
	}
	return resp.StatusCode, out
}

func postJSON(t *testing.T, url, body string) (int, map[string]any) {
	t.Helper()
	return request(t, "POST", url, "", body)
}

// register creates the account name and returns its session token. Each
// call comes from its own address, so the registration limit does not apply.
func register(t *testing.T, srv *httptest.Server, name string) string {
	t.Helper()
	ip := clientCount.Add(1)
	resp, status := registerFrom(t, srv, name, fmt.Sprintf("10.%d.%d.%d", ip>>16&255, ip>>8&255, ip&255))
	if status != http.StatusCreated {
		t.Fatalf("register %s: %d", name, status)
	}
	for _, c := range resp.Cookies() {
		if c.Name == sessionCookie {
			return c.Value
		}
	}
	t.Fatalf("register %s: no session cookie", name)
	return ""
}

var hostCount, clientCount atomic.Int64

// registerFrom registers name as a client at ip.
func registerFrom(t *testing.T, srv *httptest.Server, name, ip string) (*http.Response, int) {
	t.Helper()
	body := fmt.Sprintf(`{"username":%q,"password":%q}`, name, testPassword)
	req, _ := http.NewRequest("POST", srv.URL+"/api/auth/register", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", ip)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp, resp.StatusCode
}

// createRoom registers a new host and creates a public 5/10 room with buy-ins 100 to 1000.
func createRoom(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	host := register(t, srv, fmt.Sprintf("host%d", hostCount.Add(1)))
	status, out := request(t, "POST", srv.URL+"/api/rooms", host, `{"smallBlind":5,"bigBlind":10,"minBuyIn":100,"maxBuyIn":1000,"public":true}`)
	if status != http.StatusCreated {
		t.Fatalf("create room: %d %v", status, out)
	}
	return out["code"].(string)
}

// joinRoom registers name, buys in for 1000 and returns the session token.
func joinRoom(t *testing.T, srv *httptest.Server, code, name string) string {
	t.Helper()
	session := register(t, srv, name)
	sit(t, srv, code, session, 1000)
	return session
}

// sit buys in at code and returns the seat.
func sit(t *testing.T, srv *httptest.Server, code, session string, buyIn int) int {
	t.Helper()
	status, out := request(t, "POST", srv.URL+"/api/rooms/"+code+"/join", session, fmt.Sprintf(`{"buyIn":%d}`, buyIn))
	if status != http.StatusOK {
		t.Fatalf("join %s: %d %v", code, status, out)
	}
	return int(out["seat"].(float64))
}

type testConn struct {
	t    *testing.T
	conn *websocket.Conn
}

// connect opens the game connection of the session's user.
func connect(t *testing.T, srv *httptest.Server, code, session string) *testConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws?room=" + code
	header := http.Header{"Cookie": {sessionCookie + "=" + session}}
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{HTTPHeader: header})
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
	if status, _ := postJSON(t, srv.URL+"/api/rooms", `{"smallBlind":5,"bigBlind":10,"minBuyIn":100,"maxBuyIn":1000}`); status != http.StatusUnauthorized {
		t.Errorf("create without login: %d, want 401", status)
	}
	host := register(t, srv, "host")
	bad := []string{
		`{"smallBlind":0,"bigBlind":10,"minBuyIn":100,"maxBuyIn":1000}`,
		`{"smallBlind":10,"bigBlind":5,"minBuyIn":100,"maxBuyIn":1000}`,
		`{"smallBlind":5,"bigBlind":10,"minBuyIn":5,"maxBuyIn":1000}`,
		`{"smallBlind":5,"bigBlind":10,"minBuyIn":500,"maxBuyIn":100}`,
		`{"smallBlind":5,"bigBlind":10,"minBuyIn":100,"maxBuyIn":2000000}`,
		`not json`,
	}
	for _, body := range bad {
		if status, _ := request(t, "POST", srv.URL+"/api/rooms", host, body); status != http.StatusBadRequest {
			t.Errorf("create %s: status %d, want 400", body, status)
		}
	}
	if status, _ := request(t, "POST", srv.URL+"/api/rooms/NOPE00/join", host, `{"buyIn":100}`); status != http.StatusNotFound {
		t.Errorf("join unknown room: status %d, want 404", status)
	}
	code := createRoom(t, srv)
	for _, body := range []string{`{"buyIn":99}`, `{"buyIn":1001}`} {
		if status, _ := request(t, "POST", srv.URL+"/api/rooms/"+code+"/join", host, body); status != http.StatusBadRequest {
			t.Errorf("join %s: status %d, want 400", body, status)
		}
	}
	for i := range 9 {
		joinRoom(t, srv, strings.ToLower(code), fmt.Sprintf("player%d", i))
	}
	if status, _ := request(t, "POST", srv.URL+"/api/rooms/"+code+"/join", host, `{"buyIn":100}`); status != http.StatusConflict {
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
		if name := s.Players[i].HandName; name != "Pair" && name != "High Card" {
			t.Errorf("player %d: own hand name %q before the flop", i, name)
		}
		if name := s.Players[other].HandName; name != "" {
			t.Errorf("player %d sees the other's hand name %q", i, name)
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
		if s.Players[1-i].HandName != "" || s.Players[actor].HandName != "" {
			t.Errorf("player %d: hand names must be hidden, or gone after a fold: %+v", i, s.Players)
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
			if p.HandName == "" {
				t.Errorf("player %d does not see %s's hand name at showdown", i, p.Name)
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
	fresh := connect(t, srv, code, tokens[0])
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	var err error
	for err == nil {
		_, _, err = conns[0].conn.Read(ctx)
	}
	if ctx.Err() != nil {
		t.Fatal("old connection was not closed")
	}
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != CloseReplaced || ce.Reason != "replaced by another connection" {
		t.Errorf("old connection closed with %v, want code %d", err, CloseReplaced)
	}
	fresh.waitState("new connection still works", func(s *stateView) bool { return s.Players[0].Connected })
}

func TestGameNeedsLoginAndSeat(t *testing.T) {
	srv := newTestServer(t, Options{})
	code := createRoom(t, srv)
	stranger := register(t, srv, "Stranger")
	for name, session := range map[string]string{"no session": "", "bad session": "nope", "not seated": stranger} {
		c := connect(t, srv, code, session)
		ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
		_, _, err := c.conn.Read(ctx)
		cancel()
		if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
			t.Errorf("%s: read error = %v, want policy violation close", name, err)
		}
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

func TestUnknownRoomIsClosedWithCode(t *testing.T) {
	srv := newTestServer(t, Options{})
	c := connect(t, srv, "NOPE00", "any-token")
	ctx, cancel := context.WithTimeout(context.Background(), waitTimeout)
	defer cancel()
	_, _, err := c.conn.Read(ctx)
	var ce websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != CloseRoomNotFound || ce.Reason != "room not found" {
		t.Errorf("read error = %v, want close code %d", err, CloseRoomNotFound)
	}
}
