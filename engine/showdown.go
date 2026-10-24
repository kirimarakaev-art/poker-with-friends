package engine

import (
	"cmp"
	"errors"
	"fmt"
)

var ErrNoPlayers = errors.New("no players at showdown")

// Compare returns 1 if h beats o, -1 if o beats h and 0 on a tie.
func (h HandValue) Compare(o HandValue) int {
	if c := cmp.Compare(h.Category, o.Category); c != 0 {
		return c
	}
	for i := range h.Ranks {
		if c := cmp.Compare(h.Ranks[i], o.Ranks[i]); c != 0 {
			return c
		}
	}
	return 0
}

// Winners returns the indexes of the players with the best hand.
// More than one index means the pot is split.
func Winners(board []Card, holes [][]Card) ([]int, error) {
	if len(holes) == 0 {
		return nil, ErrNoPlayers
	}
	if err := checkUnique(board, holes); err != nil {
		return nil, err
	}
	var winners []int
	var best HandValue
	for i, hole := range holes {
		hand := make([]Card, 0, len(board)+len(hole))
		hand = append(append(hand, board...), hole...)
		v, err := Evaluate(hand)
		if err != nil {
			return nil, fmt.Errorf("player %d: %w", i, err)
		}
		switch c := v.Compare(best); {
		case c > 0:
			best, winners = v, []int{i}
		case c == 0:
			winners = append(winners, i)
		}
	}
	return winners, nil
}

func checkUnique(board []Card, holes [][]Card) error {
	seen := map[Card]bool{}
	for _, c := range board {
		seen[c] = true
	}
	for i, hole := range holes {
		for _, c := range hole {
			if seen[c] {
				return fmt.Errorf("player %d: card %v already in play: %w", i, c, ErrInvalidHand)
			}
			seen[c] = true
		}
	}
	return nil
}
