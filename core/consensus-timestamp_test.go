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

func generateBlocksWithAcks(blockNum, maxAcks int) []*types.Block {
	chain := []*types.Block{
		&types.Block{
			Hash: common.NewRandomHash(),
			Acks: make(map[common.Hash]struct{}),
		},
	}
	for i := 1; i < blockNum; i++ {
		acks := make(map[common.Hash]struct{})
		ackNum := rand.Intn(maxAcks) + 1
		for j := 0; j < ackNum; j++ {
			ack := rand.Intn(len(chain))
			acks[chain[ack].Hash] = struct{}{}
		}
		block := &types.Block{
			Hash: common.NewRandomHash(),
			Acks: acks,
		}
		chain = append(chain, block)
	}
	return chain
}

func fillBlocksTimestamps(blocks []*types.Block, validatorNum int,
	step, sigma time.Duration) {
	curTime := time.Now().UTC()
	vIDs := make([]types.ValidatorID, validatorNum)
	for i := 0; i < validatorNum; i++ {
		vIDs[i] = types.ValidatorID{Hash: common.NewRandomHash()}
	}
	for _, block := range blocks {
		block.Timestamps = make(map[types.ValidatorID]time.Time)
		for _, vID := range vIDs {
			diffSeconds := rand.NormFloat64() * sigma.Seconds()
			diffSeconds = math.Min(diffSeconds, step.Seconds()/2)
			diffSeconds = math.Max(diffSeconds, -step.Seconds()/2)
			diffDuration := time.Duration(diffSeconds*1000) * time.Millisecond
			block.Timestamps[vID] = curTime.Add(diffDuration)
		}
		curTime = curTime.Add(step)
	}
}

func extractTimestamps(blocks []*types.Block) []time.Time {
	timestamps := make([]time.Time, len(blocks))
	for idx, block := range blocks {
		timestamps[idx] = block.ConsensusTime
	}
	return timestamps
}

func (s *ConsensusTimestampTest) TestMainChainSelection() {
	ct := newConsensusTimestamp()
	ct2 := newConsensusTimestamp()
	blockNums := []int{50, 100, 30}
	maxAcks := 5
	for _, blockNum := range blockNums {
		chain := generateBlocksWithAcks(blockNum, maxAcks)
		mainChain, _ := ct.selectMainChain(chain)
		// Verify the selected main chain.
		for i := 1; i < len(mainChain); i++ {
			_, exists := mainChain[i].Acks[mainChain[i-1].Hash]
			s.True(exists)
		}
		// Verify if selectMainChain is stable.
		mainChain2, _ := ct2.selectMainChain(chain)
		s.Equal(mainChain, mainChain2)
	}
}

func (s *ConsensusTimestampTest) TestTimestampPartition() {
	blockNums := []int{50, 100, 30}
	validatorNum := 19
	sigma := 100 * time.Millisecond
	maxAcks := 5
	totalMainChain := make([]*types.Block, 1)
	totalChain := make([]*types.Block, 0)
	totalTimestamps := make([]time.Time, 0)
	ct := newConsensusTimestamp()
	var lastMainChainBlock *types.Block
	for _, blockNum := range blockNums {
		chain := generateBlocksWithAcks(blockNum, maxAcks)
		fillBlocksTimestamps(chain, validatorNum, time.Second, sigma)
		blocksWithTimestamps, mainChain, err := ct.processBlocks(chain)
		s.Require().Nil(err)
		timestamps := extractTimestamps(blocksWithTimestamps)
		if lastMainChainBlock != nil {
			s.Require().Equal(mainChain[0], lastMainChainBlock)
		}
		s.Require().Equal(mainChain[len(mainChain)-1], ct.lastMainChainBlock)
		lastMainChainBlock = ct.lastMainChainBlock
		totalMainChain =
			append(totalMainChain[:len(totalMainChain)-1], mainChain...)
		totalChain = append(totalChain, chain...)
		totalTimestamps = append(totalTimestamps, timestamps...)
	}
	ct2 := newConsensusTimestamp()
	blocksWithTimestamps2, mainChain2, err := ct2.processBlocks(totalChain)
	s.Require().Nil(err)
	timestamps2 := extractTimestamps(blocksWithTimestamps2)
	s.Equal(totalMainChain, mainChain2)
	s.Equal(totalTimestamps, timestamps2)
}

func timeDiffWithinTolerance(t1, t2 time.Time, tolerance time.Duration) bool {
	if t1.After(t2) {
		return timeDiffWithinTolerance(t2, t1, tolerance)
	}
	return t1.Add(tolerance).After(t2)
}

func (s *ConsensusTimestampTest) TestTimestampIncrease() {
	validatorNum := 19
	sigma := 100 * time.Millisecond
	ct := newConsensusTimestamp()
	chain := generateBlocksWithAcks(1000, 5)
	fillBlocksTimestamps(chain, validatorNum, time.Second, sigma)
	blocksWithTimestamps, _, err := ct.processBlocks(chain)
	s.Require().Nil(err)
	timestamps := extractTimestamps(blocksWithTimestamps)
	for i := 1; i < len(timestamps); i++ {
		s.True(timestamps[i].After(timestamps[i-1]))
	}
	// Test if the processBlocks is stable.
	ct2 := newConsensusTimestamp()
	blocksWithTimestamps2, _, err := ct2.processBlocks(chain)
	s.Require().Nil(err)
	timestamps2 := extractTimestamps(blocksWithTimestamps2)
	s.Equal(timestamps, timestamps2)
}

func (s *ConsensusTimestampTest) TestByzantineBiasTime() {
	// Test that Byzantine node cannot bias the timestamps.
	validatorNum := 19
	sigma := 100 * time.Millisecond
	tolerance := 4 * sigma
	ct := newConsensusTimestamp()
	chain := generateBlocksWithAcks(1000, 5)
	fillBlocksTimestamps(chain, validatorNum, time.Second, sigma)
	blocksWithTimestamps, _, err := ct.processBlocks(chain)
	s.Require().Nil(err)
	timestamps := extractTimestamps(blocksWithTimestamps)
	byzantine := validatorNum / 3
	validators := make([]types.ValidatorID, 0, validatorNum)
	for vID := range chain[0].Timestamps {
		validators = append(validators, vID)
	}
	// The number of Byzantine node is at most N/3.
	for i := 0; i < byzantine; i++ {
		// Pick one validator to be Byzantine node.
		// It is allowed to have the vID be duplicated,
		// because the number of Byzantine node is between 1 and N/3.
		vID := validators[rand.Intn(validatorNum)]
		for _, block := range chain {
			block.Timestamps[vID] = time.Time{}
		}
	}
	ctByzantine := newConsensusTimestamp()
	blocksWithTimestampsB, _, err := ctByzantine.processBlocks(chain)
	s.Require().Nil(err)
	timestampsWithByzantine := extractTimestamps(blocksWithTimestampsB)
	for idx, timestamp := range timestamps {
		timestampWithByzantine := timestampsWithByzantine[idx]
		s.True(timeDiffWithinTolerance(
			timestamp, timestampWithByzantine, tolerance))
	}
}

func TestConsensusTimestamp(t *testing.T) {
	suite.Run(t, new(ConsensusTimestampTest))
}
