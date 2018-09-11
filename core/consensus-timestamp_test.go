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
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type ConsensusTimestampTest struct {
	suite.Suite
}

func (s *ConsensusTimestampTest) generateBlocksWithTimestamp(
	blockNum, chainNum int,
	step, sigma time.Duration) []*types.Block {
	blocks := make([]*types.Block, blockNum)
	chainIDs := make([]uint32, len(blocks))
	for i := range chainIDs {
		chainIDs[i] = uint32(i % chainNum)
	}
	rand.Shuffle(len(chainIDs), func(i, j int) {
		chainIDs[i], chainIDs[j] = chainIDs[j], chainIDs[i]
	})
	chainTimestamps := make(map[uint32]time.Time)
	for idx := range blocks {
		blocks[idx] = &types.Block{}
		block := blocks[idx]
		if idx < chainNum {
			// Genesis blocks.
			block.Position.ChainID = uint32(idx)
			block.ParentHash = common.Hash{}
			block.Position.Height = 0
			s.Require().True(block.IsGenesis())
			chainTimestamps[uint32(idx)] = time.Now().UTC()
		} else {
			block.Position.ChainID = chainIDs[idx]
			// Assign 1 to height to make this block non-genesis.
			block.Position.Height = 1
			s.Require().False(block.IsGenesis())
		}
		block.Timestamp = chainTimestamps[block.Position.ChainID]
		// Update timestamp for next block.
		diffSeconds := rand.NormFloat64() * sigma.Seconds()
		diffSeconds = math.Min(diffSeconds, step.Seconds()/2.1)
		diffSeconds = math.Max(diffSeconds, -step.Seconds()/2.1)
		diffDuration := time.Duration(diffSeconds*1000) * time.Millisecond
		chainTimestamps[block.Position.ChainID] =
			chainTimestamps[block.Position.ChainID].Add(step).Add(diffDuration)
		s.Require().True(block.Timestamp.Before(
			chainTimestamps[block.Position.ChainID]))
	}
	return blocks
}

func (s *ConsensusTimestampTest) extractTimestamps(
	blocks []*types.Block) []time.Time {
	timestamps := make([]time.Time, 0, len(blocks))
	for _, block := range blocks {
		if block.IsGenesis() {
			continue
		}
		timestamps = append(timestamps, block.Notary.Timestamp)
	}
	return timestamps
}

// TestTimestampPartition verifies that processing segments of compatction chain
// should have the same result as processing the whole chain at once.
func (s *ConsensusTimestampTest) TestTimestampPartition() {
	blockNums := []int{50, 100, 30}
	validatorNum := 19
	sigma := 100 * time.Millisecond
	totalTimestamps := make([]time.Time, 0)
	ct := newConsensusTimestamp()
	totalBlockNum := 0
	for _, blockNum := range blockNums {
		totalBlockNum += blockNum
	}
	totalChain := s.generateBlocksWithTimestamp(
		totalBlockNum, validatorNum, time.Second, sigma)
	for _, blockNum := range blockNums {
		var chain []*types.Block
		chain, totalChain = totalChain[:blockNum], totalChain[blockNum:]
		err := ct.processBlocks(chain)
		s.Require().NoError(err)
		timestamps := s.extractTimestamps(chain)
		totalChain = append(totalChain, chain...)
		totalTimestamps = append(totalTimestamps, timestamps...)
	}
	ct2 := newConsensusTimestamp()
	err := ct2.processBlocks(totalChain)
	s.Require().NoError(err)
	timestamps2 := s.extractTimestamps(totalChain)
	s.Equal(totalTimestamps, timestamps2)
}

func (s *ConsensusTimestampTest) TestTimestampIncrease() {
	validatorNum := 19
	sigma := 100 * time.Millisecond
	ct := newConsensusTimestamp()
	chain := s.generateBlocksWithTimestamp(1000, validatorNum, time.Second, sigma)
	err := ct.processBlocks(chain)
	s.Require().NoError(err)
	timestamps := s.extractTimestamps(chain)
	for i := 1; i < len(timestamps); i++ {
		s.False(timestamps[i].Before(timestamps[i-1]))
	}
	// Test if the processBlocks is stable.
	ct2 := newConsensusTimestamp()
	ct2.processBlocks(chain)
	s.Require().NoError(err)
	timestamps2 := s.extractTimestamps(chain)
	s.Equal(timestamps, timestamps2)
}

func TestConsensusTimestamp(t *testing.T) {
	suite.Run(t, new(ConsensusTimestampTest))
}
