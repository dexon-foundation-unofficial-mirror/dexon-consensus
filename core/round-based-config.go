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

package core

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type roundBasedConfig struct {
	roundID          uint64
	roundBeginHeight uint64
	roundEndHeight   uint64
	roundInterval    uint64
}

func (config *roundBasedConfig) setupRoundBasedFields(
	roundID uint64, cfg *types.Config) {
	config.roundID = roundID
	config.roundInterval = cfg.RoundInterval
}

func (config *roundBasedConfig) setRoundBeginHeight(begin uint64) {
	config.roundBeginHeight = begin
	config.roundEndHeight = begin + config.roundInterval
}

// isLastBlock checks if a block is the last block of this round.
func (config *roundBasedConfig) isLastBlock(b *types.Block) bool {
	if b.Position.Round != config.roundID {
		panic(fmt.Errorf("attempt to compare by different round: %s, %d",
			b, config.roundID))
	}
	return b.Position.Height+1 == config.roundEndHeight
}
