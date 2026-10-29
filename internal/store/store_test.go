package store

import (
	"context"
	"crypto/sha256"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

// storeUnderTest is the method list both Memory and Postgres implement.
type storeUnderTest interface {
	CreateUser(ctx context.Context, u NewUser) (User, error)
	UserByName(ctx context.Context, username string) (User, error)
	UserByID(ctx context.Context, id int64) (User, error)
	CreateSession(ctx context.Context, tokenHash [32]byte, userID int64, expires time.Time) error
	SessionUser(ctx context.Context, tokenHash [32]byte, now time.Time) (User, error)
	DeleteSession(ctx context.Context, tokenHash [32]byte) error
	ClaimBonus(ctx context.Context, userID int64, now time.Time) (User, error)
	UserStats(ctx context.Context, userID int64) (Stats, error)
	Users(ctx context.Context) ([]User, error)
	SetBalance(ctx context.Context, userID int64, balance int) (User, error)
	SetAdmin(ctx context.Context, username string, admin bool) error

	CreateRoom(ctx context.Context, r Room) error
	SitDown(ctx context.Context, code string, p Player) (int, error)
	StandUp(ctx context.Context, code string, seat, stack int) error
	SetStack(ctx context.Context, code string, seat, stack int) error
	SaveHand(ctx context.Context, code string, h Hand) error
	DeleteRoom(ctx context.Context, code string) error
	LoadRooms(ctx context.Context) ([]Room, error)
	Hands(ctx context.Context, code string, limit int) ([]Hand, error)
}

var (
	_ storeUnderTest = (*Memory)(nil)
	_ storeUnderTest = (*Postgres)(nil)
)

// runStoreSuite runs the behaviour tests against stores made by open; each
// subtest gets an empty store.
func runStoreSuite(t *testing.T, open func(t *testing.T) storeUnderTest) {
	tests := []struct {
		name string
		fn   func(t *testing.T, s storeUnderTest)
	}{
		{"Register", testRegister},
		{"Lookups", testLookups},
		{"Sessions", testSessions},
		{"Bonus", testBonus},
		{"Stats", testStats},
		{"RoomsRoundTrip", testRoomsRoundTrip},
		{"CreateRoomErrors", testCreateRoomErrors},
		{"SitDown", testSitDown},
		{"SitDownConcurrent", testSitDownConcurrent},
		{"StandUp", testStandUp},
		{"SetStack", testSetStack},
		{"SaveHand", testSaveHand},
		{"DeleteRoom", testDeleteRoom},
		{"ResultsAreCopies", testResultsAreCopies},
		{"AdminUserChanges", testAdminUserChanges},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, open(t))
		})
	}
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func mustUser(t *testing.T, s storeUnderTest, name string) User {
	t.Helper()
	u, err := s.CreateUser(testCtx(t), NewUser{Username: name, PasswordHash: "hash-" + name})
	if err != nil {
		t.Fatalf("CreateUser(%s): %v", name, err)
	}
	return u
}

func mustRoom(t *testing.T, s storeUnderTest, code string, owner User) Room {
	t.Helper()
	r := Room{Code: code, OwnerID: owner.ID, Public: true, SmallBlind: 5, BigBlind: 10, MinBuyIn: 10, MaxBuyIn: 100_000}
	if err := s.CreateRoom(testCtx(t), r); err != nil {
		t.Fatalf("CreateRoom(%s): %v", code, err)
	}
	return r
}

func mustSit(t *testing.T, s storeUnderTest, code string, seat int, u User, stack int) int {
	t.Helper()
	balance, err := s.SitDown(testCtx(t), code, Player{Seat: seat, UserID: u.ID, Name: u.Username, Stack: stack})
	if err != nil {
		t.Fatalf("SitDown(%s, %d, %s): %v", code, seat, u.Username, err)
	}
	return balance
}

func balanceOf(t *testing.T, s storeUnderTest, id int64) int {
	t.Helper()
	u, err := s.UserByID(testCtx(t), id)
	if err != nil {
		t.Fatalf("UserByID(%d): %v", id, err)
	}
	return u.Balance
}

func playersOf(t *testing.T, s storeUnderTest, code string) []Player {
	t.Helper()
	rooms, err := s.LoadRooms(testCtx(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rooms {
		if r.Code == code {
			return r.Players
		}
	}
	t.Fatalf("room %s not loaded", code)
	return nil
}

func testRegister(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	u, err := s.CreateUser(ctx, NewUser{Username: "Kerim", PasswordHash: "h1"})
	if err != nil {
		t.Fatal(err)
	}
	want := User{ID: u.ID, Username: "Kerim", PasswordHash: "h1", Balance: StartBalance}
	if u.ID <= 0 || !reflect.DeepEqual(u, want) {
		t.Errorf("CreateUser = %+v, want %+v with ID > 0", u, want)
	}
	for _, name := range []string{"Kerim", "kerim", "KERIM"} {
		if _, err := s.CreateUser(ctx, NewUser{Username: name, PasswordHash: "h2"}); !errors.Is(err, ErrUsernameTaken) {
			t.Errorf("CreateUser(%s) again: err = %v, want ErrUsernameTaken", name, err)
		}
	}
	if _, err := s.CreateUser(ctx, NewUser{Username: "Керим", PasswordHash: "h3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUser(ctx, NewUser{Username: "кЕРИМ", PasswordHash: "h4"}); !errors.Is(err, ErrUsernameTaken) {
		t.Errorf("Cyrillic username in another case: err = %v, want ErrUsernameTaken", err)
	}
	other := mustUser(t, s, "Ann")
	if other.ID == u.ID {
		t.Error("two users got the same ID")
	}
}

func testLookups(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	u := mustUser(t, s, "Kerim")
	mustUser(t, s, "Ann")
	for _, name := range []string{"Kerim", "kerim", "KERIM"} {
		got, err := s.UserByName(ctx, name)
		if err != nil || !reflect.DeepEqual(got, u) {
			t.Errorf("UserByName(%s) = %+v, %v; want %+v", name, got, err, u)
		}
	}
	got, err := s.UserByID(ctx, u.ID)
	if err != nil || !reflect.DeepEqual(got, u) {
		t.Errorf("UserByID = %+v, %v; want %+v", got, err, u)
	}
	if _, err := s.UserByName(ctx, "nobody"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByName(nobody): err = %v, want ErrNotFound", err)
	}
	if _, err := s.UserByID(ctx, u.ID+1000); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID(missing): err = %v, want ErrNotFound", err)
	}
}

func testSessions(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	u := mustUser(t, s, "Kerim")
	now := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)
	a, b := sha256.Sum256([]byte("a")), sha256.Sum256([]byte("b"))
	if err := s.CreateSession(ctx, a, u.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, b, u.ID, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateSession(ctx, sha256.Sum256([]byte("c")), u.ID+1000, now.Add(time.Hour)); err == nil {
		t.Error("session of a missing user was accepted")
	}

	got, err := s.SessionUser(ctx, a, now)
	if err != nil || !reflect.DeepEqual(got, u) {
		t.Errorf("SessionUser = %+v, %v; want %+v", got, err, u)
	}
	if _, err := s.SessionUser(ctx, sha256.Sum256([]byte("x")), now); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown session: err = %v, want ErrNotFound", err)
	}
	if _, err := s.SessionUser(ctx, a, now.Add(time.Hour)); !errors.Is(err, ErrNotFound) {
		t.Errorf("expired session: err = %v, want ErrNotFound", err)
	}

	if err := s.DeleteSession(ctx, b); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SessionUser(ctx, b, now); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleted session: err = %v, want ErrNotFound", err)
	}
	if err := s.DeleteSession(ctx, b); err != nil {
		t.Errorf("deleting a missing session: %v", err)
	}
}

func testBonus(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	u := mustUser(t, s, "Kerim")
	t0 := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)

	if _, err := s.ClaimBonus(ctx, u.ID, t0); !errors.Is(err, ErrBonusUnavailable) {
		t.Errorf("bonus with a full balance: err = %v, want ErrBonusUnavailable", err)
	}
	if _, err := s.ClaimBonus(ctx, u.ID+1000, t0); !errors.Is(err, ErrNotFound) {
		t.Errorf("bonus of a missing user: err = %v, want ErrNotFound", err)
	}

	mustRoom(t, s, "AAAAAA", u)
	mustRoom(t, s, "BBBBBB", u)
	if b := mustSit(t, s, "AAAAAA", 0, u, StartBalance-BonusBelow+1); b != BonusBelow-1 {
		t.Fatalf("balance after sitting = %d", b)
	}
	got, err := s.ClaimBonus(ctx, u.ID, t0)
	if err != nil {
		t.Fatalf("first bonus: %v", err)
	}
	if got.Balance != BonusBelow-1+BonusAmount || !got.BonusAt.Equal(t0) {
		t.Errorf("after first bonus: balance %d, bonus at %v", got.Balance, got.BonusAt)
	}
	if got.BonusAvailable(t0.Add(BonusEvery)) {
		t.Error("BonusAvailable with a balance above the limit")
	}

	// Spend everything, so only the 24-hour rule stands in the way.
	mustSit(t, s, "BBBBBB", 0, u, got.Balance)
	if _, err := s.ClaimBonus(ctx, u.ID, t0.Add(BonusEvery-time.Second)); !errors.Is(err, ErrBonusUnavailable) {
		t.Errorf("second bonus within 24h: err = %v, want ErrBonusUnavailable", err)
	}
	if b := balanceOf(t, s, u.ID); b != 0 {
		t.Errorf("refused bonus changed the balance to %d", b)
	}
	got, err = s.ClaimBonus(ctx, u.ID, t0.Add(BonusEvery))
	if err != nil {
		t.Fatalf("bonus after 24h: %v", err)
	}
	if got.Balance != BonusAmount || !got.BonusAt.Equal(t0.Add(BonusEvery)) {
		t.Errorf("after second bonus: balance %d, bonus at %v", got.Balance, got.BonusAt)
	}
	stored, err := s.UserByID(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Balance != got.Balance || !stored.BonusAt.Equal(got.BonusAt) {
		t.Errorf("stored user %+v differs from ClaimBonus result %+v", stored, got)
	}
}

func testStats(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann, bob := mustUser(t, s, "Ann"), mustUser(t, s, "Bob")
	if st, err := s.UserStats(ctx, ann.ID); err != nil || st != (Stats{}) {
		t.Errorf("stats of a new user = %+v, %v", st, err)
	}
	mustRoom(t, s, "AAAAAA", ann)
	mustSit(t, s, "AAAAAA", 0, ann, 1000)
	mustSit(t, s, "AAAAAA", 1, bob, 1000)
	at := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)
	hands := []Hand{
		{Number: 1, Board: []string{}, Pot: 15, FinishedAt: at, Seats: []HandSeat{
			{Seat: 0, UserID: ann.ID, Name: "Ann", Bet: 5, Won: 0, Stack: 995},
			{Seat: 1, UserID: bob.ID, Name: "Bob", Bet: 10, Won: 15, Stack: 1005},
		}},
		{Number: 2, Board: []string{}, Pot: 20, FinishedAt: at, Seats: []HandSeat{
			{Seat: 0, UserID: ann.ID, Name: "Ann", Bet: 10, Won: 10, Stack: 995},
			{Seat: 1, UserID: bob.ID, Name: "Bob", Bet: 10, Won: 10, Stack: 1005},
		}},
		{Number: 3, Board: []string{}, Pot: 20, FinishedAt: at, Seats: []HandSeat{
			{Seat: 0, UserID: ann.ID, Name: "Ann", Bet: 10, Won: 20, Stack: 1005},
			{Seat: 1, UserID: bob.ID, Name: "Bob", Bet: 10, Won: 0, Stack: 995},
		}},
	}
	for _, h := range hands {
		if err := s.SaveHand(ctx, "AAAAAA", h); err != nil {
			t.Fatal(err)
		}
	}
	if st, err := s.UserStats(ctx, ann.ID); err != nil || st != (Stats{Hands: 3, HandsWon: 2}) {
		t.Errorf("Ann's stats = %+v, %v; want 3 hands, 2 won", st, err)
	}
	if st, err := s.UserStats(ctx, bob.ID); err != nil || st != (Stats{Hands: 3, HandsWon: 2}) {
		t.Errorf("Bob's stats = %+v, %v; want 3 hands, 2 won", st, err)
	}
}

func testRoomsRoundTrip(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann, bob := mustUser(t, s, "Ann"), mustUser(t, s, "Боб")
	rooms := []Room{
		{Code: "AAAAAA", OwnerID: bob.ID, Public: true, SmallBlind: 5, BigBlind: 10, MinBuyIn: 200, MaxBuyIn: 1000},
		{Code: "BBBBBB", OwnerID: ann.ID, Public: false, SmallBlind: 1, BigBlind: 2, MinBuyIn: 2, MaxBuyIn: 2},
	}
	for _, r := range []Room{rooms[1], rooms[0]} {
		if err := s.CreateRoom(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	mustSit(t, s, "AAAAAA", 3, bob, 700)
	mustSit(t, s, "AAAAAA", 1, ann, 500)

	got, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rooms[0].Owner = "Боб"
	rooms[0].Players = []Player{
		{Seat: 1, UserID: ann.ID, Name: "Ann", Stack: 500},
		{Seat: 3, UserID: bob.ID, Name: "Боб", Stack: 700},
	}
	rooms[1].Owner = "Ann"
	if !reflect.DeepEqual(got, rooms) {
		t.Errorf("LoadRooms =\n%+v\nwant\n%+v", got, rooms)
	}
}

func testCreateRoomErrors(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	u := mustUser(t, s, "Ann")
	mustRoom(t, s, "AAAAAA", u)
	bad := map[string]Room{
		"duplicate code":  {Code: "AAAAAA", OwnerID: u.ID, SmallBlind: 5, BigBlind: 10, MinBuyIn: 10, MaxBuyIn: 10},
		"missing owner":   {Code: "BBBBBB", OwnerID: u.ID + 1000, SmallBlind: 5, BigBlind: 10, MinBuyIn: 10, MaxBuyIn: 10},
		"min below blind": {Code: "CCCCCC", OwnerID: u.ID, SmallBlind: 5, BigBlind: 10, MinBuyIn: 9, MaxBuyIn: 10},
		"max below min":   {Code: "DDDDDD", OwnerID: u.ID, SmallBlind: 5, BigBlind: 10, MinBuyIn: 20, MaxBuyIn: 19},
		"zero blind":      {Code: "EEEEEE", OwnerID: u.ID, SmallBlind: 0, BigBlind: 10, MinBuyIn: 20, MaxBuyIn: 20},
	}
	for name, r := range bad {
		if err := s.CreateRoom(ctx, r); err == nil {
			t.Errorf("%s: room was accepted", name)
		}
	}
	rooms, err := s.LoadRooms(ctx)
	if err != nil || len(rooms) != 1 {
		t.Errorf("rooms = %+v, %v; want only AAAAAA", rooms, err)
	}
}

func testSitDown(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann, bob := mustUser(t, s, "Ann"), mustUser(t, s, "Bob")
	mustRoom(t, s, "AAAAAA", ann)
	mustRoom(t, s, "BBBBBB", ann)

	if b := mustSit(t, s, "AAAAAA", 0, ann, 2500); b != StartBalance-2500 {
		t.Errorf("balance after sitting = %d, want %d", b, StartBalance-2500)
	}
	if b := balanceOf(t, s, ann.ID); b != StartBalance-2500 {
		t.Errorf("stored balance = %d", b)
	}

	if _, err := s.SitDown(ctx, "BBBBBB", Player{Seat: 0, UserID: ann.ID, Name: "Ann", Stack: StartBalance}); !errors.Is(err, ErrNotEnoughChips) {
		t.Errorf("sitting with too few chips: err = %v, want ErrNotEnoughChips", err)
	}
	if _, err := s.SitDown(ctx, "AAAAAA", Player{Seat: 1, UserID: ann.ID, Name: "Ann", Stack: 100}); err == nil {
		t.Error("the same user sat twice at one table")
	}
	if _, err := s.SitDown(ctx, "AAAAAA", Player{Seat: 0, UserID: bob.ID, Name: "Bob", Stack: 100}); err == nil {
		t.Error("a taken seat was accepted")
	}
	if _, err := s.SitDown(ctx, "NOROOM", Player{Seat: 0, UserID: bob.ID, Name: "Bob", Stack: 100}); err == nil {
		t.Error("a missing room was accepted")
	}
	if _, err := s.SitDown(ctx, "AAAAAA", Player{Seat: 5, UserID: bob.ID + 1000, Name: "X", Stack: 100}); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing user: err = %v, want ErrNotFound", err)
	}
	if b := balanceOf(t, s, ann.ID); b != StartBalance-2500 {
		t.Errorf("Ann's balance after failed sits = %d, want %d", b, StartBalance-2500)
	}
	if b := balanceOf(t, s, bob.ID); b != StartBalance {
		t.Errorf("Bob's balance after failed sits = %d, want %d", b, StartBalance)
	}
	if got := playersOf(t, s, "BBBBBB"); len(got) != 0 {
		t.Errorf("players at BBBBBB = %+v, want none", got)
	}

	if b := mustSit(t, s, "BBBBBB", 4, ann, StartBalance-2500); b != 0 {
		t.Errorf("balance after sitting with everything = %d", b)
	}
	want := []Player{{Seat: 0, UserID: ann.ID, Name: "Ann", Stack: 2500}}
	if got := playersOf(t, s, "AAAAAA"); !reflect.DeepEqual(got, want) {
		t.Errorf("players at AAAAAA = %+v, want %+v", got, want)
	}
}

func testSitDownConcurrent(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	u := mustUser(t, s, "Ann")
	const rooms, stack = 8, 2000
	codes := make([]string, rooms)
	for i := range codes {
		codes[i] = "ROOM0" + string(rune('A'+i))
		mustRoom(t, s, codes[i], u)
	}
	var wg sync.WaitGroup
	errs := make([]error, rooms)
	for i, code := range codes {
		wg.Go(func() {
			_, errs[i] = s.SitDown(ctx, code, Player{Seat: 0, UserID: u.ID, Name: "Ann", Stack: stack})
		})
	}
	wg.Wait()
	seated := 0
	for _, err := range errs {
		switch {
		case err == nil:
			seated++
		case !errors.Is(err, ErrNotEnoughChips):
			t.Errorf("SitDown: %v", err)
		}
	}
	if want := StartBalance / stack; seated != want {
		t.Errorf("seated at %d tables, want %d", seated, want)
	}
	if b := balanceOf(t, s, u.ID); b != StartBalance-seated*stack {
		t.Errorf("balance = %d, want %d", b, StartBalance-seated*stack)
	}
}

func testStandUp(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann, bob := mustUser(t, s, "Ann"), mustUser(t, s, "Bob")
	mustRoom(t, s, "AAAAAA", ann)
	mustSit(t, s, "AAAAAA", 0, ann, 1000)
	mustSit(t, s, "AAAAAA", 1, bob, 1000)

	if err := s.StandUp(ctx, "AAAAAA", 0, 1500); err != nil {
		t.Fatal(err)
	}
	if b := balanceOf(t, s, ann.ID); b != StartBalance+500 {
		t.Errorf("balance after standing up = %d, want %d", b, StartBalance+500)
	}
	want := []Player{{Seat: 1, UserID: bob.ID, Name: "Bob", Stack: 1000}}
	if got := playersOf(t, s, "AAAAAA"); !reflect.DeepEqual(got, want) {
		t.Errorf("players = %+v, want %+v", got, want)
	}
	if err := s.StandUp(ctx, "AAAAAA", 0, 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("standing up from a free seat: err = %v, want ErrNotFound", err)
	}
	if err := s.StandUp(ctx, "NOROOM", 1, 100); !errors.Is(err, ErrNotFound) {
		t.Errorf("standing up in a missing room: err = %v, want ErrNotFound", err)
	}
	if b := balanceOf(t, s, ann.ID); b != StartBalance+500 {
		t.Errorf("balance changed by a failed StandUp: %d", b)
	}
	// The seat and the table are free for Ann again.
	mustSit(t, s, "AAAAAA", 0, ann, 100)
}

func testSetStack(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann := mustUser(t, s, "Ann")
	mustRoom(t, s, "AAAAAA", ann)
	mustSit(t, s, "AAAAAA", 0, ann, 1000)
	if err := s.SetStack(ctx, "AAAAAA", 0, 5000); err != nil {
		t.Fatal(err)
	}
	if got := playersOf(t, s, "AAAAAA"); len(got) != 1 || got[0].Stack != 5000 {
		t.Errorf("players = %+v, want Ann at 5000", got)
	}
	if b := balanceOf(t, s, ann.ID); b != StartBalance-1000 {
		t.Errorf("SetStack changed the balance to %d", b)
	}
}

func testSaveHand(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann, bob, cid := mustUser(t, s, "Ann"), mustUser(t, s, "Bob"), mustUser(t, s, "Cid")
	mustRoom(t, s, "AAAAAA", ann)
	mustRoom(t, s, "BBBBBB", ann)
	for i, u := range []User{ann, bob, cid} {
		mustSit(t, s, "AAAAAA", i, u, 1000)
	}

	at := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)
	hand1 := Hand{Number: 1, Board: []string{}, Pot: 15, FinishedAt: at, Seats: []HandSeat{
		{Seat: 0, UserID: ann.ID, Name: "Ann", Bet: 5, Won: 0, Stack: 995},
		{Seat: 1, UserID: bob.ID, Name: "Bob", Bet: 10, Won: 15, Stack: 1005},
	}}
	hand2 := Hand{Number: 2, Board: []string{"Ah", "Kd", "7c", "7s", "2h"}, Pot: 400, FinishedAt: at.Add(time.Minute), Seats: []HandSeat{
		{Seat: 0, UserID: ann.ID, Name: "Ann", Cards: []string{"As", "Ac"}, Bet: 200, Won: 400, Stack: 1195},
		{Seat: 1, UserID: bob.ID, Name: "Bob", Cards: []string{"Kh", "Ks"}, Bet: 200, Won: 0, Stack: 805},
		{Seat: 2, UserID: 0, Name: "Cid", Bet: 0, Won: 0, Stack: 1000},
	}}
	for _, h := range []Hand{hand1, hand2} {
		if err := s.SaveHand(ctx, "AAAAAA", h); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveHand(ctx, "AAAAAA", hand1); err == nil {
		t.Error("hand 1 was saved twice")
	}
	if err := s.SaveHand(ctx, "NOROOM", hand1); err == nil {
		t.Error("hand of a missing room was saved")
	}

	hands, err := s.Hands(ctx, "AAAAAA", 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := range hands {
		if !hands[i].FinishedAt.Equal([]Hand{hand2, hand1}[i].FinishedAt) {
			t.Errorf("hand %d finished at %v", hands[i].Number, hands[i].FinishedAt)
		}
		hands[i].FinishedAt = hands[i].FinishedAt.UTC()
	}
	if want := []Hand{hand2, hand1}; !reflect.DeepEqual(hands, want) {
		t.Errorf("Hands =\n%+v\nwant\n%+v", hands, want)
	}
	last, err := s.Hands(ctx, "AAAAAA", 1)
	if err != nil || len(last) != 1 || last[0].Number != 2 {
		t.Errorf("Hands(limit 1) = %+v, %v; want only hand 2", last, err)
	}
	for _, code := range []string{"BBBBBB", "NOROOM"} {
		if none, err := s.Hands(ctx, code, 10); err != nil || len(none) != 0 {
			t.Errorf("Hands(%s) = %v, %v; want none", code, none, err)
		}
	}

	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rooms[0].LastHand != 2 || rooms[1].LastHand != 0 {
		t.Errorf("LastHand = %d and %d, want 2 and 0", rooms[0].LastHand, rooms[1].LastHand)
	}
	var stacks []int
	for _, p := range rooms[0].Players {
		stacks = append(stacks, p.Stack)
	}
	if want := []int{1195, 805, 1000}; !reflect.DeepEqual(stacks, want) {
		t.Errorf("stacks = %v, want %v", stacks, want)
	}
}

func testDeleteRoom(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann, bob := mustUser(t, s, "Ann"), mustUser(t, s, "Bob")
	mustRoom(t, s, "AAAAAA", ann)
	mustRoom(t, s, "BBBBBB", ann)
	mustSit(t, s, "AAAAAA", 0, ann, 1000)
	mustSit(t, s, "AAAAAA", 1, bob, 1000)
	mustSit(t, s, "BBBBBB", 0, ann, 1000)
	if err := s.SaveHand(ctx, "AAAAAA", Hand{Number: 1, Board: []string{}, Pot: 20, FinishedAt: time.Now(), Seats: []HandSeat{
		{Seat: 0, UserID: ann.ID, Name: "Ann", Bet: 10, Won: 20, Stack: 1010},
		{Seat: 1, UserID: bob.ID, Name: "Bob", Bet: 10, Won: 0, Stack: 990},
	}}); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteRoom(ctx, "AAAAAA"); err != nil {
		t.Fatal(err)
	}
	if b := balanceOf(t, s, ann.ID); b != StartBalance-2000+1010 {
		t.Errorf("Ann's balance = %d, want %d", b, StartBalance-2000+1010)
	}
	if b := balanceOf(t, s, bob.ID); b != StartBalance-1000+990 {
		t.Errorf("Bob's balance = %d, want %d", b, StartBalance-1000+990)
	}
	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Code != "BBBBBB" || len(rooms[0].Players) != 1 {
		t.Errorf("rooms = %+v, want only BBBBBB with Ann", rooms)
	}
	if hands, err := s.Hands(ctx, "AAAAAA", 10); err != nil || len(hands) != 0 {
		t.Errorf("hands of the deleted room = %+v, %v", hands, err)
	}
	if err := s.DeleteRoom(ctx, "AAAAAA"); err != nil {
		t.Errorf("deleting a missing room: %v", err)
	}
	// The code is free again.
	mustRoom(t, s, "AAAAAA", bob)
}

func testResultsAreCopies(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	ann := mustUser(t, s, "Ann")
	mustRoom(t, s, "AAAAAA", ann)
	mustSit(t, s, "AAAAAA", 0, ann, 1000)
	h := Hand{Number: 1, Board: []string{"Ah", "Kd", "7c"}, Pot: 10, FinishedAt: time.Now(), Seats: []HandSeat{
		{Seat: 0, UserID: ann.ID, Name: "Ann", Cards: []string{"As", "Ac"}, Bet: 10, Won: 10, Stack: 1000},
	}}
	if err := s.SaveHand(ctx, "AAAAAA", h); err != nil {
		t.Fatal(err)
	}
	h.Board[0], h.Seats[0].Cards[0] = "XX", "XX"

	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	rooms[0].Players[0].Stack = 1
	hands, err := s.Hands(ctx, "AAAAAA", 1)
	if err != nil {
		t.Fatal(err)
	}
	hands[0].Board[0], hands[0].Seats[0].Cards[0], hands[0].Seats[0].Stack = "YY", "YY", 1

	if got := playersOf(t, s, "AAAAAA"); got[0].Stack != 1000 {
		t.Errorf("stack changed through a returned slice: %d", got[0].Stack)
	}
	hands, err = s.Hands(ctx, "AAAAAA", 1)
	if err != nil {
		t.Fatal(err)
	}
	if hands[0].Board[0] != "Ah" || hands[0].Seats[0].Cards[0] != "As" || hands[0].Seats[0].Stack != 1000 {
		t.Errorf("hand changed through a shared slice: %+v", hands[0])
	}
}

func testAdminUserChanges(t *testing.T, s storeUnderTest) {
	ctx := testCtx(t)
	bob := mustUser(t, s, "bob")
	ann := mustUser(t, s, "Ann")

	users, err := s.Users(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0].ID != ann.ID || users[1].ID != bob.ID {
		t.Errorf("Users = %+v, want Ann then bob (by name, any case)", users)
	}

	u, err := s.SetBalance(ctx, ann.ID, 42)
	if err != nil || u.Balance != 42 || u.ID != ann.ID {
		t.Errorf("SetBalance = %+v, %v", u, err)
	}
	if got, _ := s.UserByID(ctx, ann.ID); got.Balance != 42 {
		t.Errorf("balance after SetBalance = %d", got.Balance)
	}
	if _, err := s.SetBalance(ctx, 999999, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetBalance of a missing user: %v, want ErrNotFound", err)
	}

	if err := s.SetAdmin(ctx, "BOB", true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserByID(ctx, bob.ID); !got.IsAdmin {
		t.Error("bob is not an admin after SetAdmin")
	}
	if err := s.SetAdmin(ctx, "bob", false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.UserByID(ctx, bob.ID); got.IsAdmin {
		t.Error("bob is still an admin")
	}
	if err := s.SetAdmin(ctx, "nobody", true); !errors.Is(err, ErrNotFound) {
		t.Errorf("SetAdmin of a missing user: %v, want ErrNotFound", err)
	}
}
