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
	"errors"
	"time"

	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// consensusTimestamp is for Concensus Timestamp Algorithm.
type consensusTimestamp struct {
	chainTimestamps []time.Time

	// This part keeps configs for each round.
	numChainsOfRounds []uint32
	numChainsBase     uint64

	// dMoment represents the genesis time.
	dMoment time.Time
	// lastTimestamp represents previous assigned consensus timestamp.
	lastTimestamp time.Time
}

var (
	// ErrTimestampNotIncrease would be reported if the timestamp is not strickly
	// increasing on the same chain.
	ErrTimestampNotIncrease = errors.New("timestamp is not increasing")
	// ErrNoRoundConfig for no round config found.
	ErrNoRoundConfig = errors.New("no round config found")
	// ErrConsensusTimestampRewind would be reported if the generated timestamp
	// is rewinded.
	ErrConsensusTimestampRewind = errors.New("consensus timestamp rewind")
)

// newConsensusTimestamp creates timestamper object.
func newConsensusTimestamp(
	dMoment time.Time, round uint64, numChains uint32) *consensusTimestamp {

	ts := make([]time.Time, numChains)
	for i := range ts {
		ts[i] = dMoment
	}
	return &consensusTimestamp{
		numChainsOfRounds: []uint32{numChains},
		numChainsBase:     round,
		dMoment:           dMoment,
		chainTimestamps:   ts,
	}
}

// appendConfig appends a configuration for upcoming round. When you append
// a config for round R, next time you can only append the config for round R+1.
func (ct *consensusTimestamp) appendConfig(
	round uint64, config *types.Config) error {

	if round != uint64(len(ct.numChainsOfRounds))+ct.numChainsBase {
		return ErrRoundNotIncreasing
	}
	ct.numChainsOfRounds = append(ct.numChainsOfRounds, config.NumChains)
	return nil
}

func (ct *consensusTimestamp) resizeChainTimetamps(numChain uint32) {
	l := uint32(len(ct.chainTimestamps))
	if numChain > l {
		for i := l; i < numChain; i++ {
			ct.chainTimestamps = append(ct.chainTimestamps, ct.dMoment)
		}
	} else if numChain < l {
		ct.chainTimestamps = ct.chainTimestamps[:numChain]
	}
}

// ProcessBlocks is the entry function.
func (ct *consensusTimestamp) processBlocks(blocks []*types.Block) (err error) {
	for _, block := range blocks {
		// Rounds might interleave within rounds if no configuration change
		// occurs. And it is limited to one round, that is, round r can only
		// interleave with r-1 and r+1.
		round := block.Position.Round
		if ct.numChainsBase == round || ct.numChainsBase+1 == round {
			// Normal case, no need to modify chainTimestamps.
		} else if ct.numChainsBase+2 == round {
			if len(ct.numChainsOfRounds) < 2 {
				return ErrNoRoundConfig
			}
			ct.numChainsBase++
			ct.numChainsOfRounds = ct.numChainsOfRounds[1:]
			if ct.numChainsOfRounds[0] > ct.numChainsOfRounds[1] {
				ct.resizeChainTimetamps(ct.numChainsOfRounds[0])
			} else {
				ct.resizeChainTimetamps(ct.numChainsOfRounds[1])
			}
		} else {
			// Error if round < base or round > base + 2.
			return ErrInvalidRoundID
		}
		ts := ct.chainTimestamps[:ct.numChainsOfRounds[round-ct.numChainsBase]]
		if block.Finalization.Timestamp, err = getMedianTime(ts); err != nil {
			return
		}
		if block.Timestamp.Before(ct.chainTimestamps[block.Position.ChainID]) {
			return ErrTimestampNotIncrease
		}
		ct.chainTimestamps[block.Position.ChainID] = block.Timestamp
		if block.Finalization.Timestamp.Before(ct.lastTimestamp) {
			block.Finalization.Timestamp = ct.lastTimestamp
		} else {
			ct.lastTimestamp = block.Finalization.Timestamp
		}
	}
	return
}

func (ct *consensusTimestamp) isSynced() bool {
	numChain := ct.numChainsOfRounds[0]
	for i := uint32(0); i < numChain; i++ {
		if ct.chainTimestamps[i].Equal(ct.dMoment) {
			return false
		}
	}
	return true
}
