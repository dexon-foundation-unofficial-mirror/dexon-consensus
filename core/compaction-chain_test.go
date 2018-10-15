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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type CompactionChainTestSuite struct {
	suite.Suite
}

func (s *CompactionChainTestSuite) SetupTest() {
}

func (s *CompactionChainTestSuite) newCompactionChain() *compactionChain {
	return newCompactionChain()
}

func (s *CompactionChainTestSuite) generateBlocks(
	size int, cc *compactionChain) []*types.Block {
	now := time.Now().UTC()
	blocks := make([]*types.Block, size)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Finalization: types.FinalizationResult{
				Timestamp: now,
			},
		}
		now = now.Add(100 * time.Millisecond)
	}
	for _, block := range blocks {
		err := cc.processBlock(block)
		s.Require().Nil(err)
	}
	return blocks
}

func (s *CompactionChainTestSuite) TestProcessBlock() {
	cc := s.newCompactionChain()
	now := time.Now().UTC()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Finalization: types.FinalizationResult{
				Timestamp: now,
			},
		}
		now = now.Add(100 * time.Millisecond)
	}
	var prevBlock *types.Block
	for _, block := range blocks {
		s.Equal(cc.prevBlock, prevBlock)
		s.Require().NoError(cc.processBlock(block))
		prevBlock = block
	}
}

func (s *CompactionChainTestSuite) TestExtractBlocks() {
	cc := s.newCompactionChain()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Position: types.Position{
				Round: 1,
			},
		}
		s.Require().False(cc.blockRegistered(blocks[idx].Hash))
		cc.registerBlock(blocks[idx])
		s.Require().True(cc.blockRegistered(blocks[idx].Hash))
	}
	// Randomness is ready for extract.
	for i := 0; i < 3; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered := cc.extractBlocks()
	s.Require().Len(delivered, 3)

	// Randomness is not yet ready for extract.
	for i := 3; i < 6; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 3)

	// Make some randomness ready.
	for i := 3; i < 6; i++ {
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 6)

	// Later block's randomness is ready.
	for i := 6; i < 10; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
		if i < 8 {
			continue
		}
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 6)

	// Prior block's randomness is ready.
	for i := 6; i < 8; i++ {
		h := common.NewRandomHash()
		s.Require().NoError(cc.processBlockRandomnessResult(
			&types.BlockRandomnessResult{
				BlockHash:  blocks[i].Hash,
				Randomness: h[:],
			}))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 10)

	// The delivered order should be the same as processing order.
	for i, block := range delivered {
		s.Equal(block.Hash, blocks[i].Hash)
	}
}

func (s *CompactionChainTestSuite) TestExtractBlocksRound0() {
	cc := s.newCompactionChain()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Position: types.Position{
				Round: 0,
			},
		}
		s.Require().False(cc.blockRegistered(blocks[idx].Hash))
		cc.registerBlock(blocks[idx])
		s.Require().True(cc.blockRegistered(blocks[idx].Hash))
	}
	// Round 0 should be able to be extracted without randomness.
	for i := 0; i < 3; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	delivered := cc.extractBlocks()
	s.Require().Len(delivered, 3)

	// Round 0 should be able to be extracted without randomness.
	for i := 3; i < 10; i++ {
		s.Require().NoError(cc.processBlock(blocks[i]))
	}
	delivered = append(delivered, cc.extractBlocks()...)
	s.Require().Len(delivered, 10)

	// The delivered order should be the same as processing order.
	for i, block := range delivered {
		s.Equal(block.Hash, blocks[i].Hash)
	}
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
