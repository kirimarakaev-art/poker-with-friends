package engine

import "testing"

func TestNewDeckSize(t *testing.T) {
	if got := len(NewDeck()); got != 52 {
		t.Errorf("len(NewDeck()) = %d, want 52", got)
	}
}

func TestNewDeckUniqueAndValid(t *testing.T) {
	seen := map[Card]bool{}
	for _, c := range NewDeck() {
		if c.Rank.String() == "?" || c.Suit.String() == "?" {
			t.Errorf("invalid card in deck: %+v", c)
		}
		if seen[c] {
			t.Errorf("duplicate card in deck: %v", c)
		}
		seen[c] = true
	}
}

func TestNewDeckThirteenPerSuit(t *testing.T) {
	perSuit := map[Suit]int{}
	for _, c := range NewDeck() {
		perSuit[c.Suit]++
	}
	for _, s := range []Suit{Clubs, Diamonds, Hearts, Spades} {
		if perSuit[s] != 13 {
			t.Errorf("suit %v has %d cards, want 13", s, perSuit[s])
		}
	}
}

func TestNewDeckIsIndependent(t *testing.T) {
	a := NewDeck()
	b := NewDeck()
	a[0] = Card{}
	if b[0] == (Card{}) {
		t.Error("NewDeck must return a fresh deck each call")
	}
}
