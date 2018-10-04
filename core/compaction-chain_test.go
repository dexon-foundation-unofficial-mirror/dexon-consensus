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
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type CompactionChainTestSuite struct {
	suite.Suite
	db blockdb.BlockDatabase
}

func (s *CompactionChainTestSuite) SetupTest() {
	var err error
	s.db, err = blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)
}

func (s *CompactionChainTestSuite) newCompactionChain() *compactionChain {
	return newCompactionChain(s.db)
}

func (s *CompactionChainTestSuite) generateBlocks(
	size int, cc *compactionChain) []*types.Block {
	now := time.Now().UTC()
	blocks := make([]*types.Block, size)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash:               common.NewRandomHash(),
			ConsensusTimestamp: now,
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
			Hash:               common.NewRandomHash(),
			ConsensusTimestamp: now,
		}
		now = now.Add(100 * time.Millisecond)
	}
	var prevBlock *types.Block
	for _, block := range blocks {
		s.Equal(cc.prevBlock, prevBlock)
		err := cc.processBlock(block)
		s.Require().Nil(err)
		prevBlock = block
	}
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
