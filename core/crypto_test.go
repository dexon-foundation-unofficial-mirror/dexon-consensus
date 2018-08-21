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

func (sb *simpleBlock) Payloads() [][]byte {
	return [][]byte{}
}

func (sb *simpleBlock) SetBlock(block *types.Block) {
	*sb.block = *block
}

func (s *CryptoTestSuite) prepareBlock(prevBlock *types.Block) *types.Block {
	acks := make(map[common.Hash]struct{})
	timestamps := make(map[types.ValidatorID]time.Time)
	timestamps[myVID] = time.Now().UTC()
	if prevBlock == nil {
		return &types.Block{
			Acks:       acks,
			Timestamps: timestamps,
			Notary: types.Notary{
				Timestamp: time.Now(),
				Height:    0,
			},
		}
	}
	parentHash, err := hashNotary(prevBlock)
	s.Require().Nil(err)
	s.Require().NotEqual(prevBlock.Hash, common.Hash{})
	acks[parentHash] = struct{}{}
	return &types.Block{
		ParentHash: prevBlock.Hash,
		Acks:       acks,
		Timestamps: timestamps,
		Height:     prevBlock.Height + 1,
		Notary: types.Notary{
			ParentHash: parentHash,
			Timestamp:  time.Now(),
			Height:     prevBlock.Notary.Height + 1,
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
	length int, prv crypto.PrivateKey) (
	[]*types.Block, []types.NotaryAck) {
	blocks := make([]*types.Block, length)
	notaryAcks := make([]types.NotaryAck, length)
	var prevBlock *types.Block
	for idx := range blocks {
		block := s.newBlock(prevBlock)
		prevBlock = block
		blocks[idx] = block
		var err error
		notaryAcks[idx].Hash, err = hashNotary(blocks[idx])
		s.Require().Nil(err)
		notaryAcks[idx].NotaryBlockHash = blocks[idx].Hash
		notaryAcks[idx].Signature, err = prv.Sign(notaryAcks[idx].Hash)
		s.Require().Nil(err)
		if idx > 0 {
			block.Notary.ParentHash = notaryAcks[idx-1].Hash
		}
	}
	return blocks, notaryAcks
}

func (s *CryptoTestSuite) TestNotaryAckSignature() {
	prv, err := eth.NewPrivateKey()
	pub := prv.PublicKey()
	s.Require().Nil(err)
	blocks, notaryAcks := s.generateCompactionChain(10, prv)
	blockMap := make(map[common.Hash]*types.Block)
	for _, block := range blocks {
		blockMap[block.Hash] = block
	}
	parentBlock := blocks[0]
	for _, notaryAck := range notaryAcks {
		notaryBlock, exist := blockMap[notaryAck.NotaryBlockHash]
		s.Require().True(exist)
		if notaryBlock.Notary.Height == 0 {
			continue
		}
		s.True(parentBlock.Notary.Height == notaryBlock.Notary.Height-1)
		hash, err := hashNotary(parentBlock)
		s.Require().Nil(err)
		s.Equal(hash, notaryBlock.Notary.ParentHash)
		s.True(verifyNotarySignature(
			pub, notaryBlock, notaryAck.Signature))
		parentBlock = notaryBlock

	}
	// Modify Block.Notary.Timestamp and verify signature again.
	for _, notaryAck := range notaryAcks {
		block, exist := blockMap[notaryAck.NotaryBlockHash]
		s.Require().True(exist)
		block.Notary.Timestamp = time.Time{}
		ackingBlock, exist := blockMap[notaryAck.NotaryBlockHash]
		s.Require().True(exist)
		s.False(verifyNotarySignature(
			pub, ackingBlock, notaryAck.Signature))
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
		block.Signature, err = prv.Sign(block.Hash)
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
