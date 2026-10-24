package engine

import (
	"errors"
	"fmt"
	"slices"
)

var ErrHandInProgress = errors.New("hand is still in progress")

// Pot is a main or side pot. A pot with one eligible seat is an uncalled
// bet that goes back to that seat.
type Pot struct {
	Amount   int
	Eligible []int
}

// Pots splits all chips put in so far into the main pot and side pots.
// Folded seats' chips stay in the pots, but folded seats are not eligible.
func (g *Game) Pots() []Pot {
	var pots []Pot
	prev := 0
	for _, level := range g.levels() {
		pot := Pot{}
		for i, s := range g.Seats {
			pot.Amount += min(s.Total, level) - min(s.Total, prev)
			if !s.Folded && (s.Total >= level || !s.AllIn) {
				pot.Eligible = append(pot.Eligible, i)
			}
		}
		pots = append(pots, pot)
		prev = level
	}
	if len(pots) > 0 {
		for _, s := range g.Seats {
			pots[len(pots)-1].Amount += max(s.Total-prev, 0)
		}
	}
	return pots
}

// levels returns where pots are cut: every all-in total of a seat still in
// the hand plus the highest total, ascending and without repeats.
func (g *Game) levels() []int {
	var levels []int
	top := 0
	for _, s := range g.Seats {
		if s.Folded {
			continue
		}
		top = max(top, s.Total)
		if s.AllIn && !slices.Contains(levels, s.Total) {
			levels = append(levels, s.Total)
		}
	}
	if top > 0 && !slices.Contains(levels, top) {
		levels = append(levels, top)
	}
	slices.Sort(levels)
	return levels
}

// Payouts returns how many chips each seat wins. Split pots are divided
// evenly; odd chips go to the winners closest to the left of the button.
func (g *Game) Payouts() ([]int, error) {
	if g.Street != Showdown {
		return nil, ErrHandInProgress
	}
	won := make([]int, len(g.Seats))
	for _, pot := range g.Pots() {
		winners, err := g.potWinners(pot)
		if err != nil {
			return nil, err
		}
		share, odd := pot.Amount/len(winners), pot.Amount%len(winners)
		for k, seat := range winners {
			won[seat] += share
			if k < odd {
				won[seat]++
			}
		}
	}
	return won, nil
}

// potWinners returns the pot's winners ordered from the left of the button.
func (g *Game) potWinners(pot Pot) ([]int, error) {
	winners := pot.Eligible
	if len(pot.Eligible) > 1 {
		holes := make([][]Card, len(pot.Eligible))
		for k, seat := range pot.Eligible {
			holes[k] = g.Seats[seat].Hole
		}
		idx, err := Winners(g.Board, holes)
		if err != nil {
			return nil, fmt.Errorf("showdown: %w", err)
		}
		winners = make([]int, len(idx))
		for k, i := range idx {
			winners[k] = pot.Eligible[i]
		}
	}
	n := len(g.Seats)
	byPosition := slices.Clone(winners)
	slices.SortFunc(byPosition, func(a, b int) int {
		return (a-g.Button-1+n)%n - (b-g.Button-1+n)%n
	})
	return byPosition, nil
}
