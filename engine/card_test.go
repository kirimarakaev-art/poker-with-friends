package engine

import "testing"

func TestCardString(t *testing.T) {
	tests := []struct {
		card Card
		want string
	}{
		{Card{Rank: Ace, Suit: Spades}, "A♠"},
		{Card{Rank: Ten, Suit: Hearts}, "T♥"},
		{Card{Rank: Two, Suit: Clubs}, "2♣"},
		{Card{Rank: King, Suit: Diamonds}, "K♦"},
	}
	for _, tt := range tests {
		if got := tt.card.String(); got != tt.want {
			t.Errorf("Card%+v.String() = %q, want %q", tt.card, got, tt.want)
		}
	}
}

func TestRankOrder(t *testing.T) {
	if !(Two < Three && Ten < Jack && King < Ace) {
		t.Error("ranks must be ordered from Two to Ace")
	}
}

func TestInvalidValues(t *testing.T) {
	if got := Rank(0).String(); got != "?" {
		t.Errorf("Rank(0).String() = %q, want %q", got, "?")
	}
	if got := Suit(99).String(); got != "?" {
		t.Errorf("Suit(99).String() = %q, want %q", got, "?")
	}
}
