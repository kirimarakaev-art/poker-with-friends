package engine

import (
	"errors"
	"fmt"
	"slices"
)

type Category uint8

const (
	HighCard Category = iota + 1
	OnePair
	TwoPair
	ThreeOfAKind
	Straight
	Flush
	FullHouse
	FourOfAKind
	StraightFlush
)

var categoryNames = [...]string{
	HighCard:      "High Card",
	OnePair:       "One Pair",
	TwoPair:       "Two Pair",
	ThreeOfAKind:  "Three of a Kind",
	Straight:      "Straight",
	Flush:         "Flush",
	FullHouse:     "Full House",
	FourOfAKind:   "Four of a Kind",
	StraightFlush: "Straight Flush",
}

func (c Category) String() string {
	if c < HighCard || c > StraightFlush {
		return "?"
	}
	return categoryNames[c]
}

// HandValue is the best five-card hand. Ranks holds tiebreakers in order of
// importance; unused slots are zero.
type HandValue struct {
	Category Category
	Ranks    [5]Rank
}

var ErrInvalidHand = errors.New("invalid hand")

// rankMask has bit r set for every rank r present.
type rankMask uint16

const (
	straightBits = rankMask(0b11111)
	wheelBits    = rankMask(1<<Ace | 1<<Two | 1<<Three | 1<<Four | 1<<Five)
)

// Evaluate finds the best five-card hand among 5 to 7 cards.
func Evaluate(cards []Card) (HandValue, error) {
	if err := validateHand(cards); err != nil {
		return HandValue{}, err
	}
	var counts [Ace + 1]int
	var suits [Spades + 1]rankMask
	var all rankMask
	for _, c := range cards {
		counts[c.Rank]++
		suits[c.Suit] |= 1 << c.Rank
		all |= 1 << c.Rank
	}
	flush := flushMask(suits)
	if high, ok := straightHigh(flush); ok {
		return value(StraightFlush, high), nil
	}

	quads, trips, pairs := groupRanks(counts)
	fullPair := fullHousePair(trips, pairs)
	switch {
	case len(quads) > 0:
		return value(FourOfAKind, withKickers(counts, 1, quads[0])...), nil
	case len(trips) > 0 && fullPair != 0:
		return value(FullHouse, trips[0], fullPair), nil
	case flush != 0:
		return value(Flush, flush.ranks()[:5]...), nil
	}
	if high, ok := straightHigh(all); ok {
		return value(Straight, high), nil
	}
	switch {
	case len(trips) > 0:
		return value(ThreeOfAKind, withKickers(counts, 2, trips[0])...), nil
	case len(pairs) > 1:
		return value(TwoPair, withKickers(counts, 1, pairs[0], pairs[1])...), nil
	case len(pairs) == 1:
		return value(OnePair, withKickers(counts, 3, pairs[0])...), nil
	}
	return value(HighCard, withKickers(counts, 5)...), nil
}

func validateHand(cards []Card) error {
	if len(cards) < 5 || len(cards) > 7 {
		return fmt.Errorf("%d cards, need 5 to 7: %w", len(cards), ErrInvalidHand)
	}
	seen := map[Card]bool{}
	for _, c := range cards {
		if c.Rank < Two || c.Rank > Ace || c.Suit < Clubs || c.Suit > Spades {
			return fmt.Errorf("card %+v: %w", c, ErrInvalidHand)
		}
		if seen[c] {
			return fmt.Errorf("duplicate card %v: %w", c, ErrInvalidHand)
		}
		seen[c] = true
	}
	return nil
}

func value(cat Category, ranks ...Rank) HandValue {
	h := HandValue{Category: cat}
	copy(h.Ranks[:], ranks)
	return h
}

// flushMask returns the ranks of the suit with five or more cards, or 0.
func flushMask(suits [Spades + 1]rankMask) rankMask {
	for _, m := range suits {
		if len(m.ranks()) >= 5 {
			return m
		}
	}
	return 0
}

func straightHigh(m rankMask) (Rank, bool) {
	for high := Ace; high >= Six; high-- {
		bits := straightBits << (high - 4)
		if m&bits == bits {
			return high, true
		}
	}
	if m&wheelBits == wheelBits {
		return Five, true
	}
	return 0, false
}

// ranks returns the ranks in m from highest to lowest.
func (m rankMask) ranks() []Rank {
	var out []Rank
	for r := Ace; r >= Two; r-- {
		if m&(1<<r) != 0 {
			out = append(out, r)
		}
	}
	return out
}

// groupRanks returns ranks seen 4, 3 and 2 times, each from highest to lowest.
func groupRanks(counts [Ace + 1]int) (quads, trips, pairs []Rank) {
	for r := Ace; r >= Two; r-- {
		switch counts[r] {
		case 4:
			quads = append(quads, r)
		case 3:
			trips = append(trips, r)
		case 2:
			pairs = append(pairs, r)
		}
	}
	return quads, trips, pairs
}

// fullHousePair returns the best rank to pair with trips[0], or 0.
func fullHousePair(trips, pairs []Rank) Rank {
	var best Rank
	if len(trips) > 1 {
		best = trips[1]
	}
	if len(pairs) > 0 && pairs[0] > best {
		best = pairs[0]
	}
	return best
}

// withKickers returns lead followed by the n highest other ranks present.
func withKickers(counts [Ace + 1]int, n int, lead ...Rank) []Rank {
	out := slices.Clone(lead)
	for r := Ace; r >= Two && len(out) < len(lead)+n; r-- {
		if counts[r] > 0 && !slices.Contains(lead, r) {
			out = append(out, r)
		}
	}
	return out
}
