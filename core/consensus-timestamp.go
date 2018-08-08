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

package core

import (
	"errors"
	"sort"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Timestamp is for Concensus Timestamp Algorithm.
type consensusTimestamp struct {
	lastMainChainBlock   *types.Block
	blocksNotInMainChain []*types.Block
}

var (
	// ErrInvalidMainChain would be reported if the invalid result from
	// main chain selection algorithm is detected.
	ErrInvalidMainChain = errors.New("invalid main chain")
	// ErrEmptyTimestamps would be reported if Block.timestamps is empty.
	ErrEmptyTimestamps = errors.New("timestamp vector should not be empty")
)

// NewTimestamp create Timestamp object.
func newConsensusTimestamp() *consensusTimestamp {
	return &consensusTimestamp{}
}

// ProcessBlocks is the entry function.
func (ct *consensusTimestamp) processBlocks(blocks []*types.Block) (
	blocksWithTimestamp []*types.Block, mainChain []*types.Block, err error) {
	if len(blocks) == 0 {
		// TODO (jimmy-dexon): Remove this panic before release.
		panic("Unexpected empty block list.")
	}
	outputFirstBlock := true
	blocks = append(ct.blocksNotInMainChain, blocks...)
	if ct.lastMainChainBlock != nil {
		// TODO (jimmy-dexon): The performance here can be optimized.
		blocks = append([]*types.Block{ct.lastMainChainBlock}, blocks...)
		outputFirstBlock = false
	}
	mainChain, nonMainChain := ct.selectMainChain(blocks)
	ct.blocksNotInMainChain = nonMainChain
	ct.lastMainChainBlock = mainChain[len(mainChain)-1]
	blocksWithTimestamp = blocks[:len(blocks)-len(nonMainChain)]
	leftMainChainIdx := 0
	rightMainChainIdx := 0
	idxMainChain := 0
	for idx, block := range blocksWithTimestamp {
		if idxMainChain >= len(mainChain) {
			err = ErrInvalidMainChain
			return
		} else if block.Hash == mainChain[idxMainChain].Hash {
			rightMainChainIdx = idx
			blocksWithTimestamp[idx].ConsensusInfo.Timestamp, err =
				ct.getMedianTime(block)
			if err != nil {
				return
			}
			// Process Non-MainChain blocks.
			if rightMainChainIdx > leftMainChainIdx {
				for idx, timestamp := range interpoTime(
					blocksWithTimestamp[leftMainChainIdx].ConsensusInfo.Timestamp,
					blocksWithTimestamp[rightMainChainIdx].ConsensusInfo.Timestamp,
					rightMainChainIdx-leftMainChainIdx-1) {
					blocksWithTimestamp[leftMainChainIdx+idx+1].ConsensusInfo.Timestamp =
						timestamp
				}
			}
			leftMainChainIdx = idx
			idxMainChain++
		}
	}
	if !outputFirstBlock {
		blocksWithTimestamp = blocksWithTimestamp[1:]
	}
	return
}

func (ct *consensusTimestamp) selectMainChain(blocks []*types.Block) (
	mainChain []*types.Block, nonMainChain []*types.Block) {
	for _, block := range blocks {
		if len(mainChain) != 0 {
			if _, exists := block.Acks[mainChain[len(mainChain)-1].Hash]; !exists {
				nonMainChain = append(nonMainChain, block)
				continue
			}
		}
		nonMainChain = []*types.Block{}
		mainChain = append(mainChain, block)
	}
	return
}

func (ct *consensusTimestamp) getMedianTime(block *types.Block) (
	timestamp time.Time, err error) {
	timestamps := []time.Time{}
	for _, timestamp := range block.Timestamps {
		timestamps = append(timestamps, timestamp)
	}
	if len(timestamps) == 0 {
		err = ErrEmptyTimestamps
		return
	}
	sort.Sort(common.ByTime(timestamps))
	if len(timestamps)%2 == 0 {
		t1 := timestamps[len(timestamps)/2-1]
		t2 := timestamps[len(timestamps)/2]
		timestamp = interpoTime(t1, t2, 1)[0]
	} else {
		timestamp = timestamps[len(timestamps)/2]
	}
	return
}

func interpoTime(t1 time.Time, t2 time.Time, sep int) []time.Time {
	if sep == 0 {
		return []time.Time{}
	}
	if t1.After(t2) {
		return interpoTime(t2, t1, sep)
	}
	timestamps := make([]time.Time, sep)
	duration := t2.Sub(t1)
	period := time.Duration(
		(duration.Nanoseconds() / int64(sep+1))) * time.Nanosecond
	prevTime := t1
	for idx := range timestamps {
		prevTime = prevTime.Add(period)
		timestamps[idx] = prevTime
	}
	return timestamps
}
