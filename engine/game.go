package engine

import (
	"errors"
	"fmt"
	"math/rand/v2"
)

const (
	MinPlayers = 2
	MaxPlayers = 9
)

type Street uint8

const (
	Preflop Street = iota + 1
	Flop
	Turn
	River
	Showdown
)

var streetNames = [...]string{
	Preflop:  "Preflop",
	Flop:     "Flop",
	Turn:     "Turn",
	River:    "River",
	Showdown: "Showdown",
}

func (s Street) String() string {
	if s < Preflop || s > Showdown {
		return "?"
	}
	return streetNames[s]
}

type Config struct {
	SmallBlind int
	BigBlind   int
}

type Seat struct {
	Stack  int
	Bet    int // chips put in on the current street
	Total  int // chips put in during the whole hand
	Hole   []Card
	Folded bool
	AllIn  bool

	needsAction bool
	canRaise    bool
}

// Game is one hand of No-Limit Texas Hold'em, from blinds to showdown.
// Showdown also means the hand is over: everyone else may have folded.
type Game struct {
	Seats      []Seat
	Board      []Card
	Street     Street
	Button     int
	ToAct      int // -1 when nobody has to act
	CurrentBet int
	MinRaise   int

	bigBlind int
	deck     Deck
}

var (
	ErrBadConfig     = errors.New("bad game config")
	ErrHandOver      = errors.New("hand is over")
	ErrNotYourTurn   = errors.New("not your turn")
	ErrIllegalAction = errors.New("illegal action")
	ErrBadAmount     = errors.New("bad amount")
)

// NewGame shuffles, deals hole cards and posts blinds.
func NewGame(cfg Config, stacks []int, button int, rng *rand.Rand) (*Game, error) {
	if err := validateConfig(cfg, stacks, button); err != nil {
		return nil, err
	}
	g := &Game{
		Seats:    make([]Seat, len(stacks)),
		Street:   Preflop,
		Button:   button,
		MinRaise: cfg.BigBlind,
		bigBlind: cfg.BigBlind,
		deck:     NewDeck(),
	}
	g.deck.Shuffle(rng)
	for i, stack := range stacks {
		hole, err := g.deck.Deal(2)
		if err != nil {
			return nil, fmt.Errorf("deal hole cards: %w", err)
		}
		g.Seats[i] = Seat{Stack: stack, Hole: hole, needsAction: true, canRaise: true}
	}

	sb, bb := g.next(button), g.next(g.next(button))
	if len(stacks) == 2 {
		sb, bb = button, g.next(button)
	}
	g.put(sb, cfg.SmallBlind)
	g.put(bb, cfg.BigBlind)
	g.CurrentBet = cfg.BigBlind
	g.ToAct = bb
	return g, g.advance()
}

func validateConfig(cfg Config, stacks []int, button int) error {
	if cfg.SmallBlind <= 0 || cfg.BigBlind < cfg.SmallBlind {
		return fmt.Errorf("blinds %d/%d: %w", cfg.SmallBlind, cfg.BigBlind, ErrBadConfig)
	}
	if len(stacks) < MinPlayers || len(stacks) > MaxPlayers {
		return fmt.Errorf("%d players, need %d to %d: %w", len(stacks), MinPlayers, MaxPlayers, ErrBadConfig)
	}
	for i, s := range stacks {
		if s <= 0 {
			return fmt.Errorf("seat %d has stack %d: %w", i, s, ErrBadConfig)
		}
	}
	if button < 0 || button >= len(stacks) {
		return fmt.Errorf("button %d: %w", button, ErrBadConfig)
	}
	return nil
}

func (g *Game) next(i int) int {
	return (i + 1) % len(g.Seats)
}

// put moves up to amount chips from the seat's stack into its bet.
func (g *Game) put(i, amount int) {
	s := &g.Seats[i]
	amount = min(amount, s.Stack)
	s.Stack -= amount
	s.Bet += amount
	s.Total += amount
	if s.Stack == 0 {
		s.AllIn = true
	}
}

// canAct reports whether the seat still makes decisions in this hand.
func (s *Seat) canAct() bool {
	return !s.Folded && !s.AllIn
}

func (g *Game) inHand() int {
	n := 0
	for i := range g.Seats {
		if !g.Seats[i].Folded {
			n++
		}
	}
	return n
}

// bettingClosed reports whether nobody has to act on this street.
func (g *Game) bettingClosed() bool {
	actors, waiting := 0, false
	last := -1
	for i := range g.Seats {
		if s := &g.Seats[i]; s.canAct() {
			actors++
			last = i
			waiting = waiting || s.needsAction
		}
	}
	switch actors {
	case 0:
		return true
	case 1:
		return g.Seats[last].Bet >= g.maxBetExcept(last)
	}
	return !waiting
}

func (g *Game) maxBetExcept(seat int) int {
	m := 0
	for i := range g.Seats {
		if i != seat && !g.Seats[i].Folded {
			m = max(m, g.Seats[i].Bet)
		}
	}
	return m
}

// advance moves the turn on, dealing new streets while betting is closed.
func (g *Game) advance() error {
	if g.inHand() == 1 {
		g.finish()
		return nil
	}
	for g.bettingClosed() {
		if g.Street == River {
			g.finish()
			return nil
		}
		if err := g.nextStreet(); err != nil {
			return err
		}
	}
	i := g.ToAct
	for range g.Seats {
		i = g.next(i)
		if s := &g.Seats[i]; s.canAct() && s.needsAction {
			g.ToAct = i
			return nil
		}
	}
	return errors.New("engine: betting is open but nobody can act")
}

func (g *Game) nextStreet() error {
	n := 1
	if g.Street == Preflop {
		n = 3
	}
	cards, err := g.deck.Deal(n)
	if err != nil {
		return fmt.Errorf("deal %v: %w", g.Street+1, err)
	}
	g.Board = append(g.Board, cards...)
	g.Street++
	g.CurrentBet = 0
	g.MinRaise = g.bigBlind
	for i := range g.Seats {
		s := &g.Seats[i]
		s.Bet = 0
		s.needsAction = s.canAct()
		s.canRaise = s.canAct()
	}
	g.ToAct = g.Button
	return nil
}

func (g *Game) finish() {
	g.Street = Showdown
	g.ToAct = -1
	for i := range g.Seats {
		g.Seats[i].needsAction = false
	}
}
