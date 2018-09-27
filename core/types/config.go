// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
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
	NumShards uint32
	NumChains uint32

	// Lambda related.
	LambdaBA  time.Duration
	LambdaDKG time.Duration

	// Total ordering related.
	K        int
	PhiRatio float32
}

// Bytes returns []byte representation of Config.
func (c *Config) Bytes() []byte {
	binaryNumShards := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryNumShards, c.NumShards)
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

	enc := make([]byte, 0, 40)
	enc = append(enc, binaryNumShards...)
	enc = append(enc, binaryNumChains...)
	enc = append(enc, binaryLambdaBA...)
	enc = append(enc, binaryLambdaDKG...)
	enc = append(enc, binaryK...)
	enc = append(enc, binaryPhiRatio...)
	return enc
}
