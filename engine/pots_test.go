package engine

import (
	"errors"
	"reflect"
	"slices"
	"testing"
)

// setCards replaces the board and hole cards so the showdown is predictable.
func setCards(t *testing.T, g *Game, board string, holes ...string) {
	t.Helper()
	g.Board = cards(t, board)
	for i, h := range holes {
		g.Seats[i].Hole = cards(t, h)
	}
}

func payouts(t *testing.T, g *Game) []int {
	t.Helper()
	p, err := g.Payouts()
	if err != nil {
		t.Fatalf("Payouts error: %v", err)
	}
	return p
}

func TestPotsWithSidePots(t *testing.T) {
	g := newGame(t, []int{50, 100, 300, 300}, 0)
	act(t, g, 3, AllIn, 0)
	act(t, g, 0, AllIn, 0)
	act(t, g, 1, AllIn, 0)
	act(t, g, 2, Call, 0)
	want := []Pot{
		{Amount: 200, Eligible: []int{0, 1, 2, 3}},
		{Amount: 150, Eligible: []int{1, 2, 3}},
		{Amount: 400, Eligible: []int{2, 3}},
	}
	if got := g.Pots(); !reflect.DeepEqual(got, want) {
		t.Errorf("Pots = %+v, want %+v", got, want)
	}
	setCards(t, g, "2c 7d 9h Js Kc", "Kh Kd", "Ah Ad", "3s 4s", "5d 6d")
	if got := payouts(t, g); !slices.Equal(got, []int{200, 150, 0, 400}) {
		t.Errorf("Payouts = %v, want [200 150 0 400]", got)
	}
}

func TestShortStackWinsOnlyMainPot(t *testing.T) {
	g := newGame(t, []int{50, 100, 300}, 0)
	act(t, g, 0, AllIn, 0)
	act(t, g, 1, AllIn, 0)
	act(t, g, 2, Call, 0)
	setCards(t, g, "2c 7d 9h Js Kc", "Kh Kd", "3s 4s", "Ah Ad")
	if got := payouts(t, g); !slices.Equal(got, []int{150, 0, 100}) {
		t.Errorf("Payouts = %v, want [150 0 100]", got)
	}
}

func TestUncalledBetIsReturned(t *testing.T) {
	g := newGame(t, []int{500, 60}, 0)
	act(t, g, 0, Raise, 200)
	act(t, g, 1, Call, 0)
	want := []Pot{
		{Amount: 120, Eligible: []int{0, 1}},
		{Amount: 140, Eligible: []int{0}},
	}
	if got := g.Pots(); !reflect.DeepEqual(got, want) {
		t.Errorf("Pots = %+v, want %+v", got, want)
	}
	setCards(t, g, "2c 7d 9h Js Kc", "3s 4s", "Ah Ad")
	if got := payouts(t, g); !slices.Equal(got, []int{140, 120}) {
		t.Errorf("Payouts = %v, want [140 120]", got)
	}
}

func TestShortBigBlindExcessReturned(t *testing.T) {
	g := newGame(t, []int{100, 7}, 0)
	act(t, g, 0, Call, 0)
	setCards(t, g, "2c 7d 9h Js Kc", "Ah Ad", "3s 4s")
	if got := payouts(t, g); !slices.Equal(got, []int{17, 0}) {
		t.Errorf("Payouts = %v, want [17 0]", got)
	}
	setCards(t, g, "2c 7d 9h Js Kc", "3s 4s", "Ah Ad")
	if got := payouts(t, g); !slices.Equal(got, []int{3, 14}) {
		t.Errorf("Payouts = %v, want [3 14]", got)
	}
}

func TestEveryoneFoldsToRaiser(t *testing.T) {
	g := newGame(t, []int{100, 100, 100}, 0)
	act(t, g, 0, Raise, 40)
	act(t, g, 1, Fold, 0)
	act(t, g, 2, Fold, 0)
	if got := payouts(t, g); !slices.Equal(got, []int{55, 0, 0}) {
		t.Errorf("Payouts = %v, want [55 0 0]", got)
	}
}

func TestSplitPotOddChip(t *testing.T) {
	g := newGame(t, []int{100, 100, 100}, 0)
	act(t, g, 0, Call, 0)
	act(t, g, 1, Fold, 0) // small blind's 5 stays in the pot
	act(t, g, 2, Check, 0)
	for g.Street != Showdown {
		act(t, g, g.ToAct, Check, 0)
	}
	setCards(t, g, "Ts Js Qs Ks As", "2c 3d", "4h 5c", "6d 7h")
	// 25 chips split two ways; the odd chip goes to the first winner left of the button.
	if got := payouts(t, g); !slices.Equal(got, []int{12, 0, 13}) {
		t.Errorf("Payouts = %v, want [12 0 13]", got)
	}
}

func TestPayoutsBeforeShowdown(t *testing.T) {
	g := newGame(t, []int{100, 100}, 0)
	if _, err := g.Payouts(); !errors.Is(err, ErrHandInProgress) {
		t.Errorf("Payouts error = %v, want ErrHandInProgress", err)
	}
	want := []Pot{{Amount: 15, Eligible: []int{0, 1}}}
	if got := g.Pots(); !reflect.DeepEqual(got, want) {
		t.Errorf("Pots during the hand = %+v, want %+v", got, want)
	}
}

// Случайные раздачи: выплаты всегда равны вложенным фишкам, а сбросившие ничего не получают.
func TestRandomGamesPayouts(t *testing.T) {
	rng := testRNG(11)
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
		for g.Street != Showdown {
			o, err := g.Options()
			if err != nil {
				t.Fatalf("game %d: Options error: %v", game, err)
			}
			a, amount := randomAction(rng.IntN(6), o, g.CurrentBet)
			if err := g.Act(g.ToAct, a, amount); err != nil {
				t.Fatalf("game %d: Act error: %v", game, err)
			}
		}
		p, err := g.Payouts()
		if err != nil {
			t.Fatalf("game %d: Payouts error: %v", game, err)
		}
		paid, put := 0, 0
		for i, s := range g.Seats {
			paid += p[i]
			put += s.Total
			if p[i] < 0 || (s.Folded && p[i] != 0) {
				t.Fatalf("game %d: seat %d folded=%v got %d", game, i, s.Folded, p[i])
			}
		}
		if paid != put {
			t.Fatalf("game %d: paid %d, players put %d", game, paid, put)
		}
	}
}
