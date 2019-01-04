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

	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

type UtilsTestSuite struct {
	suite.Suite
}

func (s *UtilsTestSuite) TestVerifyDKGComplaint() {
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
		PublicKeyShares: *pubShares,
	}
	mpk.Signature, err = prv1.Sign(hashDKGMasterPublicKey(mpk))
	s.Require().NoError(err)

	// Valid NackComplaint.
	complaint := &typesDKG.Complaint{
		ProposerID: nID2,
	}
	signComplaint(prv2, complaint)
	s.Require().True(complaint.IsNack())
	ok, err := VerifyDKGComplaint(complaint, mpk)
	s.Require().NoError(err)
	s.True(ok)

	// Correct privateShare.
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
	ok, err = VerifyDKGComplaint(complaint, mpk)
	s.Require().NoError(err)
	s.True(ok)

	// Incorrect privateShare.
	share, exist = prvShares.Share(typesDKG.NewID(nID1))
	s.Require().True(exist)
	prvShare.PrivateShare = *share
	prvShare.Signature, err = prv1.Sign(hashDKGPrivateShare(prvShare))
	s.Require().NoError(err)
	complaint.PrivateShare = *prvShare
	signComplaint(prv2, complaint)
	ok, err = VerifyDKGComplaint(complaint, mpk)
	s.Require().NoError(err)
	s.False(ok)

	// MPK is incorrect.
	mpk.Round++
	ok, err = VerifyDKGComplaint(complaint, mpk)
	s.Require().NoError(err)
	s.False(ok)

	// MPK's proposer not match with prvShares'.
	mpk.Round--
	mpk.ProposerID = nID2
	mpk.Signature, err = prv1.Sign(hashDKGMasterPublicKey(mpk))
	s.Require().NoError(err)
	ok, err = VerifyDKGComplaint(complaint, mpk)
	s.Require().NoError(err)
	s.False(ok)
}

func TestUtils(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
