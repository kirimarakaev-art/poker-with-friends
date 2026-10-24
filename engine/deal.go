package engine

import (
	"errors"
	"fmt"
	"slices"
)

var ErrNotEnoughCards = errors.New("not enough cards in deck")

// Deal removes n cards from the top of the deck and returns them.
// On error the deck is left unchanged.
func (d *Deck) Deal(n int) ([]Card, error) {
	if n < 0 || n > len(*d) {
		return nil, fmt.Errorf("deal %d of %d: %w", n, len(*d), ErrNotEnoughCards)
	}
	hand := slices.Clone((*d)[:n])
	*d = (*d)[n:]
	return hand, nil
}
