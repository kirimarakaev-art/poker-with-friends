package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// openTest opens a store in a fresh schema of TEST_DATABASE_URL and drops
// the schema after the test. Without TEST_DATABASE_URL the test is skipped.
func openTest(t *testing.T) *Postgres {
	t.Helper()
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	schema := "test_" + strings.ToLower(rand.Text())
	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if c, err := pgx.Connect(ctx, base); err == nil {
			c.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			c.Close(ctx)
		}
	})

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()

	s, err := Open(ctx, u.String())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(s.Close)
	return s
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestRoomsAndPlayersRoundTrip(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)

	if err := s.CreateRoom(ctx, Room{Code: "AAAAAA", SmallBlind: 5, BigBlind: 10, Stack: 1000}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateRoom(ctx, Room{Code: "BBBBBB", SmallBlind: 1, BigBlind: 2, Stack: 200}); err != nil {
		t.Fatal(err)
	}
	ann := Player{Seat: 0, Name: "Ann", TokenHash: sha256.Sum256([]byte("a")), Stack: 1000}
	bob := Player{Seat: 1, Name: "Боб", TokenHash: sha256.Sum256([]byte("b")), Stack: 1000}
	for _, p := range []Player{ann, bob} {
		if err := s.AddPlayer(ctx, "AAAAAA", p); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []Room{
		{Code: "AAAAAA", SmallBlind: 5, BigBlind: 10, Stack: 1000, Players: []Player{ann, bob}},
		{Code: "BBBBBB", SmallBlind: 1, BigBlind: 2, Stack: 200},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRooms =\n%+v\nwant\n%+v", got, want)
	}
}

func TestDuplicateSeatIsRejected(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)
	if err := s.CreateRoom(ctx, Room{Code: "AAAAAA", SmallBlind: 5, BigBlind: 10, Stack: 1000}); err != nil {
		t.Fatal(err)
	}
	p := Player{Seat: 0, Name: "Ann", Stack: 1000}
	if err := s.AddPlayer(ctx, "AAAAAA", p); err != nil {
		t.Fatal(err)
	}
	if err := s.AddPlayer(ctx, "AAAAAA", p); err == nil {
		t.Error("second player on seat 0 was accepted")
	}
	if err := s.AddPlayer(ctx, "NOROOM", p); err == nil {
		t.Error("player in a missing room was accepted")
	}
}

func TestRemovedSeatCanBeTakenAgain(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)
	if err := s.CreateRoom(ctx, Room{Code: "AAAAAA", SmallBlind: 5, BigBlind: 10, Stack: 1000}); err != nil {
		t.Fatal(err)
	}
	ann := Player{Seat: 0, Name: "Ann", Stack: 1000}
	bob := Player{Seat: 1, Name: "Bob", Stack: 1000}
	for _, p := range []Player{ann, bob} {
		if err := s.AddPlayer(ctx, "AAAAAA", p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RemovePlayer(ctx, "AAAAAA", 0); err != nil {
		t.Fatal(err)
	}
	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got := rooms[0].Players; !reflect.DeepEqual(got, []Player{bob}) {
		t.Errorf("players after removal = %+v", got)
	}
	cat := Player{Seat: 0, Name: "Cat", Stack: 1000}
	if err := s.AddPlayer(ctx, "AAAAAA", cat); err != nil {
		t.Errorf("free seat 0 was not accepted: %v", err)
	}
}

func TestSetStackAndDeleteRoom(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)
	for _, code := range []string{"AAAAAA", "BBBBBB"} {
		if err := s.CreateRoom(ctx, Room{Code: code, SmallBlind: 5, BigBlind: 10, Stack: 1000}); err != nil {
			t.Fatal(err)
		}
		if err := s.AddPlayer(ctx, code, Player{Seat: 0, Name: "Ann", Stack: 1000}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveHand(ctx, "AAAAAA", Hand{Number: 1, Board: []string{}, FinishedAt: time.Now(),
		Seats: []HandSeat{{Seat: 0, Name: "Ann", Bet: 10, Won: 0, Stack: 990}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStack(ctx, "BBBBBB", 0, 5000); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteRoom(ctx, "AAAAAA"); err != nil {
		t.Fatal(err)
	}

	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) != 1 || rooms[0].Code != "BBBBBB" || rooms[0].Players[0].Stack != 5000 {
		t.Errorf("rooms = %+v, want only BBBBBB with Ann at 5000", rooms)
	}
	if hands, err := s.Hands(ctx, "AAAAAA", 10); err != nil || len(hands) != 0 {
		t.Errorf("hands of the deleted room = %+v, %v", hands, err)
	}
}

func TestSaveHandUpdatesStacksAndHistory(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)
	if err := s.CreateRoom(ctx, Room{Code: "AAAAAA", SmallBlind: 5, BigBlind: 10, Stack: 1000}); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Ann", "Bob", "Cid"} {
		if err := s.AddPlayer(ctx, "AAAAAA", Player{Seat: i, Name: name, Stack: 1000}); err != nil {
			t.Fatal(err)
		}
	}

	at := time.Date(2026, 10, 25, 20, 0, 0, 0, time.UTC)
	hand1 := Hand{Number: 1, Board: []string{}, Pot: 15, FinishedAt: at, Seats: []HandSeat{
		{Seat: 0, Name: "Ann", Bet: 5, Won: 0, Stack: 995},
		{Seat: 1, Name: "Bob", Bet: 10, Won: 15, Stack: 1005},
	}}
	hand2 := Hand{Number: 2, Board: []string{"Ah", "Kd", "7c", "7s", "2h"}, Pot: 400, FinishedAt: at.Add(time.Minute), Seats: []HandSeat{
		{Seat: 0, Name: "Ann", Cards: []string{"As", "Ac"}, Bet: 200, Won: 400, Stack: 1195},
		{Seat: 1, Name: "Bob", Cards: []string{"Kh", "Ks"}, Bet: 200, Won: 0, Stack: 805},
		{Seat: 2, Name: "Cid", Bet: 0, Won: 0, Stack: 1000},
	}}
	for _, h := range []Hand{hand1, hand2} {
		if err := s.SaveHand(ctx, "AAAAAA", h); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SaveHand(ctx, "AAAAAA", hand1); err == nil {
		t.Error("hand 1 was saved twice")
	}

	hands, err := s.Hands(ctx, "AAAAAA", 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := range hands {
		hands[i].FinishedAt = hands[i].FinishedAt.UTC()
	}
	if want := []Hand{hand2, hand1}; !reflect.DeepEqual(hands, want) {
		t.Errorf("Hands =\n%+v\nwant\n%+v", hands, want)
	}

	last, err := s.Hands(ctx, "AAAAAA", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(last) != 1 || last[0].Number != 2 {
		t.Errorf("Hands(limit 1) = %+v, want only hand 2", last)
	}
	none, err := s.Hands(ctx, "BBBBBB", 10)
	if err != nil || len(none) != 0 {
		t.Errorf("Hands of an empty room = %v, %v; want none", none, err)
	}

	rooms, err := s.LoadRooms(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if rooms[0].LastHand != 2 {
		t.Errorf("LastHand = %d, want 2", rooms[0].LastHand)
	}
	var stacks []int
	for _, p := range rooms[0].Players {
		stacks = append(stacks, p.Stack)
	}
	if want := []int{1195, 805, 1000}; !reflect.DeepEqual(stacks, want) {
		t.Errorf("stacks = %v, want %v", stacks, want)
	}
}

func TestOpenTwiceKeepsData(t *testing.T) {
	s := openTest(t)
	ctx := testCtx(t)
	if err := s.CreateRoom(ctx, Room{Code: "AAAAAA", SmallBlind: 5, BigBlind: 10, Stack: 1000}); err != nil {
		t.Fatal(err)
	}
	// Migrations that are already applied must not run again.
	if err := migrate(ctx, s.pool); err != nil {
		t.Fatal(err)
	}
	rooms, err := s.LoadRooms(ctx)
	if err != nil || len(rooms) != 1 {
		t.Errorf("LoadRooms after second migrate = %v, %v", rooms, err)
	}
}
