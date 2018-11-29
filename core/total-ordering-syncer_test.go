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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type TotalOrderingSyncerTestSuite struct {
	suite.Suite
}

func (s *TotalOrderingSyncerTestSuite) genDeliverySet(numChains uint32) (
	deliverySet [][]*types.Block, revealer *test.RandomDAGRevealer) {

	genesisTime := time.Now().UTC()
	genesisConfig := &types.Config{
		K:             0,
		PhiRatio:      0.5,
		NumChains:     numChains,
		RoundInterval: 1000 * time.Second,
	}

	to := newTotalOrdering(genesisTime, 0, genesisConfig)

	gen := test.NewBlocksGenerator(&test.BlocksGeneratorConfig{
		NumChains:            numChains,
		MinBlockTimeInterval: 250 * time.Millisecond,
	}, nil, hashBlock)

	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().NoError(err)
	s.Require().NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(20*time.Second),
		db))
	iter, err := db.GetAll()
	s.Require().NoError(err)

	revealer, err = test.NewRandomDAGRevealer(iter)
	s.Require().NoError(err)

	revealer.Reset()
	for {
		// Reveal next block.
		b, err := revealer.Next()
		if err != nil {
			if err == blockdb.ErrIterationFinished {
				err = nil
				break
			}
		}
		s.Require().NoError(err)
		// Perform total ordering.
		blocks, _, err := to.processBlock(&b)
		s.Require().NoError(err)
		if len(blocks) > 0 {
			deliverySet = append(deliverySet, blocks)
		}
	}
	return
}

func (s *TotalOrderingSyncerTestSuite) TestRandomSync() {
	numChains := uint32(13)
	skipSet := 2
	skipDAG := int(numChains) * skipSet
	repeat := 100
	if testing.Short() {
		repeat = 10
	}

	for ; repeat >= 0; repeat-- {
		toc := newTotalOrderingSyncer(numChains)
		deliverySet, revealer := s.genDeliverySet(numChains)
		blockToDeliverySet := make(map[common.Hash]int)
		deliverySetMap := make(map[int][]common.Hash)
		offset := 0
		for i, delivery := range deliverySet {
			if i > 0 {
				// The hash of last block of previous delivery set is less than the hash
				// of first block of current delivery set. The syncer cannot seperate
				// these two delevery set so they need to be combined. This will not
				// affect the final result because the output of syncer is also sorted
				// and it will be the same as the output of total ordering.
				if deliverySet[i-1][len(deliverySet[i-1])-1].Hash.Less(
					delivery[0].Hash) {
					offset++
				}
			}
			for _, block := range delivery {
				blockToDeliverySet[block.Hash] = i - offset
				deliverySetMap[i-offset] = append(deliverySetMap[i-offset], block.Hash)
			}
		}

		revealer.Reset()
		for i := 0; i < skipDAG; i++ {
			_, err := revealer.Next()
			s.Require().NoError(err)
		}

		for _, delivery := range deliverySet {
			for _, block := range delivery {
				toc.processFinalizedBlock(block)
			}
		}

		minDeliverySetIdx := -1
		deliverySetMap2 := make(map[int][]common.Hash)

		for {
			b, err := revealer.Next()
			if err != nil {
				if err != blockdb.ErrIterationFinished {
					s.Require().NoError(err)
				}
				err = nil
				break
			}
			deliver := toc.processBlock(&b)
			for _, block := range deliver {
				idx, exist := blockToDeliverySet[block.Hash]
				if !exist {
					continue
				}
				if minDeliverySetIdx == -1 {
					minDeliverySetIdx = idx
				}
				s.Require().True(idx >= minDeliverySetIdx)
				deliverySetMap2[idx] = append(deliverySetMap2[idx], block.Hash)
			}
		}

		s.Require().NotEqual(-1, minDeliverySetIdx)
		for i := minDeliverySetIdx; ; i++ {
			if _, exist := deliverySetMap[i]; !exist {
				break
			}
			for _, v := range deliverySetMap[i] {
				s.Contains(deliverySetMap2[i], v)
			}
			s.Require().Len(deliverySetMap2[i], len(deliverySetMap[i]))
		}
	}
}

// TestMissingMiddleDeliverySet tests the following case
// The number denotes the index of delivery set.
// X means that the block is not synced in lattice.
// O means that the block is synced but the index is not important.
// The assumption is that once the block is synced, all newer blocks
// on the same chain will be synced as well.
// ********************
//  O O O 5 6
//  3 3 3 X 5
// ------------
//  0 1 2 3 4(ChainID)
// ********************
// In this case, the block of delivery set 4 is not synced in lattice;
// therefore, the minimum index of delivery set should be 5 instead of 3.
// (Note: delivery set 6 is to make syncer identify delivery set 5)

func (s *TotalOrderingSyncerTestSuite) TestMissingMiddleDeliverySet() {
	numChains := uint32(5)
	b00 := &types.Block{
		Hash: common.Hash{0x10},
		Position: types.Position{
			ChainID: uint32(0),
			Height:  uint64(3),
		},
	}
	b10 := &types.Block{
		Hash: common.Hash{0x20},
		Position: types.Position{
			ChainID: uint32(1),
			Height:  uint64(3),
		},
	}
	b20 := &types.Block{
		Hash: common.Hash{0x30},
		Position: types.Position{
			ChainID: uint32(2),
			Height:  uint64(3),
		},
	}
	b30 := &types.Block{
		Hash: common.Hash{0x21},
		Position: types.Position{
			ChainID: uint32(3),
			Height:  uint64(3),
		},
	}
	b31 := &types.Block{
		Hash: common.Hash{0x12},
		Position: types.Position{
			ChainID: uint32(3),
			Height:  uint64(4),
		},
	}
	b40 := &types.Block{
		Hash: common.Hash{0x22},
		Position: types.Position{
			ChainID: uint32(4),
			Height:  uint64(3),
		},
	}
	b41 := &types.Block{
		Hash: common.Hash{0x12},
		Position: types.Position{
			ChainID: uint32(4),
			Height:  uint64(4),
		},
	}
	blocks := []*types.Block{b00, b10, b20, b30, b31, b40, b41}

	// Test process sequence 1.
	toc := newTotalOrderingSyncer(numChains)

	for _, block := range blocks {
		toc.processFinalizedBlock(block)
	}

	s.Require().Len(toc.processBlock(b00), 0)
	s.Require().Len(toc.processBlock(b10), 0)
	s.Require().Len(toc.processBlock(b20), 0)
	s.Require().Len(toc.processBlock(b31), 0)
	deliver := toc.processBlock(b40)
	s.Require().Len(deliver, 2)
	s.Equal(deliver[0], b31)
	s.Equal(deliver[1], b40)

	// Test process sequence 2.
	toc2 := newTotalOrderingSyncer(numChains)

	for _, block := range blocks {
		toc2.processFinalizedBlock(block)
	}

	s.Require().Len(toc2.processBlock(b31), 0)
	s.Require().Len(toc2.processBlock(b40), 0)
	s.Require().Len(toc2.processBlock(b20), 0)
	s.Require().Len(toc2.processBlock(b10), 0)
	deliver = toc2.processBlock(b00)
	s.Require().Len(deliver, 2)
	s.Equal(deliver[0], b31)
	s.Equal(deliver[1], b40)

}

func (s *TotalOrderingSyncerTestSuite) TestBootstrap() {
	numChains := uint32(13)
	toc := newTotalOrderingSyncer(numChains)
	deliverySet, revealer := s.genDeliverySet(numChains)
	deliveredNum := 0
	for _, delivery := range deliverySet {
		deliveredNum += len(delivery)
	}

	actualDeliveredNum := 0
	revealer.Reset()
	for {
		b, err := revealer.Next()
		if err != nil {
			if err != blockdb.ErrIterationFinished {
				s.Require().NoError(err)
			}
			err = nil
			break
		}
		deliver := toc.processBlock(&b)
		actualDeliveredNum += len(deliver)
	}

	// The last few blocks revealer might not be in the output of total order.
	// So the deliveredNum might be less than actualDeliveredNum.
	s.True(actualDeliveredNum >= deliveredNum)
}

func TestTotalOrderingSyncer(t *testing.T) {
	suite.Run(t, new(TotalOrderingSyncerTestSuite))
}
