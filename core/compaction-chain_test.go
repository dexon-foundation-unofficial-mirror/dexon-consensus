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
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
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
			Hash: common.NewRandomHash(),
			Witness: types.Witness{
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
			Witness: types.Witness{
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
			s.Equal(block.Witness.Height, prevBlock.Witness.Height+1)
			prevHash, err := hashWitness(prevBlock)
			s.Require().Nil(err)
			s.Equal(prevHash, block.Witness.ParentHash)
		}
		prevBlock = block
	}
}

func (s *CompactionChainTestSuite) TestProcessWitnessAck() {
	cc := s.newCompactionChain()
	blocks := s.generateBlocks(10, cc)
	prv1, err := ecdsa.NewPrivateKey()
	s.Require().Nil(err)
	prv2, err := ecdsa.NewPrivateKey()
	s.Require().Nil(err)
	nID1 := types.NewNodeID(prv1.PublicKey())
	nID2 := types.NewNodeID(prv2.PublicKey())
	auth1 := NewAuthenticator(prv1)
	auth2 := NewAuthenticator(prv2)
	witnessAcks1 := []*types.WitnessAck{}
	witnessAcks2 := []*types.WitnessAck{}
	for _, block := range blocks {
		cc.prevBlock = block
		witnessAck1, err := auth1.SignAsWitnessAck(block)
		s.Require().Nil(err)
		witnessAck2, err := auth2.SignAsWitnessAck(block)
		s.Require().Nil(err)
		witnessAcks1 = append(witnessAcks1, witnessAck1)
		witnessAcks2 = append(witnessAcks2, witnessAck2)
	}
	// The acked block is not yet in db.
	err = cc.processWitnessAck(witnessAcks1[0])
	s.Nil(err)
	s.Equal(0, len(cc.witnessAcks()))
	err = cc.processWitnessAck(witnessAcks2[1])
	s.Nil(err)
	s.Equal(0, len(cc.witnessAcks()))
	// Insert to block to db and trigger processPendingWitnessAck.
	s.Require().Nil(s.db.Put(*blocks[0]))
	s.Require().Nil(s.db.Put(*blocks[1]))
	err = cc.processWitnessAck(witnessAcks1[2])
	s.Nil(err)
	s.Equal(2, len(cc.witnessAcks()))

	// Test the witnessAcks should be the last witnessAck.
	s.Require().Nil(s.db.Put(*blocks[2]))
	s.Require().Nil(s.db.Put(*blocks[3]))
	s.Nil(cc.processWitnessAck(witnessAcks1[3]))

	acks := cc.witnessAcks()
	s.Equal(blocks[3].Hash, acks[nID1].WitnessBlockHash)
	s.Equal(blocks[1].Hash, acks[nID2].WitnessBlockHash)

	// Test that witnessAck on less Witness.Height should be ignored.
	s.Require().Nil(s.db.Put(*blocks[4]))
	s.Require().Nil(s.db.Put(*blocks[5]))
	s.Nil(cc.processWitnessAck(witnessAcks1[5]))
	s.Nil(cc.processWitnessAck(witnessAcks2[5]))
	s.Nil(cc.processWitnessAck(witnessAcks1[4]))
	s.Nil(cc.processWitnessAck(witnessAcks2[4]))

	acks = cc.witnessAcks()
	s.Equal(blocks[5].Hash, acks[nID1].WitnessBlockHash)
	s.Equal(blocks[5].Hash, acks[nID2].WitnessBlockHash)
}

func TestCompactionChain(t *testing.T) {
	suite.Run(t, new(CompactionChainTestSuite))
}
