// Copyright 2019 The dexon-consensus Authors
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

package utils

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

type PenaltyHelperTestSuite struct {
	suite.Suite
}

func (s *PenaltyHelperTestSuite) TestDKGComplaint() {
	signComplaint := func(prv crypto.PrivateKey, complaint *typesDKG.Complaint) {
		var err error
		complaint.Signature, err = prv.Sign(hashDKGComplaint(complaint))
		s.Require().NoError(err)
	}
	prv1, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	nID1 := types.NewNodeID(prv1.PublicKey())

	prv2, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	nID2 := types.NewNodeID(prv2.PublicKey())

	prvShares, pubShares := dkg.NewPrivateKeyShares(3)
	mpk := &typesDKG.MasterPublicKey{
		ProposerID:      nID1,
		DKGID:           typesDKG.NewID(nID1),
		PublicKeyShares: *pubShares.Move(),
	}
	mpk.Signature, err = prv1.Sign(hashDKGMasterPublicKey(mpk))
	s.Require().NoError(err)

	// NackComplaint should not be penalized.
	complaint := &typesDKG.Complaint{
		ProposerID: nID2,
	}
	signComplaint(prv2, complaint)
	s.Require().True(complaint.IsNack())
	ok, err := NeedPenaltyDKGPrivateShare(complaint, mpk)
	s.Require().NoError(err)
	s.False(ok)

	// Correct privateShare should not be penalized.
	prvShares.SetParticipants(dkg.IDs{typesDKG.NewID(nID1), typesDKG.NewID(nID2)})
	share, exist := prvShares.Share(typesDKG.NewID(nID2))
	s.Require().True(exist)
	prvShare := &typesDKG.PrivateShare{
		ProposerID:   nID1,
		ReceiverID:   nID2,
		PrivateShare: *share,
	}
	prvShare.Signature, err = prv1.Sign(hashDKGPrivateShare(prvShare))
	s.Require().NoError(err)
	complaint.PrivateShare = *prvShare
	signComplaint(prv2, complaint)
	ok, err = NeedPenaltyDKGPrivateShare(complaint, mpk)
	s.Require().NoError(err)
	s.False(ok)

	// Penalize incorrect privateShare.
	share, exist = prvShares.Share(typesDKG.NewID(nID1))
	s.Require().True(exist)
	prvShare.PrivateShare = *share
	prvShare.Signature, err = prv1.Sign(hashDKGPrivateShare(prvShare))
	s.Require().NoError(err)
	complaint.PrivateShare = *prvShare
	signComplaint(prv2, complaint)
	ok, err = NeedPenaltyDKGPrivateShare(complaint, mpk)
	s.Require().NoError(err)
	s.True(ok)

	// Should not penalize if mpk is incorrect.
	mpk.Round++
	ok, err = NeedPenaltyDKGPrivateShare(complaint, mpk)
	s.Equal(ErrInvalidDKGMasterPublicKey, err)

	// Should not penalize if mpk's proposer not match with prvShares'.
	mpk.Round--
	mpk.ProposerID = nID2
	mpk.Signature, err = prv1.Sign(hashDKGMasterPublicKey(mpk))
	s.Require().NoError(err)
	ok, err = NeedPenaltyDKGPrivateShare(complaint, mpk)
	s.Require().NoError(err)
	s.False(ok)
}

func (s *PenaltyHelperTestSuite) TestForkVote() {
	prv1, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)

	vote1 := types.NewVote(types.VoteCom, common.NewRandomHash(), uint64(0))
	vote1.ProposerID = types.NewNodeID(prv1.PublicKey())

	vote2 := vote1.Clone()
	for vote2.BlockHash == vote1.BlockHash {
		vote2.BlockHash = common.NewRandomHash()
	}
	vote1.Signature, err = prv1.Sign(HashVote(vote1))
	s.Require().NoError(err)
	vote2.Signature, err = prv1.Sign(HashVote(vote2))
	s.Require().NoError(err)

	ok, err := NeedPenaltyForkVote(vote1, vote2)
	s.Require().NoError(err)
	s.True(ok)

	// Invalid signature should not be penalized.
	vote2.VoteHeader.Period++
	ok, err = NeedPenaltyForkVote(vote1, vote2)
	s.Require().NoError(err)
	s.False(ok)

	// Period not matched.
	vote2.Signature, err = prv1.Sign(HashVote(vote2))
	s.Require().NoError(err)
	ok, err = NeedPenaltyForkVote(vote1, vote2)
	s.Require().NoError(err)
	s.False(ok)

	// Proposer not matched.
	vote2 = vote1.Clone()
	for vote2.BlockHash == vote1.BlockHash {
		vote2.BlockHash = common.NewRandomHash()
	}
	prv2, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	vote2.ProposerID = types.NewNodeID(prv2.PublicKey())
	vote2.Signature, err = prv2.Sign(HashVote(vote2))
	s.Require().NoError(err)
	ok, err = NeedPenaltyForkVote(vote1, vote2)
	s.Require().NoError(err)
	s.False(ok)
}

func (s *PenaltyHelperTestSuite) TestForkBlock() {
	hashBlock := func(block *types.Block) common.Hash {
		block.PayloadHash = crypto.Keccak256Hash(block.Payload)
		var err error
		block.Hash, err = HashBlock(block)
		s.Require().NoError(err)
		return block.Hash
	}
	prv1, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)

	block1 := &types.Block{
		ProposerID: types.NewNodeID(prv1.PublicKey()),
		ParentHash: common.NewRandomHash(),
	}

	block2 := block1.Clone()
	for block2.ParentHash == block1.ParentHash {
		block2.ParentHash = common.NewRandomHash()
	}
	block1.Signature, err = prv1.Sign(hashBlock(block1))
	s.Require().NoError(err)
	block2.Signature, err = prv1.Sign(hashBlock(block2))
	s.Require().NoError(err)

	ok, err := NeedPenaltyForkBlock(block1, block2)
	s.Require().NoError(err)
	s.True(ok)

	// Invalid signature should not be penalized.
	block2.ParentHash[0]++
	ok, err = NeedPenaltyForkBlock(block1, block2)
	s.Require().NoError(err)
	s.False(ok)

	// Position not matched.
	block2.Position.Height++
	block2.Signature, err = prv1.Sign(hashBlock(block2))
	s.Require().NoError(err)
	ok, err = NeedPenaltyForkBlock(block1, block2)
	s.Require().NoError(err)
	s.False(ok)

	// Proposer not matched.
	block2 = block1.Clone()
	for block2.ParentHash == block1.ParentHash {
		block2.ParentHash = common.NewRandomHash()
	}
	prv2, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	block2.ProposerID = types.NewNodeID(prv2.PublicKey())
	block2.Signature, err = prv2.Sign(hashBlock(block2))
	s.Require().NoError(err)
	ok, err = NeedPenaltyForkBlock(block1, block2)
	s.Require().NoError(err)
	s.False(ok)
}

func TestPenaltyHelper(t *testing.T) {
	suite.Run(t, new(PenaltyHelperTestSuite))
}
