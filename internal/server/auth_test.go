package server

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

func me(t *testing.T, url, session string) map[string]any {
	t.Helper()
	status, out := request(t, "GET", url+"/api/me", session, "")
	if status != http.StatusOK {
		t.Fatalf("GET /api/me: %d %v", status, out)
	}
	return out
}

func balanceOf(t *testing.T, url, session string) int {
	t.Helper()
	return int(me(t, url, session)["balance"].(float64))
}

// eventually retries check until it passes: stacks and history are saved
// in the background after a hand.
func eventually(t *testing.T, what string, check func() bool) {
	t.Helper()
	for deadline := time.Now().Add(waitTimeout); !check(); time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestRegisterLoginLogout(t *testing.T) {
	srv := newTestServer(t, Options{})
	session := register(t, srv, "Керим_1")
	u := me(t, srv.URL, session)
	if u["username"] != "Керим_1" || u["balance"].(float64) != 10000 || u["isAdmin"] != false || u["bonusAvailable"] != false {
		t.Errorf("new user = %v", u)
	}
	if stats := u["stats"].(map[string]any); stats["hands"].(float64) != 0 || stats["handsWon"].(float64) != 0 {
		t.Errorf("new user stats = %v", stats)
	}

	if status, _ := postJSON(t, srv.URL+"/api/auth/register", `{"username":"керим_1","password":"password123"}`); status != http.StatusConflict {
		t.Errorf("same name in other case: %d, want 409", status)
	}
	if status, _ := postJSON(t, srv.URL+"/api/auth/login", `{"username":"КЕРИМ_1","password":"wrong-password"}`); status != http.StatusUnauthorized {
		t.Errorf("wrong password: %d, want 401", status)
	}
	if status, _ := postJSON(t, srv.URL+"/api/auth/login", `{"username":"nobody","password":"password123"}`); status != http.StatusUnauthorized {
		t.Errorf("unknown user: %d, want 401", status)
	}

	resp, err := http.Post(srv.URL+"/api/auth/login", "application/json", strings.NewReader(`{"username":"КЕРИМ_1","password":"password123"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cookie := resp.Header.Get("Set-Cookie")
	if resp.StatusCode != http.StatusOK || !strings.Contains(cookie, "HttpOnly") || !strings.Contains(cookie, "SameSite=Strict") || !strings.Contains(cookie, "Path=/") {
		t.Errorf("login: %d, cookie %q", resp.StatusCode, cookie)
	}

	request(t, "POST", srv.URL+"/api/auth/logout", session, `{}`)
	if status, _ := request(t, "GET", srv.URL+"/api/me", session, ""); status != http.StatusUnauthorized {
		t.Errorf("me after logout: %d, want 401", status)
	}
	if status, _ := request(t, "GET", srv.URL+"/api/me", "", ""); status != http.StatusUnauthorized {
		t.Errorf("me without session: %d, want 401", status)
	}
}

func TestRegisterValidation(t *testing.T) {
	srv := newTestServer(t, Options{})
	tests := []struct{ username, password string }{
		{"ab", "password123"},
		{strings.Repeat("a", 21), "password123"},
		{"has space", "password123"},
		{"semi;colon", "password123"},
		{"fine_name", "short"},
		{"fine_name", strings.Repeat("x", 73)},
	}
	for _, tt := range tests {
		body := fmt.Sprintf(`{"username":%q,"password":%q}`, tt.username, tt.password)
		if status, _ := postJSON(t, srv.URL+"/api/auth/register", body); status != http.StatusBadRequest {
			t.Errorf("register %q/%d bytes: %d, want 400", tt.username, len(tt.password), status)
		}
	}
	for _, ok := range []string{"Ann", "a.b-c_d", "Ёжик2000", strings.Repeat("я", 20)} {
		register(t, srv, ok)
	}
}

func TestLoginIsRateLimited(t *testing.T) {
	srv := newTestServer(t, Options{})
	register(t, srv, "Ann")
	for range maxAuthFailures {
		postJSON(t, srv.URL+"/api/auth/login", `{"username":"Ann","password":"guess-guess"}`)
	}
	if status, _ := postJSON(t, srv.URL+"/api/auth/login", `{"username":"Ann","password":"password123"}`); status != http.StatusTooManyRequests {
		t.Errorf("login after %d failures: %d, want 429", maxAuthFailures, status)
	}
}

func TestRegistrationsAreLimited(t *testing.T) {
	srv := newTestServer(t, Options{})
	for i := range maxSignups {
		if _, status := registerFrom(t, srv, fmt.Sprintf("Ann%d", i), "10.9.9.9"); status != http.StatusCreated {
			t.Fatalf("registration %d: %d", i, status)
		}
	}
	if _, status := registerFrom(t, srv, "Extra", "10.9.9.9"); status != http.StatusTooManyRequests {
		t.Errorf("registration over the limit: %d, want 429", status)
	}
	if _, status := registerFrom(t, srv, "Other", "10.9.9.10"); status != http.StatusCreated {
		t.Errorf("registration from another address: %d, want 201", status)
	}
}

func TestBuyInAndCashOut(t *testing.T) {
	srv, code, conns, tokens, states := startHeadsUp(t, Options{NextHandDelay: time.Minute})
	if b := balanceOf(t, srv.URL, tokens[0]); b != 9000 {
		t.Errorf("balance after a 1000 buy-in = %d, want 9000", b)
	}
	// Joining again keeps the seat and charges nothing.
	if seat := sit(t, srv, code, tokens[0], 500); seat != 0 || balanceOf(t, srv.URL, tokens[0]) != 9000 {
		t.Errorf("second join: seat %d, balance %d", seat, balanceOf(t, srv.URL, tokens[0]))
	}

	folder := states[0].ToAct
	conns[folder].send(`{"type":"act","action":"fold"}`)
	conns[1-folder].waitState("showdown", func(s *stateView) bool { return s.Street == "Showdown" })
	conns[folder].send(`{"type":"leave"}`)
	conns[folder].waitClosed()
	conns[1-folder].waitState("folder gone", func(s *stateView) bool { return len(s.Players) < 2 || s.Players[folder] == nil })
	// The stack (1000 minus the folded blind) is back on the balance.
	want := 10000 - map[bool]int{true: 5, false: 10}[states[0].Button == folder]
	eventually(t, fmt.Sprintf("balance %d after leaving", want), func() bool { return balanceOf(t, srv.URL, tokens[folder]) == want })
	eventually(t, "winner's stats", func() bool {
		stats := me(t, srv.URL, tokens[1-folder])["stats"].(map[string]any)
		return stats["hands"].(float64) == 1 && stats["handsWon"].(float64) == 1
	})
}

func TestBuyInNeedsChips(t *testing.T) {
	srv := newTestServer(t, Options{})
	host := register(t, srv, "host")
	_, out := request(t, "POST", srv.URL+"/api/rooms", host, `{"smallBlind":50,"bigBlind":100,"minBuyIn":2000,"maxBuyIn":20000,"public":false}`)
	code := out["code"].(string)
	ann := register(t, srv, "Ann")
	sit(t, srv, code, ann, 9500)
	if b := balanceOf(t, srv.URL, ann); b != 500 {
		t.Fatalf("balance = %d, want 500", b)
	}
	bob := register(t, srv, "Bob")
	if status, out := request(t, "POST", srv.URL+"/api/rooms/"+code+"/join", bob, `{"buyIn":15000}`); status != http.StatusConflict || out["error"] != "not enough chips" {
		t.Errorf("buy-in above balance: %d %v", status, out)
	}
	if b := balanceOf(t, srv.URL, bob); b != 10000 {
		t.Errorf("failed buy-in changed the balance to %d", b)
	}
	// The seat reserved for the failed buy-in is free again.
	if seat := sit(t, srv, code, bob, 10000); seat != 1 {
		t.Errorf("Bob got seat %d, want 1", seat)
	}
}

func TestDailyBonus(t *testing.T) {
	srv := newTestServer(t, Options{})
	ann := register(t, srv, "Ann")
	if status, _ := request(t, "POST", srv.URL+"/api/me/bonus", ann, `{}`); status != http.StatusConflict {
		t.Errorf("bonus with 10000 chips: %d, want 409", status)
	}
	host := register(t, srv, "host")
	_, out := request(t, "POST", srv.URL+"/api/rooms", host, `{"smallBlind":5,"bigBlind":10,"minBuyIn":100,"maxBuyIn":10000,"public":true}`)
	sit(t, srv, out["code"].(string), ann, 9500)
	if me(t, srv.URL, ann)["bonusAvailable"] != true {
		t.Error("bonus not offered at 500 chips")
	}
	status, u := request(t, "POST", srv.URL+"/api/me/bonus", ann, `{}`)
	if status != http.StatusOK || u["balance"].(float64) != 1500 || u["bonusAvailable"] != false {
		t.Errorf("bonus: %d %v", status, u)
	}
	if status, _ := request(t, "POST", srv.URL+"/api/me/bonus", ann, `{}`); status != http.StatusConflict {
		t.Errorf("second bonus: %d, want 409", status)
	}
}

func TestRoomsPerOwnerAreLimited(t *testing.T) {
	srv := newTestServer(t, Options{})
	ann := register(t, srv, "Ann")
	const body = `{"smallBlind":5,"bigBlind":10,"minBuyIn":100,"maxBuyIn":1000,"public":true}`
	for i := range maxOwnRooms {
		if status, out := request(t, "POST", srv.URL+"/api/rooms", ann, body); status != http.StatusCreated {
			t.Fatalf("room %d: %d %v", i, status, out)
		}
	}
	if status, _ := request(t, "POST", srv.URL+"/api/rooms", ann, body); status != http.StatusTooManyRequests {
		t.Errorf("room over the limit: %d, want 429", status)
	}
	if status, _ := request(t, "POST", srv.URL+"/api/rooms", register(t, srv, "Bob"), body); status != http.StatusCreated {
		t.Errorf("another owner: %d, want 201", status)
	}
}

func TestOldEmptyRoomIsNotActive(t *testing.T) {
	now := time.Now()
	old := newTable(tableConfig{ownerID: 1, created: now.Add(-freshRoom - time.Second)})
	t.Cleanup(old.close)
	if old.activeOf(1, now) {
		t.Error("an empty room older than freshRoom counts against its owner")
	}
	if fresh := newTable(tableConfig{ownerID: 1, created: now}); !fresh.activeOf(1, now) {
		t.Error("a new empty room does not count")
	} else {
		fresh.close()
	}
	if old.activeOf(2, now) {
		t.Error("a room counts against another user")
	}
}

func TestRoomLists(t *testing.T) {
	srv := newTestServer(t, Options{})
	ann := register(t, srv, "Ann")
	create := func(public bool) string {
		_, out := request(t, "POST", srv.URL+"/api/rooms", ann, fmt.Sprintf(`{"smallBlind":5,"bigBlind":10,"minBuyIn":100,"maxBuyIn":1000,"public":%t}`, public))
		return out["code"].(string)
	}
	open, private, empty := create(true), create(false), create(true)
	sit(t, srv, open, ann, 1000)
	sit(t, srv, private, ann, 1000)
	joinRoom(t, srv, open, "Bob")

	rooms := func(path, session string) []map[string]any {
		t.Helper()
		status, out := request(t, "GET", srv.URL+path, session, "")
		if status != http.StatusOK {
			t.Fatalf("GET %s: %d %v", path, status, out)
		}
		var list []map[string]any
		for _, r := range out["rooms"].([]any) {
			list = append(list, r.(map[string]any))
		}
		return list
	}
	codes := func(list []map[string]any) []string {
		var out []string
		for _, r := range list {
			out = append(out, r["code"].(string))
		}
		return out
	}

	cat := register(t, srv, "Cat")
	public := rooms("/api/rooms", cat)
	if got := codes(public); len(got) != 2 || got[0] != open || got[1] != empty {
		t.Errorf("public rooms = %v, want [%s %s] (fullest first, no private)", got, open, empty)
	}
	if r := public[0]; r["owner"] != "Ann" || r["players"].(float64) != 2 || r["maxPlayers"].(float64) != 9 || r["seated"] != false || r["minBuyIn"].(float64) != 100 {
		t.Errorf("room info = %v", r)
	}
	if got := codes(rooms("/api/me/rooms", ann)); len(got) != 2 {
		t.Errorf("Ann's rooms = %v, want the open and the private one", got)
	}
	status, info := request(t, "GET", srv.URL+"/api/rooms/"+strings.ToLower(private), cat, "")
	if status != http.StatusOK || info["public"] != false || info["seated"] != false {
		t.Errorf("private room by code: %d %v", status, info)
	}
	if status, _ := request(t, "GET", srv.URL+"/api/rooms", "", ""); status != http.StatusUnauthorized {
		t.Errorf("rooms without login: %d, want 401", status)
	}
}
