package engine

import (
	crand "crypto/rand"
	"math/rand/v2"
)

// NewRNG returns a ChaCha8 generator seeded from crypto/rand.
// It is not safe for concurrent use: give each table its own.
func NewRNG() *rand.Rand {
	var seed [32]byte
	crand.Read(seed[:]) // never returns an error since Go 1.24
	return rand.New(rand.NewChaCha8(seed))
}

// Shuffle shuffles the deck in place using Fisher–Yates.
func (d Deck) Shuffle(rng *rand.Rand) {
	for i := len(d) - 1; i > 0; i-- {
		j := rng.IntN(i + 1)
		d[i], d[j] = d[j], d[i]
	}
}
