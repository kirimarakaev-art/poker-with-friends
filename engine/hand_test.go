package engine

import (
	"errors"
	"strings"
	"testing"
)

// cards parses "As Kd Tc" into cards: rank from rankSymbols, suit from "cdhs".
func cards(t *testing.T, s string) []Card {
	t.Helper()
	var out []Card
	for _, f := range strings.Fields(s) {
		if len(f) != 2 {
			t.Fatalf("bad card %q", f)
		}
		r := strings.IndexByte(rankSymbols, f[0])
		su := strings.IndexByte("cdhs", f[1])
		if r < 0 || su < 0 {
			t.Fatalf("bad card %q", f)
		}
		out = append(out, Card{Rank: Two + Rank(r), Suit: Clubs + Suit(su)})
	}
	return out
}

func TestEvaluate(t *testing.T) {
	tests := []struct {
		name  string
		hand  string
		cat   Category
		ranks []Rank
	}{
		{"royal flush", "As Ks Qs Js Ts 2c 3d", StraightFlush, []Rank{Ace}},
		{"steel wheel", "As 2s 3s 4s 5s Kd Kh", StraightFlush, []Rank{Five}},
		{"straight flush under off-suit straight", "9s 8h 7h 6h 5h 4h Ts", StraightFlush, []Rank{Eight}},
		{"quads", "Ah Ad Ac As Kd Qh 2c", FourOfAKind, []Rank{Ace, King}},
		{"quads with trips kicker", "9h 9d 9c 9s 5d 5h 5c", FourOfAKind, []Rank{Nine, Five}},
		{"full house from two trips", "Kh Kd Kc 7s 7d 7h 2c", FullHouse, []Rank{King, Seven}},
		{"full house picks higher pair", "5h 5d 5c Qs Qd 3h 3c", FullHouse, []Rank{Five, Queen}},
		{"flush beats straight", "As Ks Qs Js 9s Td 2c", Flush, []Rank{Ace, King, Queen, Jack, Nine}},
		{"six-card flush takes top five", "Ah Jh 9h 6h 4h 2h Kd", Flush, []Rank{Ace, Jack, Nine, Six, Four}},
		{"straight with paired card", "9c 8d 8h 7s 6c 5d 2h", Straight, []Rank{Nine}},
		{"wheel", "Ac 2d 3h 4s 5c Kd Qh", Straight, []Rank{Five}},
		{"six-card run takes highest", "4c 5d 6h 7s 8c 9d Kh", Straight, []Rank{Nine}},
		{"no wrap-around straight", "Qc Kd Ah 2s 3c 7d 9h", HighCard, []Rank{Ace, King, Queen, Nine, Seven}},
		{"trips", "7h 7d 7c Ks 9d 4h 2c", ThreeOfAKind, []Rank{Seven, King, Nine}},
		{"two pair", "Kh Kd 5c 5s Qd 3h 2c", TwoPair, []Rank{King, Five, Queen}},
		{"three pairs use third as kicker", "Ah Ad 9c 9s 4d 4h 2c", TwoPair, []Rank{Ace, Nine, Four}},
		{"one pair", "Jh Jd Ac 8s 6d 4h 2c", OnePair, []Rank{Jack, Ace, Eight, Six}},
		{"high card", "Ah Jd 9c 7s 5d 3h 2c", HighCard, []Rank{Ace, Jack, Nine, Seven, Five}},
		{"five cards", "2h 3d 4c 5s 7d", HighCard, []Rank{Seven, Five, Four, Three, Two}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := HandValue{Category: tt.cat}
			copy(want.Ranks[:], tt.ranks)
			got, err := Evaluate(cards(t, tt.hand))
			if err != nil {
				t.Fatalf("Evaluate(%s) error: %v", tt.hand, err)
			}
			if got != want {
				t.Errorf("Evaluate(%s) = %v %v, want %v %v", tt.hand, got.Category, got.Ranks, want.Category, want.Ranks)
			}
		})
	}
}

func TestEvaluateErrors(t *testing.T) {
	tests := []struct {
		name  string
		cards []Card
	}{
		{"too few", cards(t, "As Ks Qs Js")},
		{"too many", cards(t, "As Ks Qs Js Ts 9s 8s 7s")},
		{"duplicate", cards(t, "As As Qs Js Ts")},
		{"invalid card", append(cards(t, "As Ks Qs Js"), Card{})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Evaluate(tt.cards); !errors.Is(err, ErrInvalidHand) {
				t.Errorf("Evaluate error = %v, want ErrInvalidHand", err)
			}
		})
	}
}

func TestCategoryString(t *testing.T) {
	if got := StraightFlush.String(); got != "Straight Flush" {
		t.Errorf("StraightFlush.String() = %q", got)
	}
	if got := HighCard.String(); got != "High Card" {
		t.Errorf("HighCard.String() = %q", got)
	}
	if got := Category(0).String(); got != "?" {
		t.Errorf("Category(0).String() = %q, want %q", got, "?")
	}
}

// Перебирает все 2 598 960 рук из 5 карт и сверяет частоты с комбинаторикой.
func TestEvaluateAllFiveCardHands(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	want := map[Category]int{
		StraightFlush: 40,
		FourOfAKind:   624,
		FullHouse:     3744,
		Flush:         5108,
		Straight:      10200,
		ThreeOfAKind:  54912,
		TwoPair:       123552,
		OnePair:       1098240,
		HighCard:      1302540,
	}
	deck := NewDeck()
	got := map[Category]int{}
	hand := make([]Card, 5)
	for a := 0; a < 52; a++ {
		for b := a + 1; b < 52; b++ {
			for c := b + 1; c < 52; c++ {
				for d := c + 1; d < 52; d++ {
					for e := d + 1; e < 52; e++ {
						hand[0], hand[1], hand[2], hand[3], hand[4] = deck[a], deck[b], deck[c], deck[d], deck[e]
						v, err := Evaluate(hand)
						if err != nil {
							t.Fatalf("Evaluate(%v) error: %v", hand, err)
						}
						got[v.Category]++
					}
				}
			}
		}
	}
	for cat, n := range want {
		if got[cat] != n {
			t.Errorf("%v: got %d hands, want %d", cat, got[cat], n)
		}
	}
}
