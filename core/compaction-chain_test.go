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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

type CompactionChainTestSuite struct {
	suite.Suite
}

func (s *CompactionChainTestSuite) TestProcessBlock() {
	cc := newCompactionChain()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
		}
	}
	var prevBlock *types.Block
	for _, block := range blocks {
		s.Equal(cc.prevBlock, prevBlock)
		cc.processBlock(block)
		if prevBlock != nil {
			s.Equal(block.ConsensusInfo.Height, prevBlock.ConsensusInfo.Height+1)
			prevHash, err := hashConsensusInfo(prevBlock)
			s.Require().Nil(err)
			s.Equal(prevHash, block.ConsensusInfoParentHash)
		}
		prevBlock = block
	}
}

func (s *CompactionChainTestSuite) TestPrepareBlock() {
	cc := newCompactionChain()
	blocks := make([]*types.Block, 10)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			ConsensusInfo: types.ConsensusInfo{
				Height: uint64(idx),
			},
		}
		if idx > 0 {
			var err error
			blocks[idx].ConsensusInfoParentHash, err = hashConsensusInfo(blocks[idx-1])
			s.Require().Nil(err)
		}
	}
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	for _, block := range blocks {
		cc.prepareBlock(block, prv)
		if cc.prevBlock != nil {
			s.True(verifyConsensusInfoSignature(
				prv.PublicKey(),
				cc.prevBlock,
				block.CompactionChainAck.ConsensusInfoSignature))
			s.Equal(block.CompactionChainAck.AckingBlockHash, cc.prevBlock.Hash)
		}
		cc.prevBlock = block
	}
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
