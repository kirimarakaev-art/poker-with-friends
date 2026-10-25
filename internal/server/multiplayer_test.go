package server

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/kirimarakaev-art/poker-with-friends/engine"
)

// sidePotSeed deals hand 2 so that the shortest stack wins the main pot and
// the side pots are split three and two ways: payouts [380 110 205 205].
const sidePotSeed = 9

// botResult is what one autonomous player saw.
type botResult struct {
	seat      int
	hand1End  *stateView // showdown of hand 1
	hand2Last *stateView // showdown of hand 2
	err       error
}

// playBot reads states and acts on its own turn: in hand 1 it folds,
// in hand 2 it goes all-in (or calls or checks when it may not raise).
// It returns at the showdown of hand 2. It always drains the connection,
// so the server never drops it as a slow client.
func playBot(c *testConn, seat int, out chan<- botResult) {
	res := botResult{seat: seat}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	acted := map[string]bool{}
	for {
		_, data, err := c.conn.Read(ctx)
		if err != nil {
			res.err = fmt.Errorf("seat %d: %w", seat, err)
			out <- res
			return
		}
		var m serverMsg
		if err := json.Unmarshal(data, &m); err != nil || m.Type != "state" {
			continue
		}
		s := m.State
		switch {
		case s.Hand == 1 && s.Street == "Showdown":
			res.hand1End = s
		case s.Hand == 2 && s.Street == "Showdown":
			res.hand2Last = s
			out <- res
			return
		}
		key := fmt.Sprint(s.Hand, s.Street, s.Pot)
		if s.ToAct != seat || s.Options == nil || acted[key] {
			continue
		}
		acted[key] = true
		action := botAction(s.Hand, s.Options.CanRaise, s.Options.CanCheck)
		if err := c.conn.Write(ctx, websocket.MessageText, []byte(`{"type":"act","action":"`+action+`"}`)); err != nil {
			res.err = fmt.Errorf("seat %d: %w", seat, err)
			out <- res
			return
		}
	}
}

func botAction(hand int, canRaise, canCheck bool) string {
	switch {
	case hand == 1:
		return "fold"
	case canRaise:
		return "allin"
	case canCheck:
		return "check"
	}
	return "call"
}

// TestFourPlayersSidePotsMatchEngine plays a four-way all-in with side pots
// over real WebSockets and checks that every client sees exactly what the
// engine computes for the same deck.
func TestFourPlayersSidePotsMatchEngine(t *testing.T) {
	l := newLobby(Options{NextHandDelay: 50 * time.Millisecond})
	srv := httptest.NewServer(newHandler(l))
	t.Cleanup(srv.Close)

	code := createRoom(t, srv)
	tbl := l.room(code)
	stacks := []int{100, 200, 300, 400}
	tokens := make([]string, len(stacks))
	for i, stack := range stacks {
		tbl.do(func() { tbl.cfg.stack = stack })
		tokens[i] = joinRoom(t, srv, code, fmt.Sprintf("p%d", i))
	}
	// Hand 1 has already started with seats 0 and 1; hand 2 uses this deck.
	tbl.do(func() { tbl.rng = rand.New(rand.NewChaCha8([32]byte{sidePotSeed})) })

	results := make(chan botResult, len(tokens))
	for i, tok := range tokens {
		go playBot(connect(t, srv, code, tok), i, results)
	}
	seen := make([]botResult, len(tokens))
	for range tokens {
		r := <-results
		if r.err != nil {
			t.Fatal(r.err)
		}
		seen[r.seat] = r
	}

	want := replayHand2(t, seen[0].hand1End, seen[0].hand2Last.Button)
	total := 0
	for seat, r := range seen {
		s := r.hand2Last
		for j, p := range s.Players {
			if p.Won != want.won[j] || p.Stack != want.stacks[j] {
				t.Errorf("seat %d sees p%d won %d stack %d, engine says won %d stack %d",
					seat, j, p.Won, p.Stack, want.won[j], want.stacks[j])
			}
			if got := strings.Join(p.Cards, " "); got != want.cards[j] {
				t.Errorf("seat %d sees p%d cards %q, engine dealt %q", seat, j, got, want.cards[j])
			}
		}
		if seat == 0 {
			for _, p := range s.Players {
				total += p.Stack
			}
		}
	}
	if total != 1000 {
		t.Errorf("chips after hand 2 = %d, want 1000", total)
	}
	winners := 0
	for _, w := range want.won {
		if w > 0 {
			winners++
		}
	}
	if want.pots < 3 || winners < 3 || want.won[0] == 0 {
		t.Errorf("scenario has %d pots and payouts %v; want 3+ pots, 3+ winners and the short stack winning", want.pots, want.won)
	}
}

type engineOutcome struct {
	won, stacks []int
	cards       []string
	pots        int
}

// replayHand2 plays hand 2 directly in the engine with the same stacks,
// button, deck and bot decisions.
func replayHand2(t *testing.T, hand1End *stateView, button int) engineOutcome {
	t.Helper()
	if hand1End == nil {
		t.Fatal("no showdown state for hand 1")
	}
	stacks := make([]int, len(hand1End.Players))
	for i, p := range hand1End.Players {
		stacks[i] = p.Stack
	}
	rng := rand.New(rand.NewChaCha8([32]byte{sidePotSeed}))
	g, err := engine.NewGame(engine.Config{SmallBlind: 5, BigBlind: 10}, stacks, button, rng)
	if err != nil {
		t.Fatalf("NewGame: %v", err)
	}
	for g.Street != engine.Showdown {
		o, err := g.Options()
		if err != nil {
			t.Fatal(err)
		}
		a := actionsByName[botAction(2, o.CanRaise, o.CanCheck)]
		if err := g.Act(g.ToAct, a, 0); err != nil {
			t.Fatalf("engine Act(%v): %v", a, err)
		}
	}
	won, err := g.Payouts()
	if err != nil {
		t.Fatal(err)
	}
	out := engineOutcome{won: won, pots: len(g.Pots())}
	t.Logf("hand 2: stacks %v, button %d, pots %+v, payouts %v", stacks, button, g.Pots(), won)
	for i, s := range g.Seats {
		out.stacks = append(out.stacks, s.Stack+won[i])
		out.cards = append(out.cards, strings.Join(cardStrings(s.Hole), " "))
	}
	return out
}
