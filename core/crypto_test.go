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
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/eth"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type CryptoTestSuite struct {
	suite.Suite
}

var myNID = types.NodeID{Hash: common.NewRandomHash()}

func (s *CryptoTestSuite) prepareBlock(prevBlock *types.Block) *types.Block {
	acks := common.Hashes{}
	now := time.Now().UTC()
	if prevBlock == nil {
		return &types.Block{
			Acks:      common.NewSortedHashes(acks),
			Timestamp: now,
			Witness: types.Witness{
				Timestamp: time.Now(),
				Height:    0,
			},
		}
	}
	parentHash, err := hashWitness(prevBlock)
	s.Require().Nil(err)
	s.Require().NotEqual(prevBlock.Hash, common.Hash{})
	acks = append(acks, parentHash)
	return &types.Block{
		ParentHash: prevBlock.Hash,
		Acks:       common.NewSortedHashes(acks),
		Timestamp:  now,
		Position: types.Position{
			Height: prevBlock.Position.Height + 1,
		},
		Witness: types.Witness{
			ParentHash: parentHash,
			Timestamp:  time.Now(),
			Height:     prevBlock.Witness.Height + 1,
		},
	}
}

func (s *CryptoTestSuite) newBlock(prevBlock *types.Block) *types.Block {
	block := s.prepareBlock(prevBlock)
	var err error
	block.Hash, err = hashBlock(block)
	s.Require().Nil(err)
	return block
}

func (s *CryptoTestSuite) generateCompactionChain(
	length int, prv crypto.PrivateKey) (
	[]*types.Block, []types.WitnessAck) {
	blocks := make([]*types.Block, length)
	witnessAcks := make([]types.WitnessAck, length)
	var prevBlock *types.Block
	for idx := range blocks {
		block := s.newBlock(prevBlock)
		prevBlock = block
		blocks[idx] = block
		var err error
		witnessAcks[idx].Hash, err = hashWitness(blocks[idx])
		s.Require().Nil(err)
		witnessAcks[idx].WitnessBlockHash = blocks[idx].Hash
		witnessAcks[idx].Signature, err = prv.Sign(witnessAcks[idx].Hash)
		s.Require().Nil(err)
		if idx > 0 {
			block.Witness.ParentHash = witnessAcks[idx-1].Hash
		}
	}
	return blocks, witnessAcks
}

func (s *CryptoTestSuite) TestWitnessAckSignature() {
	prv, err := eth.NewPrivateKey()
	pub := prv.PublicKey()
	s.Require().Nil(err)
	blocks, witnessAcks := s.generateCompactionChain(10, prv)
	blockMap := make(map[common.Hash]*types.Block)
	for _, block := range blocks {
		blockMap[block.Hash] = block
	}
	parentBlock := blocks[0]
	for _, witnessAck := range witnessAcks {
		witnessBlock, exist := blockMap[witnessAck.WitnessBlockHash]
		s.Require().True(exist)
		if witnessBlock.Witness.Height == 0 {
			continue
		}
		s.True(parentBlock.Witness.Height == witnessBlock.Witness.Height-1)
		hash, err := hashWitness(parentBlock)
		s.Require().Nil(err)
		s.Equal(hash, witnessBlock.Witness.ParentHash)
		s.True(verifyWitnessSignature(
			pub, witnessBlock, witnessAck.Signature))
		parentBlock = witnessBlock

	}
	// Modify Block.Witness.Timestamp and verify signature again.
	for _, witnessAck := range witnessAcks {
		block, exist := blockMap[witnessAck.WitnessBlockHash]
		s.Require().True(exist)
		block.Witness.Timestamp = time.Time{}
		ackingBlock, exist := blockMap[witnessAck.WitnessBlockHash]
		s.Require().True(exist)
		s.False(verifyWitnessSignature(
			pub, ackingBlock, witnessAck.Signature))
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
			s.True(parentBlock.Position.Height == block.Position.Height-1)
			hash, err := hashBlock(parentBlock)
			s.Require().Nil(err)
			s.Equal(hash, block.ParentHash)
		}
		s.True(verifyBlockSignature(pub, block, block.Signature))
	}
	// Modify Block.Acks and verify signature again.
	for _, block := range blocks {
		block.Acks = append(block.Acks, common.NewRandomHash())
		s.False(verifyBlockSignature(
			pub, block, block.Signature))
	}
}

func (s *CryptoTestSuite) TestVoteSignature() {
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	pub := prv.PublicKey()
	nID := types.NewNodeID(pub)
	vote := &types.Vote{
		ProposerID: nID,
		Type:       types.VoteAck,
		BlockHash:  common.NewRandomHash(),
		Period:     1,
	}
	vote.Signature, err = prv.Sign(hashVote(vote))
	s.Require().Nil(err)
	s.True(verifyVoteSignature(vote))
	vote.Type = types.VoteConfirm
	s.False(verifyVoteSignature(vote))
}

func (s *CryptoTestSuite) TestCRSSignature() {
	crs := common.NewRandomHash()
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	pub := prv.PublicKey()
	nID := types.NewNodeID(pub)
	block := &types.Block{
		ProposerID: nID,
	}
	block.CRSSignature, err = prv.Sign(hashCRS(block, crs))
	s.Require().Nil(err)
	s.True(verifyCRSSignature(block, crs))
	block.Position.Height++
	s.False(verifyCRSSignature(block, crs))
}

func (s *CryptoTestSuite) TestDKGSignature() {
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	nID := types.NewNodeID(prv.PublicKey())
	prvShare := &types.DKGPrivateShare{
		ProposerID:   nID,
		Round:        5,
		PrivateShare: *dkg.NewPrivateKey(),
	}
	prvShare.Signature, err = prv.Sign(hashDKGPrivateShare(prvShare))
	s.Require().Nil(err)
	s.True(verifyDKGPrivateShareSignature(prvShare))
	prvShare.Round++
	s.False(verifyDKGPrivateShareSignature(prvShare))

	id := dkg.NewID([]byte{13})
	_, pkShare := dkg.NewPrivateKeyShares(1)
	mpk := &types.DKGMasterPublicKey{
		ProposerID:      nID,
		Round:           5,
		DKGID:           id,
		PublicKeyShares: *pkShare,
	}
	mpk.Signature, err = prv.Sign(hashDKGMasterPublicKey(mpk))
	s.Require().Nil(err)
	s.True(verifyDKGMasterPublicKeySignature(mpk))
	mpk.Round++
	s.False(verifyDKGMasterPublicKeySignature(mpk))

	complaint := &types.DKGComplaint{
		ProposerID:   nID,
		Round:        5,
		PrivateShare: *prvShare,
	}
	complaint.Signature, err = prv.Sign(hashDKGComplaint(complaint))
	s.Require().Nil(err)
	s.True(verifyDKGComplaintSignature(complaint))
	complaint.Round++
	s.False(verifyDKGComplaintSignature(complaint))

	sig := &types.DKGPartialSignature{
		ProposerID:       nID,
		Round:            5,
		PartialSignature: dkg.PartialSignature{},
	}
	sig.Signature, err = prv.Sign(hashDKGPartialSignature(sig))
	s.Require().Nil(err)
	s.True(verifyDKGPartialSignatureSignature(sig))
	sig.Round++
	s.False(verifyDKGPartialSignatureSignature(sig))
}

func TestCrypto(t *testing.T) {
	suite.Run(t, new(CryptoTestSuite))
}
