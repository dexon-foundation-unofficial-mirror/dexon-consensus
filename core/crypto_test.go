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
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

type CryptoTestSuite struct {
	suite.Suite
}

func (s *CryptoTestSuite) newBlock(prevBlock *types.Block) *types.Block {
	if prevBlock == nil {
		return &types.Block{
			Hash: common.NewRandomHash(),
			ConsensusInfo: types.ConsensusInfo{
				Timestamp: time.Now(),
				Height:    0,
			},
		}
	}
	parentHash, err := hashCompactionChainAck(prevBlock)
	s.Require().Nil(err)
	return &types.Block{
		Hash: common.NewRandomHash(),
		ConsensusInfoParentHash: parentHash,
		CompactionChainAck: types.CompactionChainAck{
			AckingBlockHash: prevBlock.Hash,
		},
		ConsensusInfo: types.ConsensusInfo{
			Timestamp: time.Now(),
			Height:    prevBlock.ConsensusInfo.Height + 1,
		},
	}
}

func (s *CryptoTestSuite) generateCompactionChain(
	length int, prv crypto.PrivateKey) []*types.Block {
	blocks := make([]*types.Block, length)
	var prevBlock *types.Block
	for idx := range blocks {
		block := s.newBlock(prevBlock)
		blocks[idx] = block
		var err error
		if idx > 0 {
			block.ConsensusInfoParentHash, err = hashCompactionChainAck(blocks[idx-1])
			s.Require().Nil(err)
			block.CompactionChainAck.ConsensusSignature, err =
				signCompactionChainAck(blocks[idx-1], prv)
			s.Require().Nil(err)
		}
	}
	return blocks
}

func (s *CryptoTestSuite) TestSignature() {
	prv, err := eth.NewPrivateKey()
	pub := prv.PublicKey()
	s.Require().Nil(err)
	blocks := s.generateCompactionChain(10, prv)
	blockMap := make(map[common.Hash]*types.Block)
	for _, block := range blocks {
		blockMap[block.Hash] = block
	}
	for _, block := range blocks {
		if block.ConsensusInfo.Height == 0 {
			continue
		}
		ackingBlock, exist := blockMap[block.CompactionChainAck.AckingBlockHash]
		s.Require().True(exist)
		s.True(ackingBlock.ConsensusInfo.Height == block.ConsensusInfo.Height-1)
		hash, err := hashCompactionChainAck(ackingBlock)
		s.Require().Nil(err)
		s.Equal(hash, block.ConsensusInfoParentHash)
		s.True(verifyCompactionChainAckSignature(
			pub, ackingBlock, block.CompactionChainAck.ConsensusSignature))
	}
	// Modify Block.ConsensusTime and verify signature again.
	for _, block := range blocks {
		block.ConsensusInfo.Timestamp = time.Time{}
		if block.ConsensusInfo.Height == 0 {
			continue
		}
		ackingBlock, exist := blockMap[block.CompactionChainAck.AckingBlockHash]
		s.Require().True(exist)
		s.False(verifyCompactionChainAckSignature(
			pub, ackingBlock, block.CompactionChainAck.ConsensusSignature))
	}
}

func TestCrypto(t *testing.T) {
	suite.Run(t, new(CryptoTestSuite))
}
