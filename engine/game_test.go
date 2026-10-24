package engine

import (
	"errors"
	"testing"
)

var testCfg = Config{SmallBlind: 5, BigBlind: 10}

func newGame(t *testing.T, stacks []int, button int) *Game {
	t.Helper()
	g, err := NewGame(testCfg, stacks, button, testRNG(9))
	if err != nil {
		t.Fatalf("NewGame error: %v", err)
	}
	return g
}

func act(t *testing.T, g *Game, seat int, a Action, amount int) {
	t.Helper()
	if err := g.Act(seat, a, amount); err != nil {
		t.Fatalf("Act(%d, %v, %d) error: %v", seat, a, amount, err)
	}
}

func chips(g *Game) int {
	n := 0
	for _, s := range g.Seats {
		n += s.Stack + s.Total
	}
	return n
}

func TestNewGameErrors(t *testing.T) {
	tests := []struct {
		name   string
		cfg    Config
		stacks []int
		button int
	}{
		{"one player", testCfg, []int{100}, 0},
		{"ten players", testCfg, make([]int, 10), 0},
		{"empty stack", testCfg, []int{100, 0}, 0},
		{"button out of range", testCfg, []int{100, 100}, 2},
		{"zero small blind", Config{0, 10}, []int{100, 100}, 0},
		{"big blind below small", Config{10, 5}, []int{100, 100}, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewGame(tt.cfg, tt.stacks, tt.button, testRNG(1)); !errors.Is(err, ErrBadConfig) {
				t.Errorf("NewGame error = %v, want ErrBadConfig", err)
			}
		})
	}
}

func TestNewGameDealsAndPostsBlinds(t *testing.T) {
	g := newGame(t, []int{100, 100, 100, 100}, 1)
	seen := map[Card]bool{}
	for i, s := range g.Seats {
		if len(s.Hole) != 2 {
			t.Fatalf("seat %d has %d hole cards", i, len(s.Hole))
		}
		for _, c := range s.Hole {
			if seen[c] {
				t.Errorf("card %v dealt twice", c)
			}
			seen[c] = true
		}
	}
	if g.Seats[2].Bet != 5 || g.Seats[3].Bet != 10 {
		t.Errorf("blinds = %d/%d, want 5/10 on seats 2/3", g.Seats[2].Bet, g.Seats[3].Bet)
	}
	if g.ToAct != 0 || g.Street != Preflop || g.CurrentBet != 10 {
		t.Errorf("ToAct=%d Street=%v CurrentBet=%d, want 0 Preflop 10", g.ToAct, g.Street, g.CurrentBet)
	}
}

func TestHeadsUpOrder(t *testing.T) {
	g := newGame(t, []int{100, 100}, 0)
	if g.Seats[0].Bet != 5 || g.Seats[1].Bet != 10 {
		t.Fatalf("heads-up button must post small blind")
	}
	if g.ToAct != 0 {
		t.Fatalf("preflop ToAct = %d, want button 0", g.ToAct)
	}
	act(t, g, 0, Call, 0)
	act(t, g, 1, Check, 0)
	if g.Street != Flop || g.ToAct != 1 {
		t.Errorf("Street=%v ToAct=%d, want Flop and big blind 1", g.Street, g.ToAct)
	}
}

func TestFullHandOfChecks(t *testing.T) {
	g := newGame(t, []int{100, 100, 100}, 0)
	act(t, g, 0, Call, 0)
	act(t, g, 1, Call, 0)
	act(t, g, 2, Check, 0)
	for _, street := range []Street{Flop, Turn, River} {
		if g.Street != street || g.ToAct != 1 {
			t.Fatalf("Street=%v ToAct=%d, want %v and seat 1", g.Street, g.ToAct, street)
		}
		act(t, g, 1, Check, 0)
		act(t, g, 2, Check, 0)
		act(t, g, 0, Check, 0)
	}
	if g.Street != Showdown || len(g.Board) != 5 || g.ToAct != -1 {
		t.Errorf("Street=%v board=%d ToAct=%d, want Showdown 5 -1", g.Street, len(g.Board), g.ToAct)
	}
	if err := g.Act(1, Check, 0); !errors.Is(err, ErrHandOver) {
		t.Errorf("Act after showdown error = %v, want ErrHandOver", err)
	}
	if _, err := g.Options(); !errors.Is(err, ErrHandOver) {
		t.Errorf("Options after showdown error = %v, want ErrHandOver", err)
	}
}

func TestBigBlindOption(t *testing.T) {
	g := newGame(t, []int{100, 100, 100}, 0)
	act(t, g, 0, Call, 0)
	act(t, g, 1, Call, 0)
	if g.Street != Preflop || g.ToAct != 2 {
		t.Fatalf("big blind must get the option, Street=%v ToAct=%d", g.Street, g.ToAct)
	}
	act(t, g, 2, Raise, 30)
	if g.ToAct != 0 || g.CurrentBet != 30 {
		t.Errorf("ToAct=%d CurrentBet=%d, want 0 and 30", g.ToAct, g.CurrentBet)
	}
}

func TestFoldToOnePlayerEndsHand(t *testing.T) {
	g := newGame(t, []int{100, 100, 100}, 0)
	act(t, g, 0, Raise, 40)
	act(t, g, 1, Fold, 0)
	act(t, g, 2, Fold, 0)
	if g.Street != Showdown || len(g.Board) != 0 {
		t.Errorf("Street=%v board=%d, want Showdown with no board", g.Street, len(g.Board))
	}
}

func TestIllegalActions(t *testing.T) {
	tests := []struct {
		name   string
		seat   int
		action Action
		amount int
		want   error
	}{
		{"wrong seat", 1, Fold, 0, ErrNotYourTurn},
		{"check facing bet", 0, Check, 0, ErrIllegalAction},
		{"bet when bet exists", 0, Bet, 30, ErrIllegalAction},
		{"raise below minimum", 0, Raise, 15, ErrBadAmount},
		{"raise not above bet", 0, Raise, 10, ErrBadAmount},
		{"raise above stack", 0, Raise, 101, ErrBadAmount},
		{"unknown action", 0, Action(0), 0, ErrIllegalAction},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGame(t, []int{100, 100, 100}, 0)
			if err := g.Act(tt.seat, tt.action, tt.amount); !errors.Is(err, tt.want) {
				t.Errorf("Act error = %v, want %v", err, tt.want)
			}
			if g.ToAct != 0 || g.Seats[0].Stack != 100 {
				t.Errorf("state changed after illegal action")
			}
		})
	}
}

func TestPostflopBetAndCallRules(t *testing.T) {
	g := newGame(t, []int{100, 100}, 0)
	act(t, g, 0, Call, 0)
	act(t, g, 1, Check, 0)
	if err := g.Act(1, Call, 0); !errors.Is(err, ErrIllegalAction) {
		t.Errorf("call with nothing to call error = %v, want ErrIllegalAction", err)
	}
	if err := g.Act(1, Raise, 20); !errors.Is(err, ErrIllegalAction) {
		t.Errorf("raise with no bet error = %v, want ErrIllegalAction", err)
	}
	if err := g.Act(1, Bet, 5); !errors.Is(err, ErrBadAmount) {
		t.Errorf("bet below big blind error = %v, want ErrBadAmount", err)
	}
	act(t, g, 1, Bet, 10)
	act(t, g, 0, Call, 0)
	if g.Street != Turn {
		t.Errorf("Street = %v, want Turn", g.Street)
	}
}

func TestMinRaiseFollowsLastRaise(t *testing.T) {
	g := newGame(t, []int{500, 500, 500}, 0)
	act(t, g, 0, Raise, 40) // raise by 30
	o, err := g.Options()
	if err != nil {
		t.Fatal(err)
	}
	if o.MinRaiseTo != 70 || o.CallAmount != 35 || o.MaxRaiseTo != 500 {
		t.Errorf("Options = %+v, want MinRaiseTo 70, CallAmount 35, MaxRaiseTo 500", o)
	}
	if err := g.Act(1, Raise, 69); !errors.Is(err, ErrBadAmount) {
		t.Errorf("raise by less than last raise error = %v, want ErrBadAmount", err)
	}
	act(t, g, 1, Raise, 70)
}

func TestShortAllInDoesNotReopenBetting(t *testing.T) {
	g := newGame(t, []int{1000, 1000, 1000}, 0)
	act(t, g, 0, Call, 0)
	act(t, g, 1, Call, 0)
	act(t, g, 2, Check, 0)
	g.Seats[2].Stack = 140 // flop: seat 2 can only go all-in for 140

	act(t, g, 1, Bet, 100)
	act(t, g, 2, AllIn, 0) // raise by 40, less than 100
	if g.CurrentBet != 140 || g.MinRaise != 100 {
		t.Fatalf("CurrentBet=%d MinRaise=%d, want 140 and 100", g.CurrentBet, g.MinRaise)
	}
	o, err := g.Options()
	if err != nil {
		t.Fatal(err)
	}
	if g.ToAct != 0 || !o.CanRaise {
		t.Fatalf("seat 0 has not acted yet and may raise: ToAct=%d %+v", g.ToAct, o)
	}
	act(t, g, 0, Call, 0)

	o, err = g.Options()
	if err != nil {
		t.Fatal(err)
	}
	if g.ToAct != 1 || o.CanRaise || o.CallAmount != 40 {
		t.Fatalf("seat 1 faces a short all-in: ToAct=%d %+v", g.ToAct, o)
	}
	if err := g.Act(1, Raise, 300); !errors.Is(err, ErrIllegalAction) {
		t.Errorf("re-raise after short all-in error = %v, want ErrIllegalAction", err)
	}
	if err := g.Act(1, AllIn, 0); !errors.Is(err, ErrIllegalAction) {
		t.Errorf("all-in raise after short all-in error = %v, want ErrIllegalAction", err)
	}
	act(t, g, 1, Call, 0)
	if g.Street != Turn {
		t.Errorf("Street = %v, want Turn", g.Street)
	}
}

func TestFullAllInReopensBetting(t *testing.T) {
	g := newGame(t, []int{1000, 1000, 1000}, 0)
	act(t, g, 0, Call, 0)
	act(t, g, 1, Call, 0)
	act(t, g, 2, Check, 0)
	g.Seats[2].Stack = 250

	act(t, g, 1, Bet, 100)
	act(t, g, 2, AllIn, 0) // raise by 150
	act(t, g, 0, Call, 0)
	o, err := g.Options()
	if err != nil {
		t.Fatal(err)
	}
	if !o.CanRaise || o.MinRaiseTo != 400 {
		t.Errorf("full all-in must reopen betting: %+v", o)
	}
	act(t, g, 1, Raise, 400)
}

func TestAllInPreflopRunsOutBoard(t *testing.T) {
	g := newGame(t, []int{100, 300}, 0)
	act(t, g, 0, AllIn, 0)
	act(t, g, 1, Call, 0)
	if g.Street != Showdown || len(g.Board) != 5 {
		t.Errorf("Street=%v board=%d, want Showdown with 5 cards", g.Street, len(g.Board))
	}
	if g.Seats[1].Total != 100 || g.Seats[1].Stack != 200 {
		t.Errorf("caller put %d, stack %d; want 100 and 200", g.Seats[1].Total, g.Seats[1].Stack)
	}
}

func TestCallForLessIsAllIn(t *testing.T) {
	g := newGame(t, []int{500, 60}, 0)
	act(t, g, 0, Raise, 200)
	act(t, g, 1, Call, 0)
	if !g.Seats[1].AllIn || g.Seats[1].Total != 60 {
		t.Errorf("seat 1 AllIn=%v Total=%d, want true 60", g.Seats[1].AllIn, g.Seats[1].Total)
	}
	if g.Street != Showdown {
		t.Errorf("Street = %v, want Showdown", g.Street)
	}
}

func TestShortBlinds(t *testing.T) {
	g := newGame(t, []int{100, 3, 100}, 0)
	if !g.Seats[1].AllIn || g.Seats[1].Bet != 3 {
		t.Fatalf("small blind with 3 chips must be all-in for 3")
	}
	if g.CurrentBet != 10 {
		t.Errorf("CurrentBet = %d, want full big blind 10", g.CurrentBet)
	}

	g = newGame(t, []int{100, 7}, 0) // heads-up, big blind all-in for 7
	o, err := g.Options()
	if err != nil {
		t.Fatal(err)
	}
	if g.ToAct != 0 || o.CanRaise {
		t.Fatalf("button must call or fold against all-in blind: ToAct=%d %+v", g.ToAct, o)
	}
	act(t, g, 0, Call, 0)
	if g.Street != Showdown || len(g.Board) != 5 {
		t.Errorf("Street=%v board=%d, want Showdown 5", g.Street, len(g.Board))
	}
}

func TestPlayerFacingOnlyAllInsMustCall(t *testing.T) {
	g := newGame(t, []int{50, 50, 500}, 0)
	act(t, g, 0, AllIn, 0)
	act(t, g, 1, AllIn, 0)
	if g.ToAct != 2 {
		t.Fatalf("ToAct = %d, want 2", g.ToAct)
	}
	o, err := g.Options()
	if err != nil {
		t.Fatal(err)
	}
	if o.CanRaise || o.CallAmount != 40 {
		t.Errorf("Options = %+v, want call 40 and no raise", o)
	}
	act(t, g, 2, Call, 0)
	if g.Street != Showdown || len(g.Board) != 5 {
		t.Errorf("Street=%v board=%d, want Showdown 5", g.Street, len(g.Board))
	}
}

func TestStringers(t *testing.T) {
	if Flop.String() != "Flop" || Street(0).String() != "?" {
		t.Error("Street.String is wrong")
	}
	if AllIn.String() != "All-in" || Action(0).String() != "?" {
		t.Error("Action.String is wrong")
	}
}

// Играет тысячи случайных раздач только разрешёнными действиями и проверяет,
// что фишки не теряются, раздача всегда заканчивается и карт на столе не больше 5.
func TestRandomGames(t *testing.T) {
	rng := testRNG(5)
	for game := range 3000 {
		n := 2 + rng.IntN(MaxPlayers-1)
		stacks := make([]int, n)
		for i := range stacks {
			stacks[i] = 1 + rng.IntN(400)
		}
		g, err := NewGame(testCfg, stacks, rng.IntN(n), rng)
		if err != nil {
			t.Fatalf("game %d: NewGame error: %v", game, err)
		}
		total := chips(g)
		for steps := 0; g.Street != Showdown; steps++ {
			if steps > 500 {
				t.Fatalf("game %d did not finish", game)
			}
			o, err := g.Options()
			if err != nil {
				t.Fatalf("game %d: Options error: %v", game, err)
			}
			a, amount := randomAction(rng.IntN(6), o, g.CurrentBet)
			if err := g.Act(g.ToAct, a, amount); err != nil {
				t.Fatalf("game %d: Act(%v, %d) with %+v error: %v", game, a, amount, o, err)
			}
			if chips(g) != total {
				t.Fatalf("game %d: chips changed from %d to %d", game, total, chips(g))
			}
		}
		if len(g.Board) > 5 || g.inHand() == 0 {
			t.Fatalf("game %d: board %d, in hand %d", game, len(g.Board), g.inHand())
		}
	}
}

func randomAction(pick int, o Options, currentBet int) (Action, int) {
	switch {
	case pick == 0:
		return Fold, 0
	case pick <= 2 && o.CanCheck:
		return Check, 0
	case pick <= 2:
		return Call, 0
	case pick == 3 && o.CanRaise:
		return AllIn, 0
	case o.CanRaise && currentBet == 0:
		return Bet, o.MinRaiseTo
	case o.CanRaise:
		return Raise, o.MinRaiseTo
	case o.CanCheck:
		return Check, 0
	}
	return Call, 0
}
