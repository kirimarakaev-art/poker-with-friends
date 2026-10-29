package server

import "github.com/kirimarakaev-art/poker-with-friends/engine"

// view builds the state that the player at seat may see. Other players'
// hole cards are included only at a showdown with two or more hands left.
func (t *table) view(seat int) *stateView {
	v := &stateView{
		Hand:    t.hand,
		Street:  "Waiting",
		Board:   []string{},
		Button:  t.button,
		ToAct:   -1,
		You:     seat,
		Players: make([]*playerView, len(t.players)),
	}
	for i, p := range t.players {
		if p != nil {
			v.Players[i] = &playerView{Seat: i, Name: p.name, Stack: p.stack, Connected: p.client != nil}
		}
	}
	if !t.seated(t.button) {
		v.Button = -1
	}
	if t.game == nil {
		return v
	}
	g := t.game
	v.Street = g.Street.String()
	v.Board = cardStrings(g.Board)
	v.CurrentBet = g.CurrentBet
	reveal := g.Street == engine.Showdown && inHand(g) > 1
	for gs, i := range t.inGame {
		s := g.Seats[gs]
		v.Pot += s.Total
		if !t.seated(i) {
			continue // left when the hand ended
		}
		pv := v.Players[i]
		pv.InHand, pv.Bet, pv.Folded, pv.AllIn = true, s.Bet, s.Folded, s.AllIn
		if t.payouts != nil {
			pv.Won = t.payouts[gs]
		} else {
			pv.Stack = s.Stack
		}
		if i == seat || (reveal && !s.Folded) {
			pv.Cards = cardStrings(s.Hole)
		}
	}
	if g.ToAct >= 0 {
		v.ToAct = t.inGame[g.ToAct]
		v.Deadline = t.deadline.UnixMilli()
		if v.ToAct == seat {
			v.Options = optionsFor(g)
		}
	}
	return v
}

func inHand(g *engine.Game) int {
	n := 0
	for _, s := range g.Seats {
		if !s.Folded {
			n++
		}
	}
	return n
}

func optionsFor(g *engine.Game) *optionsView {
	o, err := g.Options()
	if err != nil {
		return nil
	}
	return &optionsView{
		CanCheck:   o.CanCheck,
		CallAmount: o.CallAmount,
		CanRaise:   o.CanRaise,
		MinRaiseTo: o.MinRaiseTo,
		MaxRaiseTo: o.MaxRaiseTo,
	}
}
