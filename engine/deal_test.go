package engine

import (
	"errors"
	"slices"
	"testing"
)

func TestDealTakesFromTop(t *testing.T) {
	d := NewDeck()
	want := slices.Clone(d[:2])
	hand, err := d.Deal(2)
	if err != nil {
		t.Fatalf("Deal(2) error: %v", err)
	}
	if !slices.Equal(hand, want) {
		t.Errorf("Deal(2) = %v, want %v", hand, want)
	}
	if len(d) != 50 {
		t.Errorf("deck has %d cards after Deal(2), want 50", len(d))
	}
	if slices.Contains(d, want[0]) || slices.Contains(d, want[1]) {
		t.Error("dealt cards must leave the deck")
	}
}

func TestDealSequence(t *testing.T) {
	d := NewDeck()
	seen := map[Card]bool{}
	for _, n := range []int{2, 2, 1, 3, 1, 1} {
		hand, err := d.Deal(n)
		if err != nil {
			t.Fatalf("Deal(%d) error: %v", n, err)
		}
		if len(hand) != n {
			t.Fatalf("Deal(%d) returned %d cards", n, len(hand))
		}
		for _, c := range hand {
			if seen[c] {
				t.Errorf("card %v dealt twice", c)
			}
			seen[c] = true
		}
	}
	if len(d) != 52-10 {
		t.Errorf("deck has %d cards, want 42", len(d))
	}
}

func TestDealAll(t *testing.T) {
	d := NewDeck()
	hand, err := d.Deal(52)
	if err != nil {
		t.Fatalf("Deal(52) error: %v", err)
	}
	if len(hand) != 52 || len(d) != 0 {
		t.Errorf("got %d dealt and %d left, want 52 and 0", len(hand), len(d))
	}
}

func TestDealErrors(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"too many", 53},
		{"negative", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := NewDeck()
			hand, err := d.Deal(tt.n)
			if !errors.Is(err, ErrNotEnoughCards) {
				t.Errorf("Deal(%d) error = %v, want ErrNotEnoughCards", tt.n, err)
			}
			if hand != nil {
				t.Errorf("Deal(%d) returned cards on error: %v", tt.n, hand)
			}
			if len(d) != 52 {
				t.Errorf("deck changed on error: %d cards left", len(d))
			}
		})
	}
}

func TestDealZero(t *testing.T) {
	d := NewDeck()
	hand, err := d.Deal(0)
	if err != nil || len(hand) != 0 || len(d) != 52 {
		t.Errorf("Deal(0) = %v, %v; deck %d cards", hand, err, len(d))
	}
}

func TestDealHandIsIndependent(t *testing.T) {
	d := NewDeck()
	hand, err := d.Deal(2)
	if err != nil {
		t.Fatalf("Deal(2) error: %v", err)
	}
	top := d[0]
	_ = append(hand, Card{})
	if d[0] != top {
		t.Error("appending to a hand must not overwrite the deck")
	}
}
