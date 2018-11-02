// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package types

import (
	"encoding/binary"
	"math"
	"time"
)

// Config stands for Current Configuration Parameters.
type Config struct {
	// Network related.
	NumChains uint32

	// Lambda related.
	LambdaBA  time.Duration
	LambdaDKG time.Duration

	// Total ordering related.
	K        int
	PhiRatio float32

	// Set related.
	NotarySetSize uint32
	DKGSetSize    uint32

	// Time related.
	RoundInterval    time.Duration
	MinBlockInterval time.Duration
	MaxBlockInterval time.Duration
}

// Clone return a copied configuration.
func (c *Config) Clone() *Config {
	return &Config{
		NumChains:        c.NumChains,
		LambdaBA:         c.LambdaBA,
		LambdaDKG:        c.LambdaDKG,
		K:                c.K,
		PhiRatio:         c.PhiRatio,
		NotarySetSize:    c.NotarySetSize,
		DKGSetSize:       c.DKGSetSize,
		RoundInterval:    c.RoundInterval,
		MinBlockInterval: c.MinBlockInterval,
		MaxBlockInterval: c.MaxBlockInterval,
	}
}

// Bytes returns []byte representation of Config.
func (c *Config) Bytes() []byte {
	binaryNumChains := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryNumChains, c.NumChains)

	binaryLambdaBA := make([]byte, 8)
	binary.LittleEndian.PutUint64(
		binaryLambdaBA, uint64(c.LambdaBA.Nanoseconds()))
	binaryLambdaDKG := make([]byte, 8)
	binary.LittleEndian.PutUint64(
		binaryLambdaDKG, uint64(c.LambdaDKG.Nanoseconds()))

	binaryK := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryK, uint32(c.K))
	binaryPhiRatio := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryPhiRatio, math.Float32bits(c.PhiRatio))

	binaryNotarySetSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryNotarySetSize, c.NotarySetSize)
	binaryDKGSetSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryDKGSetSize, c.DKGSetSize)

	binaryRoundInterval := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRoundInterval,
		uint64(c.RoundInterval.Nanoseconds()))
	binaryMinBlockInterval := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryMinBlockInterval,
		uint64(c.MinBlockInterval.Nanoseconds()))
	binaryMaxBlockInterval := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryMaxBlockInterval,
		uint64(c.MaxBlockInterval.Nanoseconds()))

	enc := make([]byte, 0, 40)
	enc = append(enc, binaryNumChains...)
	enc = append(enc, binaryLambdaBA...)
	enc = append(enc, binaryLambdaDKG...)
	enc = append(enc, binaryK...)
	enc = append(enc, binaryPhiRatio...)
	enc = append(enc, binaryNotarySetSize...)
	enc = append(enc, binaryDKGSetSize...)
	enc = append(enc, binaryRoundInterval...)
	enc = append(enc, binaryMinBlockInterval...)
	enc = append(enc, binaryMaxBlockInterval...)
	return enc
}
