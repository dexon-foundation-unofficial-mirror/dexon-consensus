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
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type ConsensusTimestampTest struct {
	suite.Suite
}

func (s *ConsensusTimestampTest) generateBlocksWithTimestamp(
	now time.Time,
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
			chainTimestamps[uint32(idx)] = now
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
		timestamps = append(timestamps, block.Finalization.Timestamp)
	}
	return timestamps
}

// TestTimestampPartition verifies that processing segments of compatction chain
// should have the same result as processing the whole chain at once.
func (s *ConsensusTimestampTest) TestTimestampPartition() {
	blockNums := []int{50, 100, 30}
	chainNum := 19
	sigma := 100 * time.Millisecond
	totalTimestamps := make([]time.Time, 0)
	now := time.Now().UTC()
	ct := newConsensusTimestamp(now, 0, uint32(chainNum))
	totalBlockNum := 0
	for _, blockNum := range blockNums {
		totalBlockNum += blockNum
	}
	totalChain := s.generateBlocksWithTimestamp(now,
		totalBlockNum, chainNum, time.Second, sigma)
	for _, blockNum := range blockNums {
		var chain []*types.Block
		chain, totalChain = totalChain[:blockNum], totalChain[blockNum:]
		err := ct.processBlocks(chain)
		s.Require().NoError(err)
		timestamps := s.extractTimestamps(chain)
		totalChain = append(totalChain, chain...)
		totalTimestamps = append(totalTimestamps, timestamps...)
	}
	ct2 := newConsensusTimestamp(now, 0, uint32(chainNum))
	err := ct2.processBlocks(totalChain)
	s.Require().NoError(err)
	timestamps2 := s.extractTimestamps(totalChain)
	s.Equal(totalTimestamps, timestamps2)
}

func (s *ConsensusTimestampTest) TestTimestampIncrease() {
	chainNum := 19
	sigma := 100 * time.Millisecond
	now := time.Now().UTC()
	ct := newConsensusTimestamp(now, 0, uint32(chainNum))
	chain := s.generateBlocksWithTimestamp(
		now, 1000, chainNum, time.Second, sigma)
	err := ct.processBlocks(chain)
	s.Require().NoError(err)
	timestamps := s.extractTimestamps(chain)
	for i := 1; i < len(timestamps); i++ {
		s.False(timestamps[i].Before(timestamps[i-1]))
	}
	// Test if the processBlocks is stable.
	ct2 := newConsensusTimestamp(now, 0, uint32(chainNum))
	ct2.processBlocks(chain)
	s.Require().NoError(err)
	timestamps2 := s.extractTimestamps(chain)
	s.Equal(timestamps, timestamps2)
}

func (s *ConsensusTimestampTest) TestTimestampConfigChange() {
	chainNum := 19
	sigma := 100 * time.Millisecond
	now := time.Now().UTC()
	ct := newConsensusTimestamp(now, 20, uint32(chainNum))
	chain := s.generateBlocksWithTimestamp(now,
		1000, chainNum, time.Second, sigma)
	blocks := make([]*types.Block, 0, 1000)
	ct.appendConfig(21, &types.Config{NumChains: uint32(16)})
	ct.appendConfig(22, &types.Config{NumChains: uint32(19)})
	// Blocks 0 to 299 is in round 20, blocks 300 to 599 is in round 21 and ignore
	// blocks which ChainID is 16 to 18, blocks 600 to 999 is in round 22.
	for i := 0; i < 1000; i++ {
		add := true
		if i < 300 {
			chain[i].Position.Round = 20
		} else if i < 600 {
			chain[i].Position.Round = 21
			add = chain[i].Position.ChainID < 16
		} else {
			chain[i].Position.Round = 22
		}
		if add {
			blocks = append(blocks, chain[i])
		}
	}
	err := ct.processBlocks(blocks)
	s.Require().NoError(err)
}

func (s *ConsensusTimestampTest) TestTimestampRoundInterleave() {
	chainNum := 9
	sigma := 100 * time.Millisecond
	now := time.Now().UTC()
	ct := newConsensusTimestamp(now, 0, uint32(chainNum))
	ct.appendConfig(1, &types.Config{NumChains: uint32(chainNum)})
	chain := s.generateBlocksWithTimestamp(now,
		100, chainNum, time.Second, sigma)
	for i := 50; i < 100; i++ {
		chain[i].Position.Round = 1
	}
	chain[48].Position.Round = 1
	chain[49].Position.Round = 1
	chain[50].Position.Round = 0
	chain[51].Position.Round = 0
	err := ct.processBlocks(chain)
	s.Require().NoError(err)
}

func (s *ConsensusTimestampTest) TestNumChainsChangeAtSecondAppendedRound() {
	now := time.Now().UTC()
	ct := newConsensusTimestamp(now, 1, 4)
	s.Require().NoError(ct.appendConfig(2, &types.Config{NumChains: 5}))
	// We should be able to handle a block from the second appended round.
	s.Require().NoError(ct.processBlocks([]*types.Block{
		&types.Block{
			Position:  types.Position{Round: 2},
			Timestamp: now.Add(1 * time.Second),
		}}))
}

func (s *ConsensusTimestampTest) TestTimestampSync() {
	chainNum := 19
	sigma := 100 * time.Millisecond
	now := time.Now().UTC()
	ct := newConsensusTimestamp(now, 0, uint32(chainNum))
	chain := s.generateBlocksWithTimestamp(now,
		100, chainNum, time.Second, sigma)
	err := ct.processBlocks(chain[:chainNum-1])
	s.Require().NoError(err)
	s.Require().False(ct.isSynced())
	err = ct.processBlocks(chain[chainNum-1:])
	s.Require().NoError(err)
	s.Require().True(ct.isSynced())
}

func TestConsensusTimestamp(t *testing.T) {
	suite.Run(t, new(ConsensusTimestampTest))
}
