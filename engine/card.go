package engine

type Suit uint8

const (
	Clubs Suit = iota + 1
	Diamonds
	Hearts
	Spades
)

func (s Suit) String() string {
	switch s {
	case Clubs:
		return "♣"
	case Diamonds:
		return "♦"
	case Hearts:
		return "♥"
	case Spades:
		return "♠"
	}
	return "?"
}

type Rank uint8

const (
	Two Rank = iota + 2
	Three
	Four
	Five
	Six
	Seven
	Eight
	Nine
	Ten
	Jack
	Queen
	King
	Ace
)

const rankSymbols = "23456789TJQKA"

func (r Rank) String() string {
	if r < Two || r > Ace {
		return "?"
	}
	return string(rankSymbols[r-Two])
}

type Card struct {
	Rank Rank
	Suit Suit
}

func (c Card) String() string {
	return c.Rank.String() + c.Suit.String()
}
