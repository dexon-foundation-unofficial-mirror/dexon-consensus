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

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// consensusTimestamp is for Concensus Timestamp Algorithm.
type consensusTimestamp struct {
	lastMainChainBlock   *types.Block
	blocksNotInMainChain []*types.Block
}

var (
	// ErrInvalidMainChain would be reported if the invalid result from
	// main chain selection algorithm is detected.
	ErrInvalidMainChain = errors.New("invalid main chain")
)

// newConsensusTimestamp create timestamper object.
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
				getMedianTime(block)
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
