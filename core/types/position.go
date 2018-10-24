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
	"errors"
	"fmt"
)

// ErrComparePositionOnDifferentChains raised when attempting to
// compare two positions with different chain ID.
var ErrComparePositionOnDifferentChains = errors.New(
	"position on different chain")

// Position describes the position in the block lattice of an entity.
type Position struct {
	ChainID uint32 `json:"chain_id"`
	Round   uint64 `json:"round"`
	Height  uint64 `json:"height"`
}

func (pos *Position) String() string {
	return fmt.Sprintf("pos[%d:%d:%d]", pos.Round, pos.ChainID, pos.Height)
}

// Equal checks if two positions are equal, it panics when their chainIDs
// are different.
func (pos *Position) Equal(other *Position) bool {
	if pos.ChainID != other.ChainID {
		panic(ErrComparePositionOnDifferentChains)
	}
	return pos.Round == other.Round && pos.Height == other.Height
}

// Newer checks if one block is newer than another one on the same chain.
// If two blocks on different chain compared by this function, it would panic.
func (pos *Position) Newer(other *Position) bool {
	if pos.ChainID != other.ChainID {
		panic(ErrComparePositionOnDifferentChains)
	}
	return pos.Round > other.Round ||
		(pos.Round == other.Round && pos.Height > other.Height)
}

// Clone a position instance.
func (pos *Position) Clone() *Position {
	return &Position{
		ChainID: pos.ChainID,
		Round:   pos.Round,
		Height:  pos.Height,
	}
}
