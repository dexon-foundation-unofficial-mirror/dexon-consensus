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

package types

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/dkg"
	"github.com/dexon-foundation/dexon/rlp"
)

type DKGTestSuite struct {
	suite.Suite
}

func (s *DKGTestSuite) genRandomBytes() []byte {
	randomness := make([]byte, 32)
	_, err := rand.Read(randomness)
	s.Require().NoError(err)
	return randomness
}

func (s *DKGTestSuite) genID() dkg.ID {
	dID, err := dkg.BytesID(s.genRandomBytes())
	s.Require().NoError(err)
	return dID
}

func (s *DKGTestSuite) clone(src, dst interface{}) {
	b, err := rlp.EncodeToBytes(src)
	s.Require().NoError(err)
	s.Require().NoError(rlp.DecodeBytes(b, dst))
}

func (s *DKGTestSuite) TestRLPEncodeDecode() {
	dID := s.genID()
	// Prepare master public key for testing.
	d := DKGMasterPublicKey{
		ProposerID: NodeID{common.Hash{1, 2, 3}},
		Round:      10,
		DKGID:      dID,
		Signature: crypto.Signature{
			Type:      "123",
			Signature: []byte{4, 5, 6},
		},
	}

	b, err := rlp.EncodeToBytes(&d)
	s.Require().NoError(err)

	var dd DKGMasterPublicKey
	err = rlp.DecodeBytes(b, &dd)
	s.Require().NoError(err)

	bb, err := rlp.EncodeToBytes(&dd)
	s.Require().NoError(err)
	s.Require().True(reflect.DeepEqual(b, bb))
	s.Require().True(d.ProposerID.Equal(dd.ProposerID))
	s.Require().True(d.Round == dd.Round)
	s.Require().True(reflect.DeepEqual(d.Signature, dd.Signature))
	s.Require().Equal(d.DKGID.GetHexString(), dd.DKGID.GetHexString())
}

func (s *DKGTestSuite) TestDKGMasterPublicKeyEquality() {
	var req = s.Require()
	// Prepare source master public key.
	master1 := &DKGMasterPublicKey{
		ProposerID: NodeID{Hash: common.NewRandomHash()},
		Round:      1234,
		DKGID:      s.genID(),
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	prvKey := dkg.NewPrivateKey()
	pubKey := prvKey.PublicKey().(dkg.PublicKey)
	_, pubShares := dkg.NewPrivateKeyShares(2)
	req.NoError(pubShares.AddShare(s.genID(), &pubKey))
	master1.PublicKeyShares = *pubShares
	// Prepare another master public key by copying every field.
	master2 := &DKGMasterPublicKey{}
	s.clone(master1, master2)
	// They should be equal.
	req.True(master1.Equal(master2))
	// Change round.
	master2.Round = 2345
	req.False(master1.Equal(master2))
	master2.Round = 1234
	// Change proposerID.
	master2.ProposerID = NodeID{Hash: common.NewRandomHash()}
	req.False(master1.Equal(master2))
	master2.ProposerID = master1.ProposerID
	// Change DKGID.
	master2.DKGID = dkg.NewID(s.genRandomBytes())
	req.False(master1.Equal(master2))
	master2.DKGID = master1.DKGID
	// Change signature.
	master2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(master1.Equal(master2))
	master2.Signature = master1.Signature
	// Change public key share.
	master2.PublicKeyShares = *dkg.NewEmptyPublicKeyShares()
	req.False(master1.Equal(master2))
}

func (s *DKGTestSuite) TestDKGPrivateShareEquality() {
	var req = s.Require()
	share1 := &DKGPrivateShare{
		ProposerID:   NodeID{Hash: common.NewRandomHash()},
		ReceiverID:   NodeID{Hash: common.NewRandomHash()},
		Round:        1,
		PrivateShare: *dkg.NewPrivateKey(),
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy.
	share2 := &DKGPrivateShare{}
	s.clone(share1, share2)
	req.True(share1.Equal(share2))
	// Change proposer ID.
	share2.ProposerID = NodeID{Hash: common.NewRandomHash()}
	req.False(share1.Equal(share2))
	share2.ProposerID = share1.ProposerID
	// Change receiver ID.
	share2.ReceiverID = NodeID{Hash: common.NewRandomHash()}
	req.False(share1.Equal(share2))
	share2.ReceiverID = share1.ReceiverID
	// Change round.
	share2.Round = share1.Round + 1
	req.False(share1.Equal(share2))
	share2.Round = share1.Round
	// Change signature.
	share2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(share1.Equal(share2))
	share2.Signature = share1.Signature
	// Change private share.
	share2.PrivateShare = *dkg.NewPrivateKey()
	req.False(share1.Equal(share2))
	share2.PrivateShare = share1.PrivateShare
	// They should be equal after chaning fields back.
	req.True(share1.Equal(share2))
}

func (s *DKGTestSuite) TestDKGComplaintEquality() {
	var req = s.Require()
	comp1 := &DKGComplaint{
		ProposerID: NodeID{Hash: common.NewRandomHash()},
		Round:      1,
		PrivateShare: DKGPrivateShare{
			ProposerID:   NodeID{Hash: common.NewRandomHash()},
			ReceiverID:   NodeID{Hash: common.NewRandomHash()},
			Round:        2,
			PrivateShare: *dkg.NewPrivateKey(),
			Signature: crypto.Signature{
				Signature: s.genRandomBytes(),
			},
		},
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy.
	comp2 := &DKGComplaint{}
	s.clone(comp1, comp2)
	req.True(comp1.Equal(comp2))
	// Change proposer ID.
	comp2.ProposerID = NodeID{Hash: common.NewRandomHash()}
	req.False(comp1.Equal(comp2))
	comp2.ProposerID = comp1.ProposerID
	// Change round.
	comp2.Round = comp1.Round + 1
	req.False(comp1.Equal(comp2))
	comp2.Round = comp1.Round
	// Change signature.
	comp2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(comp1.Equal(comp2))
	comp2.Signature = comp1.Signature
	// Change share's round.
	comp2.PrivateShare.Round = comp1.PrivateShare.Round + 1
	req.False(comp1.Equal(comp2))
	comp2.PrivateShare.Round = comp1.PrivateShare.Round
	// After changing every field back, should be equal.
	req.True(comp1.Equal(comp2))
}

func (s *DKGTestSuite) TestDKGFinalizeEquality() {
	var req = s.Require()
	final1 := &DKGFinalize{
		ProposerID: NodeID{Hash: common.NewRandomHash()},
		Round:      1,
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy
	final2 := &DKGFinalize{}
	s.clone(final1, final2)
	req.True(final1.Equal(final2))
	// Change proposer ID.
	final2.ProposerID = NodeID{Hash: common.NewRandomHash()}
	req.False(final1.Equal(final2))
	final2.ProposerID = final1.ProposerID
	// Change round.
	final2.Round = final1.Round + 1
	req.False(final1.Equal(final2))
	final2.Round = final1.Round
	// Change signature.
	final2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(final1.Equal(final2))
	final2.Signature = final1.Signature
	// After changing every field back, they should be equal.
	req.True(final1.Equal(final2))
}

func TestDKG(t *testing.T) {
	suite.Run(t, new(DKGTestSuite))
}
