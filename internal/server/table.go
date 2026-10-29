package server

import (
	"context"
	"errors"
	"log"
	mrand "math/rand/v2"
	"slices"
	"time"

	"github.com/kirimarakaev-art/poker-with-friends/engine"
	"github.com/kirimarakaev-art/poker-with-friends/internal/store"
)

var (
	errTableFull = errors.New("table is full")
	errNotSeated = errors.New("not seated")
	errJoining   = errors.New("already joining this table")
	errNotInHand = errors.New("you are not in this hand")
	errEmptySeat = errors.New("nobody sits there")
)

type tableConfig struct {
	code          string
	owner         string // username
	ownerID       int64
	created       time.Time // zero for rooms restored after a restart
	public        bool
	minBuyIn      int
	maxBuyIn      int
	rec           *recorder
	game          engine.Config
	turnTimeout   time.Duration
	nextHandDelay time.Duration
	awayTimeout   time.Duration
}

type player struct {
	userID   int64
	name     string
	stack    int
	gameSeat int // seat in the current engine.Game, -1 if not in it
	client   *client
	leaving  bool // folds on its turn and gives up the seat when the hand ends
	awaySeq  int  // cancels the away timer
}

// table runs one poker table. All its state is owned by the goroutine
// started in newTable; other goroutines change it only through do.
type table struct {
	cfg  tableConfig
	cmds chan func()
	rng  *mrand.Rand

	players     []*player     // by seat; nil is a free seat, the last one is never nil
	reserved    map[int]int64 // seat -> user whose buy-in is being saved
	game        *engine.Game
	inGame      []int // engine seat -> table seat
	payouts     []int // engine seat -> chips won, set when the hand is over
	button      int   // table seat, -1 before the first hand
	hand        int
	turnSeq     int
	deadline    time.Time
	nextPending bool
	closed      bool // the admin closed the room: timers and new hands stop
}

func newTable(cfg tableConfig) *table {
	return startTable(&table{cfg: cfg, cmds: make(chan func()), rng: engine.NewRNG(), button: -1, reserved: map[int]int64{}})
}

// restoreTable rebuilds a saved room: players keep their seats and stacks,
// and hand numbers go on from the last saved hand. Nobody is connected
// yet, so every player starts the away timer.
func restoreTable(cfg tableConfig, r store.Room) *table {
	t := &table{cfg: cfg, cmds: make(chan func()), rng: engine.NewRNG(), button: -1, hand: r.LastHand, reserved: map[int]int64{}}
	for _, p := range r.Players {
		for len(t.players) <= p.Seat {
			t.players = append(t.players, nil)
		}
		t.players[p.Seat] = &player{userID: p.UserID, name: p.Name, stack: p.Stack, gameSeat: -1}
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

// reserve holds the first free seat for userID while the buy-in is saved;
// sit or cancelReserve must follow. A player who already sits gets the seat
// with seated set, and nothing is reserved.
func (t *table) reserve(userID int64) (seat int, seated bool, err error) {
	t.do(func() {
		if s, p := t.findPlayer(userID); p != nil {
			seat, seated = s, true
			return
		}
		for _, id := range t.reserved {
			if id == userID {
				err = errJoining
				return
			}
		}
		for seat = 0; seat < engine.MaxPlayers; seat++ {
			_, held := t.reserved[seat]
			if !t.seated(seat) && !held && !t.closed {
				t.reserved[seat] = userID
				return
			}
		}
		err = errTableFull
	})
	return seat, seated, err
}

func (t *table) cancelReserve(seat int) {
	t.do(func() { delete(t.reserved, seat) })
}

// sit seats a player whose buy-in is saved.
func (t *table) sit(seat int, userID int64, name string, stack int) {
	t.do(func() {
		delete(t.reserved, seat)
		for len(t.players) <= seat {
			t.players = append(t.players, nil)
		}
		p := &player{userID: userID, name: name, stack: stack, gameSeat: -1}
		t.players[seat] = p
		t.startAway(p) // until the player connects
		t.maybeStartHand()
		t.broadcast()
	})
}

// info describes the table for the room lists; seated says whether userID sits here.
func (t *table) info(userID int64) (v roomInfo) {
	t.do(func() {
		v = roomInfo{
			Code:       t.cfg.code,
			Owner:      t.cfg.owner,
			Public:     t.cfg.public,
			SmallBlind: t.cfg.game.SmallBlind,
			BigBlind:   t.cfg.game.BigBlind,
			MinBuyIn:   t.cfg.minBuyIn,
			MaxBuyIn:   t.cfg.maxBuyIn,
			MaxPlayers: engine.MaxPlayers,
		}
		for _, p := range t.players {
			if p != nil {
				v.Players++
				v.Seated = v.Seated || p.userID == userID
			}
		}
	})
	return v
}

// activeOf reports whether the table counts against userID's room limit:
// they own it and someone sits there, or it is newer than freshRoom.
func (t *table) activeOf(userID int64, now time.Time) (active bool) {
	if t.cfg.ownerID != userID {
		return false
	}
	t.do(func() {
		active = now.Sub(t.cfg.created) < freshRoom || len(t.reserved) > 0 ||
			slices.ContainsFunc(t.players, func(p *player) bool { return p != nil })
	})
	return active
}

func (t *table) findPlayer(userID int64) (int, *player) {
	for i, p := range t.players {
		if p != nil && p.userID == userID {
			return i, p
		}
	}
	return -1, nil
}

// seated reports whether somebody sits at seat.
func (t *table) seated(seat int) bool {
	return seat >= 0 && seat < len(t.players) && t.players[seat] != nil
}

// attach connects c to the player userID, replacing an older connection.
func (t *table) attach(userID int64, c *client) (err error) {
	t.do(func() {
		_, p := t.findPlayer(userID)
		if p == nil {
			err = errNotSeated
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

func (t *table) detach(userID int64, c *client) {
	t.do(func() {
		if _, p := t.findPlayer(userID); p != nil && p.client == c {
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
		if !t.closed && p.awaySeq == seq && p.client == nil {
			t.leaveSeat(p)
		}
	})
}

func (t *table) leave(userID int64) (err error) {
	t.do(func() {
		_, p := t.findPlayer(userID)
		if p == nil {
			err = errNotSeated
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

// removePlayer frees seat, disconnects its player and returns the stack to
// the player's balance.
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
	stack := p.stack
	t.cfg.rec.record(func(ctx context.Context, s Store) error {
		return s.StandUp(ctx, t.cfg.code, seat, stack)
	})
}

func (t *table) act(userID int64, a engine.Action, amount int) (err error) {
	t.do(func() {
		_, p := t.findPlayer(userID)
		switch {
		case p == nil:
			err = errNotSeated
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

func (t *table) chat(userID int64, text string) {
	t.do(func() {
		if _, p := t.findPlayer(userID); p != nil {
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
	if t.closed {
		return
	}
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
		if !t.seated(i) {
			continue
		}
		if t.players[i].leaving {
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
		hs := store.HandSeat{Seat: i, UserID: p.userID, Name: p.name, Bet: s.Total, Won: t.payouts[gs], Stack: p.stack}
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

// kick frees seat for the admin, as if its player had left.
func (t *table) kick(seat int) (err error) {
	t.do(func() {
		if !t.seated(seat) {
			err = errEmptySeat
			return
		}
		t.leaveSeat(t.players[seat])
	})
	return err
}

// close disconnects everyone for good; the lobby has already forgotten the room.
func (t *table) close() {
	t.do(func() {
		t.closed = true
		t.turnSeq++ // cancels the turn timer
		for _, p := range t.players {
			if p != nil && p.client != nil {
				p.client.closeWith(CloseRoomNotFound, "room closed")
				p.client = nil
			}
		}
	})
}

// adminView describes the table for the admin API.
func (t *table) adminView() (v adminRoomView) {
	t.do(func() {
		v = adminRoomView{
			Code:       t.cfg.code,
			SmallBlind: t.cfg.game.SmallBlind,
			BigBlind:   t.cfg.game.BigBlind,
			Hand:       t.hand,
			Street:     "Waiting",
			Players:    make([]*adminPlayerView, len(t.players)),
		}
		if t.game != nil {
			v.Street = t.game.Street.String()
		}
		for i, p := range t.players {
			if p == nil {
				continue
			}
			pv := &adminPlayerView{Seat: i, Name: p.name, Stack: p.stack, Connected: p.client != nil, Leaving: p.leaving}
			if t.game != nil && p.gameSeat >= 0 && !t.handOver() {
				s := t.game.Seats[p.gameSeat]
				pv.InHand, pv.Stack, pv.Bet = true, s.Stack, s.Total
			}
			v.Players[i] = pv
		}
	})
	return v
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
