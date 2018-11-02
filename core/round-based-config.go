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
	"time"

	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type roundBasedConfig struct {
	roundID uint64

	// roundBeginTime is the beginning of round, as local time.
	roundBeginTime time.Time
	roundInterval  time.Duration

	// roundEndTime is a cache for begin + interval.
	roundEndTime time.Time
}

func (config *roundBasedConfig) setupRoundBasedFields(
	roundID uint64, cfg *types.Config) {
	config.roundID = roundID
	config.roundInterval = cfg.RoundInterval
}

func (config *roundBasedConfig) setRoundBeginTime(begin time.Time) {
	config.roundBeginTime = begin
	config.roundEndTime = begin.Add(config.roundInterval)
}

// isLastBlock checks if a block is the last block of this round.
func (config *roundBasedConfig) isLastBlock(b *types.Block) bool {
	return b.Position.Round == config.roundID &&
		b.Timestamp.After(config.roundEndTime)
}
