package engine

type Deck []Card

func NewDeck() Deck {
	deck := make(Deck, 0, 52)
	for s := Clubs; s <= Spades; s++ {
		for r := Two; r <= Ace; r++ {
			deck = append(deck, Card{Rank: r, Suit: s})
		}
	}
	return deck
}
