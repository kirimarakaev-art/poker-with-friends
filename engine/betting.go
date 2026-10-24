package engine

import "fmt"

type Action uint8

const (
	Fold Action = iota + 1
	Check
	Call
	Bet
	Raise
	AllIn
)

var actionNames = [...]string{
	Fold:  "Fold",
	Check: "Check",
	Call:  "Call",
	Bet:   "Bet",
	Raise: "Raise",
	AllIn: "All-in",
}

func (a Action) String() string {
	if a < Fold || a > AllIn {
		return "?"
	}
	return actionNames[a]
}

// Options describes what the seat to act may do.
// Bet and raise amounts are the seat's total bet on this street.
type Options struct {
	CanCheck   bool
	CallAmount int
	CanRaise   bool
	MinRaiseTo int
	MaxRaiseTo int
}

func (g *Game) Options() (Options, error) {
	if g.Street == Showdown {
		return Options{}, ErrHandOver
	}
	s := &g.Seats[g.ToAct]
	maxTo := s.Bet + s.Stack
	return Options{
		CanCheck:   s.Bet == g.CurrentBet,
		CallAmount: min(g.CurrentBet-s.Bet, s.Stack),
		CanRaise:   s.canRaise && maxTo > g.CurrentBet && g.othersCanAct(g.ToAct),
		MinRaiseTo: min(g.CurrentBet+g.MinRaise, maxTo),
		MaxRaiseTo: maxTo,
	}, nil
}

// Act applies an action of the seat to act. amount is used only for Bet
// and Raise and is the seat's total bet on this street after the action.
func (g *Game) Act(seat int, a Action, amount int) error {
	if g.Street == Showdown {
		return ErrHandOver
	}
	if seat != g.ToAct {
		return fmt.Errorf("seat %d, waiting for seat %d: %w", seat, g.ToAct, ErrNotYourTurn)
	}
	if err := g.apply(seat, a, amount); err != nil {
		return fmt.Errorf("seat %d %v: %w", seat, a, err)
	}
	return g.advance()
}

func (g *Game) apply(seat int, a Action, amount int) error {
	s := &g.Seats[seat]
	switch a {
	case Fold:
		s.Folded = true
	case Check:
		if s.Bet != g.CurrentBet {
			return ErrIllegalAction
		}
	case Call:
		if s.Bet == g.CurrentBet {
			return ErrIllegalAction
		}
		g.put(seat, g.CurrentBet-s.Bet)
	case Bet, Raise:
		if (a == Bet) != (g.CurrentBet == 0) {
			return ErrIllegalAction
		}
		return g.raiseTo(seat, amount)
	case AllIn:
		if s.Bet+s.Stack <= g.CurrentBet {
			g.put(seat, s.Stack)
			break
		}
		return g.raiseTo(seat, s.Bet+s.Stack)
	default:
		return ErrIllegalAction
	}
	s.needsAction, s.canRaise = false, false
	return nil
}

// othersCanAct reports whether anyone besides seat could answer a raise.
func (g *Game) othersCanAct(seat int) bool {
	for i := range g.Seats {
		if i != seat && g.Seats[i].canAct() {
			return true
		}
	}
	return false
}

// raiseTo makes the seat's street bet equal to `to`. A raise smaller than
// the minimum is allowed only as an all-in and does not reopen betting
// for seats that have already acted.
func (g *Game) raiseTo(seat, to int) error {
	s := &g.Seats[seat]
	maxTo := s.Bet + s.Stack
	switch {
	case !s.canRaise || !g.othersCanAct(seat):
		return ErrIllegalAction
	case to <= g.CurrentBet || to > maxTo:
		return ErrBadAmount
	case to < g.CurrentBet+g.MinRaise && to != maxTo:
		return ErrBadAmount
	}
	increment := to - g.CurrentBet
	full := increment >= g.MinRaise
	if full {
		g.MinRaise = increment
	}
	g.CurrentBet = to
	g.put(seat, to-s.Bet)
	s.needsAction, s.canRaise = false, false
	for i := range g.Seats {
		o := &g.Seats[i]
		if i == seat || !o.canAct() {
			continue
		}
		o.needsAction = true
		if full {
			o.canRaise = true
		}
	}
	return nil
}
