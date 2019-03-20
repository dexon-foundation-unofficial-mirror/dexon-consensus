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
	"time"
)

// Config stands for Current Configuration Parameters.
type Config struct {
	// Lambda related.
	LambdaBA  time.Duration
	LambdaDKG time.Duration

	// Set related.
	NotarySetSize uint32

	// Time related.
	RoundLength      uint64
	MinBlockInterval time.Duration
}

// Clone return a copied configuration.
func (c *Config) Clone() *Config {
	return &Config{
		LambdaBA:         c.LambdaBA,
		LambdaDKG:        c.LambdaDKG,
		NotarySetSize:    c.NotarySetSize,
		RoundLength:      c.RoundLength,
		MinBlockInterval: c.MinBlockInterval,
	}
}

// Bytes returns []byte representation of Config.
func (c *Config) Bytes() []byte {
	binaryLambdaBA := make([]byte, 8)
	binary.LittleEndian.PutUint64(
		binaryLambdaBA, uint64(c.LambdaBA.Nanoseconds()))
	binaryLambdaDKG := make([]byte, 8)
	binary.LittleEndian.PutUint64(
		binaryLambdaDKG, uint64(c.LambdaDKG.Nanoseconds()))

	binaryNotarySetSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryNotarySetSize, c.NotarySetSize)

	binaryRoundLength := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRoundLength, c.RoundLength)
	binaryMinBlockInterval := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryMinBlockInterval,
		uint64(c.MinBlockInterval.Nanoseconds()))

	enc := make([]byte, 0, 40)
	enc = append(enc, binaryLambdaBA...)
	enc = append(enc, binaryLambdaDKG...)
	enc = append(enc, binaryNotarySetSize...)
	enc = append(enc, binaryRoundLength...)
	enc = append(enc, binaryMinBlockInterval...)
	return enc
}
