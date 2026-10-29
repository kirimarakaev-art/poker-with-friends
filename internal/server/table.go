package server

import (
	"context"
	"crypto/rand"
	"errors"
	"log"
	mrand "math/rand/v2"
	"slices"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/engine"
	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

var (
	errTableFull   = errors.New("table is full")
	errUnknownSeat = errors.New("unknown player token")
	errNotInHand   = errors.New("you are not in this hand")
)

type tableConfig struct {
	code          string
	rec           *recorder
	game          engine.Config
	stack         int
	turnTimeout   time.Duration
	nextHandDelay time.Duration
	awayTimeout   time.Duration
}

type player struct {
	name      string
	tokenHash [32]byte
	stack     int
	gameSeat  int // seat in the current engine.Game, -1 if not in it
	client    *client
	leaving   bool // folds on its turn and gives up the seat when the hand ends
	awaySeq   int  // cancels the away timer
}

// table runs one poker table. All its state is owned by the goroutine
// started in newTable; other goroutines change it only through do.
type table struct {
	cfg  tableConfig
	cmds chan func()
	rng  *mrand.Rand

	players     []*player // by seat; nil is a free seat, the last one is never nil
	game        *engine.Game
	inGame      []int // engine seat -> table seat
	payouts     []int // engine seat -> chips won, set when the hand is over
	button      int   // table seat, -1 before the first hand
	hand        int
	turnSeq     int
	deadline    time.Time
	nextPending bool
}

func newTable(cfg tableConfig) *table {
	return startTable(&table{cfg: cfg, cmds: make(chan func()), rng: engine.NewRNG(), button: -1})
}

// restoreTable rebuilds a saved room: players keep their seats, tokens and
// stacks, and hand numbers go on from the last saved hand. Nobody is
// connected yet, so every player starts the away timer.
func restoreTable(cfg tableConfig, r store.Room) *table {
	t := &table{cfg: cfg, cmds: make(chan func()), rng: engine.NewRNG(), button: -1, hand: r.LastHand}
	for _, p := range r.Players {
		for len(t.players) <= p.Seat {
			t.players = append(t.players, nil)
		}
		t.players[p.Seat] = &player{name: p.Name, tokenHash: p.TokenHash, stack: p.Stack, gameSeat: -1}
	}
	startTable(t)
	t.do(func() {
		for _, p := range t.players {
			if p != nil {
				t.startAway(p)
			}
		}
	})
	return t
}

func startTable(t *table) *table {
	go func() {
		for cmd := range t.cmds {
			cmd()
		}
	}()
	return t
}

// do runs fn on the table goroutine and waits for it to finish.
func (t *table) do(fn func()) {
	done := make(chan struct{})
	t.cmds <- func() {
		fn()
		close(done)
	}
	<-done
}

// later runs fn on the table goroutine after d.
func (t *table) later(d time.Duration, fn func()) {
	time.AfterFunc(d, func() { t.cmds <- fn })
}

func (t *table) join(name string) (seat int, token string, err error) {
	t.do(func() {
		seat = slices.Index(t.players, nil)
		if seat < 0 {
			if len(t.players) == engine.MaxPlayers {
				err = errTableFull
				return
			}
			seat = len(t.players)
			t.players = append(t.players, nil)
		}
		token = rand.Text()
		p := &player{name: name, tokenHash: hashToken(token), stack: t.cfg.stack, gameSeat: -1}
		t.players[seat] = p
		t.startAway(p) // until the player connects
		rec := store.Player{Seat: seat, Name: p.name, TokenHash: p.tokenHash, Stack: p.stack}
		t.cfg.rec.record(func(ctx context.Context, s Store) error {
			return s.AddPlayer(ctx, t.cfg.code, rec)
		})
		t.maybeStartHand()
		t.broadcast()
	})
	return seat, token, err
}

func (t *table) findPlayer(token string) (int, *player) {
	h := hashToken(token)
	for i, p := range t.players {
		if p != nil && p.tokenHash == h {
			return i, p
		}
	}
	return -1, nil
}

// seated reports whether somebody sits at seat.
func (t *table) seated(seat int) bool {
	return seat >= 0 && seat < len(t.players) && t.players[seat] != nil
}

// attach connects c to the player with token, replacing an older connection.
func (t *table) attach(token string, c *client) (err error) {
	t.do(func() {
		_, p := t.findPlayer(token)
		if p == nil {
			err = errUnknownSeat
			return
		}
		if p.client != nil {
			p.client.replaced()
		}
		p.client = c
		p.awaySeq++        // cancels the away timer
		t.maybeStartHand() // a restored table waits for its players
		t.broadcast()
	})
	return err
}

func (t *table) detach(token string, c *client) {
	t.do(func() {
		if _, p := t.findPlayer(token); p != nil && p.client == c {
			p.client = nil
			t.startAway(p)
			t.broadcast()
		}
	})
}

// startAway frees the seat of p if it stays disconnected for awayTimeout.
func (t *table) startAway(p *player) {
	p.awaySeq++
	seq := p.awaySeq
	t.later(t.cfg.awayTimeout, func() {
		if p.awaySeq == seq && p.client == nil {
			t.leaveSeat(p)
		}
	})
}

func (t *table) leave(token string) (err error) {
	t.do(func() {
		_, p := t.findPlayer(token)
		if p == nil {
			err = errUnknownSeat
			return
		}
		t.leaveSeat(p)
	})
	return err
}

// leaveSeat frees the seat of p at once, or, if p plays in the current
// hand, folds on its turn and frees the seat when the hand ends.
func (t *table) leaveSeat(p *player) {
	seat := slices.Index(t.players, p)
	if seat < 0 {
		return // already gone
	}
	if t.handOver() || p.gameSeat < 0 {
		t.removePlayer(seat)
		t.broadcast()
		return
	}
	p.leaving = true
	if t.inGame[t.game.ToAct] == seat {
		t.afterAction() // folds for p now
	}
}

// removePlayer frees seat and disconnects its player.
func (t *table) removePlayer(seat int) {
	p := t.players[seat]
	t.players[seat] = nil
	for len(t.players) > 0 && t.players[len(t.players)-1] == nil {
		t.players = t.players[:len(t.players)-1]
	}
	if p.client != nil {
		p.client.left()
		p.client = nil
	}
	t.cfg.rec.record(func(ctx context.Context, s Store) error {
		return s.RemovePlayer(ctx, t.cfg.code, seat)
	})
}

func (t *table) act(token string, a engine.Action, amount int) (err error) {
	t.do(func() {
		_, p := t.findPlayer(token)
		switch {
		case p == nil:
			err = errUnknownSeat
		case t.game == nil || p.gameSeat < 0:
			err = errNotInHand
		default:
			if err = t.game.Act(p.gameSeat, a, amount); err == nil {
				t.afterAction()
			}
		}
	})
	return err
}

func (t *table) chat(token, text string) {
	t.do(func() {
		if _, p := t.findPlayer(token); p != nil {
			t.sendAll(serverMsg{Type: "chat", From: p.name, Text: text})
		}
	})
}

func (t *table) handOver() bool {
	return t.game == nil || t.game.Street == engine.Showdown
}

func (t *table) maybeStartHand() {
	if t.handOver() && !t.nextPending {
		t.startHand()
	}
}

func (t *table) startHand() {
	var seats []int
	for i, p := range t.players {
		if p == nil {
			continue
		}
		p.gameSeat = -1
		if p.stack > 0 {
			seats = append(seats, i)
		}
	}
	if len(seats) < engine.MinPlayers {
		return
	}
	t.button = t.nextButton(seats)
	stacks := make([]int, len(seats))
	gameButton := 0
	for gs, i := range seats {
		stacks[gs] = t.players[i].stack
		t.players[i].gameSeat = gs
		if i == t.button {
			gameButton = gs
		}
	}
	g, err := engine.NewGame(t.cfg.game, stacks, gameButton, t.rng)
	if err != nil {
		log.Printf("table: new hand: %v", err)
		return
	}
	t.game, t.inGame, t.payouts = g, seats, nil
	t.hand++
	t.afterAction()
}

// nextButton returns the first seat with chips after the current button.
func (t *table) nextButton(seats []int) int {
	for _, i := range seats {
		if i > t.button {
			return i
		}
	}
	return seats[0]
}

func (t *table) afterAction() {
	t.foldLeavers()
	if t.game.Street == engine.Showdown {
		t.finishHand()
	} else {
		t.startTurnTimer()
	}
	t.broadcast()
}

func (t *table) finishHand() {
	t.turnSeq++ // cancels the turn timer
	payouts, err := t.game.Payouts()
	if err != nil {
		log.Printf("table: payouts: %v", err)
		payouts = make([]int, len(t.inGame))
	}
	for gs, i := range t.inGame {
		t.players[i].stack = t.game.Seats[gs].Stack + payouts[gs]
	}
	t.payouts = payouts
	h := t.handRecord()
	t.cfg.rec.record(func(ctx context.Context, s Store) error {
		return s.SaveHand(ctx, t.cfg.code, h)
	})
	for _, i := range t.inGame {
		if t.seated(i) && t.players[i].leaving {
			t.removePlayer(i)
		}
	}
	t.nextPending = true
	t.later(t.cfg.nextHandDelay, func() {
		t.nextPending = false
		t.startHand()
	})
}

// handRecord describes the finished hand for the history. Cards are kept
// only for hands shown at a showdown, as in view.
func (t *table) handRecord() store.Hand {
	g := t.game
	reveal := inHand(g) > 1
	h := store.Hand{Number: t.hand, Board: cardStrings(g.Board), FinishedAt: time.Now()}
	for gs, i := range t.inGame {
		s := g.Seats[gs]
		p := t.players[i]
		hs := store.HandSeat{Seat: i, Name: p.name, Bet: s.Total, Won: t.payouts[gs], Stack: p.stack}
		if reveal && !s.Folded {
			hs.Cards = cardStrings(s.Hole)
		}
		h.Pot += s.Total
		h.Seats = append(h.Seats, hs)
	}
	return h
}

func (t *table) startTurnTimer() {
	t.turnSeq++
	seq := t.turnSeq
	t.deadline = time.Now().Add(t.cfg.turnTimeout)
	t.later(t.cfg.turnTimeout, func() {
		if seq == t.turnSeq {
			t.autoAct()
		}
	})
}

// foldLeavers folds for every player who left the table, as their turns come.
func (t *table) foldLeavers() {
	g := t.game
	for g.Street != engine.Showdown && g.ToAct >= 0 && t.players[t.inGame[g.ToAct]].leaving {
		if err := g.Act(g.ToAct, engine.Fold, 0); err != nil {
			log.Printf("table: fold for leaver: %v", err)
			return
		}
	}
}

// autoAct checks or folds for a player who ran out of time.
func (t *table) autoAct() {
	o, err := t.game.Options()
	if err != nil {
		return
	}
	a := engine.Fold
	if o.CanCheck {
		a = engine.Check
	}
	if err := t.game.Act(t.game.ToAct, a, 0); err != nil {
		log.Printf("table: auto %v: %v", a, err)
		return
	}
	t.afterAction()
}

func (t *table) broadcast() {
	for i, p := range t.players {
		if p != nil && p.client != nil {
			p.client.send(serverMsg{Type: "state", State: t.view(i)})
		}
	}
}

func (t *table) sendAll(msg serverMsg) {
	for _, p := range t.players {
		if p != nil && p.client != nil {
			p.client.send(msg)
		}
	}
}
