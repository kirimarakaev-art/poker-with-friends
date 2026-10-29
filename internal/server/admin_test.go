package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

const testAdmin = "Boss"

func adminOptions(opts Options) Options {
	opts.AdminUsername = testAdmin
	return opts
}

// adminRooms lists the rooms as the admin session sees them.
func adminRooms(t *testing.T, srv *httptest.Server, admin string) []adminRoomView {
	t.Helper()
	req, _ := http.NewRequest("GET", srv.URL+"/api/admin/rooms", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: admin})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("admin rooms: %d", resp.StatusCode)
	}
	var out struct {
		Rooms []adminRoomView `json:"rooms"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out.Rooms
}

func TestAdminNeedsRole(t *testing.T) {
	srv := newTestServer(t, adminOptions(Options{}))
	if status, _ := request(t, "GET", srv.URL+"/api/admin/users", "", ""); status != http.StatusUnauthorized {
		t.Errorf("without login: %d, want 401", status)
	}
	if status, _ := request(t, "GET", srv.URL+"/api/admin/users", register(t, srv, "Ann"), ""); status != http.StatusForbidden {
		t.Errorf("as a player: %d, want 403", status)
	}
	// Registering the admin's name does not make an admin: anyone could
	// take a name that is not registered yet. The role comes at start.
	early := register(t, srv, "boss")
	if me(t, srv.URL, early)["isAdmin"] != false {
		t.Fatal("registration gave the admin role")
	}
	v, _ := testLobbies.Load(srv.URL)
	if err := v.(*lobby).grantAdmin(context.Background()); err != nil {
		t.Fatal(err)
	}
	admin := early // any case of AdminUsername
	if me(t, srv.URL, admin)["isAdmin"] != true {
		t.Fatal("the admin account has no admin role after start")
	}
	if status, _ := request(t, "GET", srv.URL+"/api/admin/users", admin, ""); status != http.StatusOK {
		t.Errorf("as admin: %d, want 200", status)
	}
}

func TestAdminRoleForExistingAccount(t *testing.T) {
	st := store.NewMemory()
	srv := httptest.NewServer(New(Options{Store: st}))
	boss := register(t, srv, testAdmin)
	srv.Close()

	h, err := Open(context.Background(), adminOptions(Options{Store: st}))
	if err != nil {
		t.Fatal(err)
	}
	srv2 := httptest.NewServer(h)
	t.Cleanup(srv2.Close)
	if me(t, srv2.URL, boss)["isAdmin"] != true {
		t.Error("an account registered before ADMIN_USERNAME was set did not become admin on start")
	}
}

func TestAdminListsRooms(t *testing.T) {
	srv, code, _, _, _ := startHeadsUp(t, adminOptions(Options{}))
	rooms := adminRooms(t, srv, registerAdmin(t, srv))
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

func TestAdminKickReturnsChips(t *testing.T) {
	srv, code, conns, tokens, states := startHeadsUp(t, adminOptions(Options{NextHandDelay: time.Minute}))
	conns[states[0].ToAct].send(`{"type":"act","action":"fold"}`)
	conns[1].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	admin := registerAdmin(t, srv)

	if status, _ := request(t, "POST", srv.URL+"/api/admin/rooms/"+code+"/kick", admin, `{"seat":5}`); status != http.StatusNotFound {
		t.Errorf("kick empty seat: %d, want 404", status)
	}
	if status, _ := request(t, "POST", srv.URL+"/api/admin/rooms/"+code+"/kick", admin, `{"seat":0}`); status != http.StatusOK {
		t.Fatalf("kick: %d", status)
	}
	if ce := conns[0].waitClosed(); ce.Code != CloseLeft {
		t.Errorf("kicked player closed with %v, want %d", ce, CloseLeft)
	}
	conns[1].waitState("Ann gone", func(s *stateView) bool { return s.Players[0] == nil })
	eventually(t, "Ann's chips back", func() bool { return balanceOf(t, srv.URL, tokens[0]) > 9000 })
}

func TestAdminClosesRoom(t *testing.T) {
	srv, code, conns, tokens, _ := startHeadsUp(t, adminOptions(Options{}))
	admin := registerAdmin(t, srv)
	if status, _ := request(t, "DELETE", srv.URL+"/api/admin/rooms/"+code, admin, ""); status != http.StatusOK {
		t.Fatalf("close room: %d", status)
	}
	for i, c := range conns {
		if ce := c.waitClosed(); ce.Code != CloseRoomNotFound {
			t.Errorf("player %d closed with %v, want %d", i, ce, CloseRoomNotFound)
		}
	}
	// The hand in play is void: everyone gets back the stack saved before it.
	for _, tok := range tokens {
		eventually(t, "buy-in back", func() bool { return balanceOf(t, srv.URL, tok) == 10000 })
	}
	if status, _ := request(t, "POST", srv.URL+"/api/rooms/"+code+"/join", register(t, srv, "Cat"), `{"buyIn":500}`); status != http.StatusNotFound {
		t.Errorf("join closed room: %d, want 404", status)
	}
	if got := adminRooms(t, srv, admin); len(got) != 0 {
		t.Errorf("rooms after close = %+v", got)
	}
	if status, _ := request(t, "DELETE", srv.URL+"/api/admin/rooms/"+code, admin, ""); status != http.StatusNotFound {
		t.Errorf("close again: %d, want 404", status)
	}
}

func TestAdminSetsBalance(t *testing.T) {
	srv := newTestServer(t, adminOptions(Options{}))
	admin := registerAdmin(t, srv)
	ann := register(t, srv, "Ann")
	status, out := request(t, "GET", srv.URL+"/api/admin/users", admin, "")
	users, _ := out["users"].([]any)
	if status != http.StatusOK || len(users) != 2 {
		t.Fatalf("users: %d %v", status, out)
	}
	first := users[0].(map[string]any)
	if first["username"] != "Ann" {
		t.Errorf("users not sorted by name: %v", users)
	}
	path := fmt.Sprintf("%s/api/admin/users/%d/balance", srv.URL, int64(first["id"].(float64)))

	for _, body := range []string{`{"balance":-1}`, `{"balance":1000000001}`, `nope`} {
		if status, _ := request(t, "POST", path, admin, body); status != http.StatusBadRequest {
			t.Errorf("balance %s: %d, want 400", body, status)
		}
	}
	if status, _ := request(t, "POST", srv.URL+"/api/admin/users/999/balance", admin, `{"balance":5}`); status != http.StatusNotFound {
		t.Errorf("missing user: %d, want 404", status)
	}
	if status, u := request(t, "POST", path, admin, `{"balance":123}`); status != http.StatusOK || u["balance"].(float64) != 123 {
		t.Errorf("set balance: %d %v", status, u)
	}
	if b := balanceOf(t, srv.URL, ann); b != 123 {
		t.Errorf("Ann's balance = %d, want 123", b)
	}
}

func TestChangesNeedJSON(t *testing.T) {
	srv := newTestServer(t, Options{})
	// A cross-site form can post text/plain without a preflight; the API refuses it.
	resp, err := http.Post(srv.URL+"/api/auth/login", "text/plain", strings.NewReader(`{"username":"Ann","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("login as text/plain: %d, want 415", resp.StatusCode)
	}
}
