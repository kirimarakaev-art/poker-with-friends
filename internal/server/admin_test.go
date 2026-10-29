package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAdminPassword = "correct horse"

// adminClient talks to the admin API with its own cookie jar.
type adminClient struct {
	t   *testing.T
	srv *httptest.Server
	c   *http.Client
}

func newAdminClient(t *testing.T, srv *httptest.Server) *adminClient {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &adminClient{t: t, srv: srv, c: &http.Client{Jar: jar}}
}

// do sends a JSON request and decodes a JSON answer into out, if given.
func (a *adminClient) do(method, path, body string, out any) *http.Response {
	a.t.Helper()
	req, err := http.NewRequest(method, a.srv.URL+path, bytes.NewBufferString(body))
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.c.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			a.t.Fatalf("decode %s %s: %v", method, path, err)
		}
	}
	return resp
}

func (a *adminClient) login() {
	a.t.Helper()
	if resp := a.do("POST", "/api/admin/login", `{"password":"`+testAdminPassword+`"}`, nil); resp.StatusCode != http.StatusOK {
		a.t.Fatalf("login: %d", resp.StatusCode)
	}
}

func (a *adminClient) rooms() []adminRoomView {
	a.t.Helper()
	var out struct {
		Rooms []adminRoomView `json:"rooms"`
	}
	if resp := a.do("GET", "/api/admin/rooms", "", &out); resp.StatusCode != http.StatusOK {
		a.t.Fatalf("rooms: %d", resp.StatusCode)
	}
	return out.Rooms
}

func adminOptions(opts Options) Options {
	opts.AdminPassword = testAdminPassword
	return opts
}

func TestAdminIsOffWithoutPassword(t *testing.T) {
	srv := newTestServer(t, Options{})
	a := newAdminClient(t, srv)
	for _, path := range []string{"/api/admin/login", "/api/admin/rooms"} {
		if resp := a.do("POST", path, `{"password":""}`, nil); resp.StatusCode != http.StatusNotFound {
			t.Errorf("POST %s without ADMIN_PASSWORD: %d, want 404", path, resp.StatusCode)
		}
	}
}

func TestAdminLoginAndLogout(t *testing.T) {
	srv := newTestServer(t, adminOptions(Options{}))
	a := newAdminClient(t, srv)
	if resp := a.do("GET", "/api/admin/rooms", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("rooms before login: %d, want 401", resp.StatusCode)
	}
	if resp := a.do("POST", "/api/admin/login", `{"password":"wrong"}`, nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("wrong password: %d, want 401", resp.StatusCode)
	}

	resp := a.do("POST", "/api/admin/login", `{"password":"`+testAdminPassword+`"}`, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	cookie := resp.Header.Get("Set-Cookie")
	for _, want := range []string{"HttpOnly", "SameSite=Strict", "Path=/api/admin"} {
		if !strings.Contains(cookie, want) {
			t.Errorf("session cookie %q has no %s", cookie, want)
		}
	}
	if got := a.rooms(); len(got) != 0 {
		t.Errorf("rooms = %+v, want none", got)
	}

	a.do("POST", "/api/admin/logout", "", nil)
	if resp := a.do("GET", "/api/admin/rooms", "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("rooms after logout: %d, want 401", resp.StatusCode)
	}
}

func TestAdminLoginIsRateLimited(t *testing.T) {
	srv := newTestServer(t, adminOptions(Options{}))
	a := newAdminClient(t, srv)
	for range maxLoginFailures {
		a.do("POST", "/api/admin/login", `{"password":"guess"}`, nil)
	}
	resp := a.do("POST", "/api/admin/login", `{"password":"`+testAdminPassword+`"}`, nil)
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("login after %d failures: %d, want 429", maxLoginFailures, resp.StatusCode)
	}
}

func TestAdminChangesNeedJSON(t *testing.T) {
	srv := newTestServer(t, adminOptions(Options{}))
	a := newAdminClient(t, srv)
	a.login()
	code := createRoom(t, srv)
	joinRoom(t, srv, code, "Ann")
	// A cross-site form can post text/plain without a preflight; the admin API refuses it.
	resp, err := a.c.Post(srv.URL+"/api/admin/rooms/"+code+"/kick", "text/plain", strings.NewReader(`{"seat":0}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("kick as text/plain: %d, want 415", resp.StatusCode)
	}
}

func TestAdminListsRooms(t *testing.T) {
	srv, code, _, _, _ := startHeadsUp(t, adminOptions(Options{}))
	a := newAdminClient(t, srv)
	a.login()
	rooms := a.rooms()
	if len(rooms) != 1 {
		t.Fatalf("rooms = %+v", rooms)
	}
	r := rooms[0]
	if r.Code != code || r.SmallBlind != 5 || r.BigBlind != 10 || r.Hand != 1 || r.Street != "Preflop" || len(r.Players) != 2 {
		t.Errorf("room = %+v", r)
	}
	for i, name := range []string{"Ann", "Bob"} {
		p := r.Players[i]
		if p == nil || p.Name != name || !p.Connected || !p.InHand || p.Stack+p.Bet != 1000 {
			t.Errorf("player %d = %+v", i, p)
		}
	}
}

func TestAdminKicksPlayer(t *testing.T) {
	srv, code, conns, _, states := startHeadsUp(t, adminOptions(Options{NextHandDelay: time.Minute}))
	conns[states[0].ToAct].send(`{"type":"act","action":"fold"}`)
	conns[1].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	a := newAdminClient(t, srv)
	a.login()

	if resp := a.do("POST", "/api/admin/rooms/"+code+"/kick", `{"seat":5}`, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("kick empty seat: %d, want 404", resp.StatusCode)
	}
	if resp := a.do("POST", "/api/admin/rooms/"+code+"/kick", `{"seat":0}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("kick: %d", resp.StatusCode)
	}
	if ce := conns[0].waitClosed(); ce.Code != CloseLeft {
		t.Errorf("kicked player closed with %v, want %d", ce, CloseLeft)
	}
	conns[1].waitState("Ann gone", func(s *stateView) bool { return s.Players[0] == nil })
}

func TestAdminSetsStackBetweenHands(t *testing.T) {
	srv, code, conns, _, states := startHeadsUp(t, adminOptions(Options{NextHandDelay: time.Minute}))
	conns[states[0].ToAct].send(`{"type":"act","action":"fold"}`)
	conns[1].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	a := newAdminClient(t, srv)
	a.login()

	for _, body := range []string{`{"seat":0,"stack":-1}`, `{"seat":0,"stack":2000000}`, `not json`} {
		if resp := a.do("POST", "/api/admin/rooms/"+code+"/stack", body, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("stack %s: %d, want 400", body, resp.StatusCode)
		}
	}
	if resp := a.do("POST", "/api/admin/rooms/"+code+"/stack", `{"seat":0,"stack":5000}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("set stack: %d", resp.StatusCode)
	}
	s := conns[1].waitState("new stack", func(s *stateView) bool { return s.Players[0].Stack == 5000 })
	if s.Players[1].Stack == 5000 {
		t.Error("the other player's stack changed too")
	}
}

func TestAdminStackInHandAppliesAfterIt(t *testing.T) {
	srv, code, conns, _, states := startHeadsUp(t, adminOptions(Options{NextHandDelay: time.Minute}))
	a := newAdminClient(t, srv)
	a.login()
	if resp := a.do("POST", "/api/admin/rooms/"+code+"/stack", `{"seat":0,"stack":3000}`, nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("set stack: %d", resp.StatusCode)
	}
	if got := a.rooms()[0].Players[0]; got.Stack == 3000 || got.PendingStack == nil || *got.PendingStack != 3000 {
		t.Errorf("during the hand: %+v, want the old stack and 3000 pending", got)
	}
	conns[states[0].ToAct].send(`{"type":"act","action":"fold"}`)
	s := conns[1].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	if s.Players[0].Stack != 3000 {
		t.Errorf("after the hand Ann has %d, want 3000", s.Players[0].Stack)
	}
}

func TestAdminClosesRoom(t *testing.T) {
	srv, code, conns, _, _ := startHeadsUp(t, adminOptions(Options{}))
	a := newAdminClient(t, srv)
	a.login()
	if resp := a.do("DELETE", "/api/admin/rooms/"+code, "", nil); resp.StatusCode != http.StatusOK {
		t.Fatalf("close room: %d", resp.StatusCode)
	}
	for i, c := range conns {
		if ce := c.waitClosed(); ce.Code != CloseRoomNotFound {
			t.Errorf("player %d closed with %v, want %d", i, ce, CloseRoomNotFound)
		}
	}
	if status, _ := postJSON(t, srv.URL+"/api/rooms/"+code+"/join", `{"name":"Cat"}`); status != http.StatusNotFound {
		t.Errorf("join closed room: %d, want 404", status)
	}
	if got := a.rooms(); len(got) != 0 {
		t.Errorf("rooms after close = %+v", got)
	}
	if resp := a.do("DELETE", "/api/admin/rooms/"+code, "", nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("close again: %d, want 404", resp.StatusCode)
	}
}

func TestAdminChangesAreSaved(t *testing.T) {
	st := newMemStore()
	l := newLobby(Options{Store: st, AdminPassword: testAdminPassword, NextHandDelay: time.Minute})
	srv := httptest.NewServer(newHandler(l))
	t.Cleanup(srv.Close)
	a := newAdminClient(t, srv)
	a.login()

	code := createRoom(t, srv)
	joinRoom(t, srv, code, "Ann")
	a.do("POST", "/api/admin/rooms/"+code+"/stack", `{"seat":0,"stack":777}`, nil)
	l.rec.flush()
	rooms, _ := st.LoadRooms(context.Background())
	if len(rooms) != 1 || rooms[0].Players[0].Stack != 777 {
		t.Fatalf("saved after stack change: %+v", rooms)
	}

	a.do("DELETE", "/api/admin/rooms/"+code, "", nil)
	l.rec.flush()
	if rooms, _ := st.LoadRooms(context.Background()); len(rooms) != 0 {
		t.Errorf("closed room is still saved: %+v", rooms)
	}
}
