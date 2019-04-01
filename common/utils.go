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

// GenerateRandomBytes generates bytes randomly.
func GenerateRandomBytes() []byte {
	randomness := make([]byte, 32)
	_, err := rand.Read(randomness)
	if err != nil {
		panic(err)
	}
	return randomness
}

// CopyBytes copies byte slice.
func CopyBytes(src []byte) (dst []byte) {
	if len(src) == 0 {
		return
	}
	dst = make([]byte, len(src))
	copy(dst, src)
	return
}
