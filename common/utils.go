package common

import (
	"math/rand"
	"time"
)

var random *rand.Rand

func init() {
	random = rand.New(rand.NewSource(time.Now().Unix()))
}

// NewRandomHash returns a random Hash-like value.
func NewRandomHash() Hash {
	x := Hash{}
	for i := 0; i < HashLength; i++ {
		x[i] = byte(random.Int() % 256)
	}
	return x
}
