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

var myVID = types.ValidatorID{Hash: common.NewRandomHash()}

type simpleBlock struct {
	block *types.Block
}

func (sb *simpleBlock) Block() *types.Block {
	return sb.block
}

func (sb *simpleBlock) GetPayloads() [][]byte {
	return [][]byte{}
}

func (s *CryptoTestSuite) prepareBlock(prevBlock *types.Block) *types.Block {
	acks := make(map[common.Hash]struct{})
	timestamps := make(map[types.ValidatorID]time.Time)
	timestamps[myVID] = time.Now().UTC()
	if prevBlock == nil {
		return &types.Block{
			Acks:       acks,
			Timestamps: timestamps,
			ConsensusInfo: types.ConsensusInfo{
				Timestamp: time.Now(),
				Height:    0,
			},
		}
	}
	parentHash, err := hashCompactionChainAck(prevBlock)
	s.Require().Nil(err)
	s.Require().NotEqual(prevBlock.Hash, common.Hash{})
	acks[parentHash] = struct{}{}
	return &types.Block{
		ParentHash:              prevBlock.Hash,
		Acks:                    acks,
		Timestamps:              timestamps,
		ConsensusInfoParentHash: parentHash,
		Height:                  prevBlock.Height + 1,
		CompactionChainAck: types.CompactionChainAck{
			AckingBlockHash: prevBlock.Hash,
		},
		ConsensusInfo: types.ConsensusInfo{
			Timestamp: time.Now(),
			Height:    prevBlock.ConsensusInfo.Height + 1,
		},
	}
}

func (s *CryptoTestSuite) newBlock(prevBlock *types.Block) *types.Block {
	block := s.prepareBlock(prevBlock)
	var err error
	block.Hash, err = hashBlock(&simpleBlock{block: block})
	s.Require().Nil(err)
	return block
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

func (s *CryptoTestSuite) TestCompactionChainAckSignature() {
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

func (s *CryptoTestSuite) generateBlockChain(
	length int, prv crypto.PrivateKey) []*types.Block {
	blocks := make([]*types.Block, length)
	var prevBlock *types.Block
	for idx := range blocks {
		block := s.newBlock(prevBlock)
		blocks[idx] = block
		var err error
		block.Signature, err = signBlock(&simpleBlock{block: block}, prv)
		s.Require().Nil(err)
	}
	return blocks
}

func (s *CryptoTestSuite) TestBlockSignature() {
	prv, err := eth.NewPrivateKey()
	pub := prv.PublicKey()
	s.Require().Nil(err)
	blocks := s.generateBlockChain(10, prv)
	blockMap := make(map[common.Hash]*types.Block)
	for _, block := range blocks {
		blockMap[block.Hash] = block
	}
	for _, block := range blocks {
		if !block.IsGenesis() {
			parentBlock, exist := blockMap[block.ParentHash]
			s.Require().True(exist)
			s.True(parentBlock.Height == block.Height-1)
			hash, err := hashBlock(&simpleBlock{block: parentBlock})
			s.Require().Nil(err)
			s.Equal(hash, block.ParentHash)
		}
		s.True(verifyBlockSignature(pub, &simpleBlock{block: block}, block.Signature))
	}
	// Modify Block.Acks and verify signature again.
	for _, block := range blocks {
		block.Acks[common.NewRandomHash()] = struct{}{}
		s.False(verifyBlockSignature(
			pub, &simpleBlock{block: block}, block.Signature))
	}
}

func TestCrypto(t *testing.T) {
	suite.Run(t, new(CryptoTestSuite))
}
