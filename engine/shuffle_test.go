package engine

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func testRNG(seed byte) *rand.Rand {
	return rand.New(rand.NewChaCha8([32]byte{seed}))
}

func sortedCopy(d Deck) Deck {
	c := slices.Clone(d)
	slices.SortFunc(c, func(a, b Card) int {
		if a.Suit != b.Suit {
			return int(a.Suit) - int(b.Suit)
		}
		return int(a.Rank) - int(b.Rank)
	})
	return c
}

func TestShuffleKeepsSameCards(t *testing.T) {
	d := NewDeck()
	d.Shuffle(testRNG(1))
	if !slices.Equal(sortedCopy(d), NewDeck()) {
		t.Error("shuffled deck must contain exactly the same 52 cards")
	}
}

func TestShuffleChangesOrder(t *testing.T) {
	d := NewDeck()
	d.Shuffle(testRNG(1))
	if slices.Equal(d, NewDeck()) {
		t.Error("shuffle left the deck in original order")
	}
}

func TestShuffleSameSeedSameOrder(t *testing.T) {
	a, b := NewDeck(), NewDeck()
	a.Shuffle(testRNG(7))
	b.Shuffle(testRNG(7))
	if !slices.Equal(a, b) {
		t.Error("same seed must give the same order")
	}
}

// Каждая из 6 перестановок трёх карт должна выпадать примерно в 1/6 случаев.
// Ловит типичную ошибку, когда j берут из [0, n) вместо [0, i].
func TestShuffleIsUniform(t *testing.T) {
	const runs = 60000
	rng := testRNG(42)
	counts := map[string]int{}
	for range runs {
		d := Deck{{Two, Clubs}, {Three, Clubs}, {Four, Clubs}}
		d.Shuffle(rng)
		counts[d[0].String()+d[1].String()+d[2].String()]++
	}
	if len(counts) != 6 {
		t.Fatalf("got %d distinct permutations, want 6", len(counts))
	}
	want := runs / 6
	for perm, n := range counts {
		if n < want*95/100 || n > want*105/100 {
			t.Errorf("permutation %s appeared %d times, want about %d", perm, n, want)
		}
	}
}

func TestNewRNGDiffersEachCall(t *testing.T) {
	a, b := NewDeck(), NewDeck()
	a.Shuffle(NewRNG())
	b.Shuffle(NewRNG())
	if slices.Equal(a, b) {
		t.Error("two NewRNG generators produced the same shuffle")
	}
}
