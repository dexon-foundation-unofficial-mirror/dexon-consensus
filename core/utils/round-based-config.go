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

package utils

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// RoundBasedConfig is based config for rounds and provide boundary checking
// for rounds.
type RoundBasedConfig struct {
	roundID          uint64
	roundBeginHeight uint64
	roundEndHeight   uint64
	roundLength      uint64
}

// SetupRoundBasedFields setup round based fields, including round ID, the
// length of rounds.
func (c *RoundBasedConfig) SetupRoundBasedFields(
	roundID uint64, cfg *types.Config) {
	if c.roundLength > 0 {
		panic(fmt.Errorf("duplicated set round based fields: %d",
			c.roundLength))
	}
	c.roundID = roundID
	c.roundLength = cfg.RoundLength
}

// SetRoundBeginHeight gives the beginning height for the initial round provided
// when constructed.
func (c *RoundBasedConfig) SetRoundBeginHeight(begin uint64) {
	if c.roundBeginHeight != 0 {
		panic(fmt.Errorf("duplicated set round begin height: %d",
			c.roundBeginHeight))
	}
	c.roundBeginHeight = begin
	c.roundEndHeight = begin + c.roundLength
}

// IsLastBlock checks if a block is the last block of this round.
func (c *RoundBasedConfig) IsLastBlock(b *types.Block) bool {
	if b.Position.Round != c.roundID {
		panic(fmt.Errorf("attempt to compare by different round: %s, %d",
			b, c.roundID))
	}
	return b.Position.Height+1 == c.roundEndHeight
}

// ExtendLength extends round ending height by the length of current round.
func (c *RoundBasedConfig) ExtendLength() {
	c.roundEndHeight += c.roundLength
}

// Contains checks if a block height is in this round.
func (c *RoundBasedConfig) Contains(h uint64) bool {
	return c.roundBeginHeight <= h && c.roundEndHeight > h
}

// RoundID returns the round ID of this config.
func (c *RoundBasedConfig) RoundID() uint64 {
	if c.roundLength == 0 {
		panic(fmt.Errorf("config is not initialized: %d", c.roundID))
	}
	return c.roundID
}

// RoundEndHeight returns next checkpoint to varify if this round is ended.
func (c *RoundBasedConfig) RoundEndHeight() uint64 {
	if c.roundLength == 0 {
		panic(fmt.Errorf("config is not initialized: %d", c.roundID))
	}
	return c.roundEndHeight
}

// AppendTo a config from previous round.
func (c *RoundBasedConfig) AppendTo(other RoundBasedConfig) {
	if c.roundID != other.roundID+1 {
		panic(fmt.Errorf("round IDs of configs not continuous: %d %d",
			c.roundID, other.roundID))
	}
	c.SetRoundBeginHeight(other.roundEndHeight)
}

// LastPeriodBeginHeight returns the begin height of last period. For example,
// if a round is extended twice, then the return from this method is:
//
//    begin + 2 * roundLength - roundLength
//
func (c *RoundBasedConfig) LastPeriodBeginHeight() uint64 {
	if c.roundLength == 0 {
		panic(fmt.Errorf("config is not initialized: %d", c.roundID))
	}
	return c.roundEndHeight - c.roundLength
}
