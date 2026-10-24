package server

import (
	"crypto/rand"
	"errors"
	"log"
	mrand "math/rand/v2"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/engine"
)

var (
	errTableFull   = errors.New("table is full")
	errUnknownSeat = errors.New("unknown player token")
	errNotInHand   = errors.New("you are not in this hand")
)

type tableConfig struct {
	game          engine.Config
	stack         int
	turnTimeout   time.Duration
	nextHandDelay time.Duration
}

type player struct {
	name     string
	token    string
	stack    int
	gameSeat int // seat in the current engine.Game, -1 if not in it
	client   *client
}

// table runs one poker table. All its state is owned by the goroutine
// started in newTable; other goroutines change it only through do.
type table struct {
	cfg  tableConfig
	cmds chan func()
	rng  *mrand.Rand

	players     []*player
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
	t := &table{cfg: cfg, cmds: make(chan func()), rng: engine.NewRNG(), button: -1}
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
		if len(t.players) == engine.MaxPlayers {
			err = errTableFull
			return
		}
		token = rand.Text()
		seat = len(t.players)
		t.players = append(t.players, &player{name: name, token: token, stack: t.cfg.stack, gameSeat: -1})
		t.maybeStartHand()
		t.broadcast()
	})
	return seat, token, err
}

func (t *table) findPlayer(token string) (int, *player) {
	for i, p := range t.players {
		if p.token == token {
			return i, p
		}
	}
	return -1, nil
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
			p.client.close()
		}
		p.client = c
		t.broadcast()
	})
	return err
}

func (t *table) detach(token string, c *client) {
	t.do(func() {
		if _, p := t.findPlayer(token); p != nil && p.client == c {
			p.client = nil
			t.broadcast()
		}
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
	t.nextPending = true
	t.later(t.cfg.nextHandDelay, func() {
		t.nextPending = false
		t.startHand()
	})
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
		if p.client != nil {
			p.client.send(serverMsg{Type: "state", State: t.view(i)})
		}
	}
}

func (t *table) sendAll(msg serverMsg) {
	for _, p := range t.players {
		if p.client != nil {
			p.client.send(msg)
		}
	}
}
