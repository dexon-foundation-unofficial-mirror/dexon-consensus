package common

import (
	"math/rand"
)

// NewRandomHash returns a random Hash-like value.
func NewRandomHash() Hash {
	x := Hash{}
	for i := 0; i < HashLength; i++ {
		x[i] = byte(rand.Int() % 256)
	}
	return x
}
