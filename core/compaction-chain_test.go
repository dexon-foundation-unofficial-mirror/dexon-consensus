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

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
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
	return newCompactionChain(s.db, eth.SigToPub)
}

func (s *CompactionChainTestSuite) generateBlocks(
	size int, cc *compactionChain) []*types.Block {
	now := time.Now().UTC()
	blocks := make([]*types.Block, size)
	for idx := range blocks {
		blocks[idx] = &types.Block{
			Hash: common.NewRandomHash(),
			Notary: types.Notary{
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
			Notary: types.Notary{
				Timestamp: now,
			},
		}
		now = now.Add(100 * time.Millisecond)
	}
	var prevBlock *types.Block
	for _, block := range blocks {
		s.Equal(cc.prevBlock, prevBlock)
		err := cc.processBlock(block)
		s.Require().Nil(err)
		if prevBlock != nil {
			s.Equal(block.Notary.Height, prevBlock.Notary.Height+1)
			prevHash, err := hashNotary(prevBlock)
			s.Require().Nil(err)
			s.Equal(prevHash, block.Notary.ParentHash)
		}
		prevBlock = block
	}
}

func (s *CompactionChainTestSuite) TestPrepareNotaryAck() {
	cc := s.newCompactionChain()
	blocks := s.generateBlocks(10, cc)
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	for _, block := range blocks {
		notaryAck, err := cc.prepareNotaryAck(prv)
		s.Require().Nil(err)
		if cc.prevBlock != nil {
			s.True(verifyNotarySignature(
				prv.PublicKey(),
				cc.prevBlock,
				notaryAck.Signature))
			s.Equal(notaryAck.NotaryBlockHash, cc.prevBlock.Hash)
		}
		cc.prevBlock = block
	}
}

func (s *CompactionChainTestSuite) TestProcessNotaryAck() {
	cc := s.newCompactionChain()
	blocks := s.generateBlocks(10, cc)
	prv1, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	prv2, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	vID1 := types.NewValidatorID(prv1.PublicKey())
	vID2 := types.NewValidatorID(prv2.PublicKey())
	notaryAcks1 := []*types.NotaryAck{}
	notaryAcks2 := []*types.NotaryAck{}
	for _, block := range blocks {
		cc.prevBlock = block
		notaryAck1, err := cc.prepareNotaryAck(prv1)
		s.Require().Nil(err)
		notaryAck2, err := cc.prepareNotaryAck(prv2)
		s.Require().Nil(err)
		notaryAcks1 = append(notaryAcks1, notaryAck1)
		notaryAcks2 = append(notaryAcks2, notaryAck2)
	}
	// The acked block is not yet in db.
	err = cc.processNotaryAck(notaryAcks1[0])
	s.Nil(err)
	s.Equal(0, len(cc.notaryAcks()))
	err = cc.processNotaryAck(notaryAcks2[1])
	s.Nil(err)
	s.Equal(0, len(cc.notaryAcks()))
	// Insert to block to db and trigger processPendingNotaryAck.
	s.Require().Nil(s.db.Put(*blocks[0]))
	s.Require().Nil(s.db.Put(*blocks[1]))
	err = cc.processNotaryAck(notaryAcks1[2])
	s.Nil(err)
	s.Equal(2, len(cc.notaryAcks()))

	// Test the notaryAcks should be the last notaryAck.
	s.Require().Nil(s.db.Put(*blocks[2]))
	s.Require().Nil(s.db.Put(*blocks[3]))
	s.Nil(cc.processNotaryAck(notaryAcks1[3]))

	acks := cc.notaryAcks()
	s.Equal(blocks[3].Hash, acks[vID1].NotaryBlockHash)
	s.Equal(blocks[1].Hash, acks[vID2].NotaryBlockHash)

	// Test that notaryAck on less Notary.Height should be ignored.
	s.Require().Nil(s.db.Put(*blocks[4]))
	s.Require().Nil(s.db.Put(*blocks[5]))
	s.Nil(cc.processNotaryAck(notaryAcks1[5]))
	s.Nil(cc.processNotaryAck(notaryAcks2[5]))
	s.Nil(cc.processNotaryAck(notaryAcks1[4]))
	s.Nil(cc.processNotaryAck(notaryAcks2[4]))

	acks = cc.notaryAcks()
	s.Equal(blocks[5].Hash, acks[vID1].NotaryBlockHash)
	s.Equal(blocks[5].Hash, acks[vID2].NotaryBlockHash)
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
