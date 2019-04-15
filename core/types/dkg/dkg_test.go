// Copyright 2018 The dexon-consensus Authors
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

package dkg

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	cryptoDKG "github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
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

func (s *DKGTestSuite) genID() cryptoDKG.ID {
	dID, err := cryptoDKG.BytesID(s.genRandomBytes())
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
	_, pubShare := cryptoDKG.NewPrivateKeyShares(10)
	d := MasterPublicKey{
		ProposerID:      types.NodeID{Hash: common.Hash{1, 2, 3}},
		Round:           10,
		Reset:           11,
		DKGID:           dID,
		PublicKeyShares: *pubShare.Clone(),
		Signature: crypto.Signature{
			Type:      "123",
			Signature: []byte{4, 5, 6},
		},
	}

	b, err := rlp.EncodeToBytes(&d)
	s.Require().NoError(err)

	var dd MasterPublicKey
	err = rlp.DecodeBytes(b, &dd)
	s.Require().NoError(err)

	bb, err := rlp.EncodeToBytes(&dd)
	s.Require().NoError(err)
	s.Require().True(reflect.DeepEqual(b, bb))
	s.Require().True(d.ProposerID.Equal(dd.ProposerID))
	s.Require().True(d.Round == dd.Round)
	s.Require().True(reflect.DeepEqual(d.Signature, dd.Signature))
	s.Require().Equal(d.DKGID.GetHexString(), dd.DKGID.GetHexString())
	s.Require().True(d.PublicKeyShares.Equal(pubShare))

	// Test DKGPrivateShare.
	p := PrivateShare{
		ProposerID:   types.NodeID{Hash: common.Hash{1, 3, 5}},
		Round:        10,
		Reset:        11,
		PrivateShare: *cryptoDKG.NewPrivateKey(),
		Signature: crypto.Signature{
			Type:      "123",
			Signature: []byte{2, 4, 6},
		},
	}

	b, err = rlp.EncodeToBytes(&p)
	s.Require().NoError(err)

	var pp PrivateShare
	err = rlp.DecodeBytes(b, &pp)
	s.Require().NoError(err)

	bb, err = rlp.EncodeToBytes(&pp)
	s.Require().NoError(err)
	s.Require().True(reflect.DeepEqual(b, bb))
	s.Require().True(p.ProposerID.Equal(pp.ProposerID))
	s.Require().True(p.Round == pp.Round)
	s.Require().True(reflect.DeepEqual(p.PrivateShare, pp.PrivateShare))
	s.Require().True(reflect.DeepEqual(p.Signature, pp.Signature))

	// Test DKG Nack Complaint.
	c := Complaint{
		ProposerID: d.ProposerID,
		Round:      10,
		Reset:      11,
		PrivateShare: PrivateShare{
			ProposerID: p.ProposerID,
			Round:      10,
			Reset:      11,
		},
		Signature: crypto.Signature{
			Type:      "123",
			Signature: []byte{3, 3, 3},
		},
	}
	s.Require().True(c.IsNack())

	b, err = rlp.EncodeToBytes(&c)
	s.Require().NoError(err)

	var cc Complaint
	err = rlp.DecodeBytes(b, &cc)
	s.Require().NoError(err)

	bb, err = rlp.EncodeToBytes(&cc)
	s.Require().NoError(err)
	s.Require().True(reflect.DeepEqual(c, cc))
	s.Require().True(c.ProposerID.Equal(cc.ProposerID))
	s.Require().True(c.Round == cc.Round)
	s.Require().True(reflect.DeepEqual(c.PrivateShare, cc.PrivateShare))
	s.Require().True(reflect.DeepEqual(c.Signature, cc.Signature))

	// Test DKG Complaint.
	c = Complaint{
		ProposerID:   d.ProposerID,
		Round:        10,
		Reset:        11,
		PrivateShare: p,
		Signature: crypto.Signature{
			Type:      "123",
			Signature: []byte{3, 3, 3},
		},
	}
	s.Require().False(c.IsNack())

	b, err = rlp.EncodeToBytes(&c)
	s.Require().NoError(err)

	err = rlp.DecodeBytes(b, &cc)
	s.Require().NoError(err)

	bb, err = rlp.EncodeToBytes(&cc)
	s.Require().NoError(err)
	s.Require().True(reflect.DeepEqual(c, cc))
	s.Require().True(c.ProposerID.Equal(cc.ProposerID))
	s.Require().True(c.Round == cc.Round)
	s.Require().True(reflect.DeepEqual(c.PrivateShare, cc.PrivateShare))
	s.Require().True(reflect.DeepEqual(c.Signature, cc.Signature))
}

func (s *DKGTestSuite) TestMasterPublicKeyEquality() {
	var req = s.Require()
	// Prepare source master public key.
	master1 := &MasterPublicKey{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Round:      1234,
		Reset:      5678,
		DKGID:      s.genID(),
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	prvKey := cryptoDKG.NewPrivateKey()
	pubKey := prvKey.PublicKey().(cryptoDKG.PublicKey)
	_, pubShares := cryptoDKG.NewPrivateKeyShares(2)
	req.NoError(pubShares.AddShare(s.genID(), &pubKey))
	master1.PublicKeyShares = *pubShares.Move()
	// Prepare another master public key by copying every field.
	master2 := &MasterPublicKey{}
	s.clone(master1, master2)
	// They should be equal.
	req.True(master1.Equal(master2))
	// Change round.
	master2.Round = 2345
	req.False(master1.Equal(master2))
	master2.Round = 1234
	// Change reset.
	master2.Reset = 6789
	req.False(master1.Equal(master2))
	master2.Reset = 5678
	// Change proposerID.
	master2.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(master1.Equal(master2))
	master2.ProposerID = master1.ProposerID
	// Change DKGID.
	master2.DKGID = cryptoDKG.NewID(s.genRandomBytes())
	req.False(master1.Equal(master2))
	master2.DKGID = master1.DKGID
	// Change signature.
	master2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(master1.Equal(master2))
	master2.Signature = master1.Signature
	// Change public key share.
	master2.PublicKeyShares = *cryptoDKG.NewEmptyPublicKeyShares()
	req.False(master1.Equal(master2))
}

func (s *DKGTestSuite) TestPrivateShareEquality() {
	var req = s.Require()
	share1 := &PrivateShare{
		ProposerID:   types.NodeID{Hash: common.NewRandomHash()},
		ReceiverID:   types.NodeID{Hash: common.NewRandomHash()},
		Round:        1,
		Reset:        2,
		PrivateShare: *cryptoDKG.NewPrivateKey(),
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy.
	share2 := &PrivateShare{}
	s.clone(share1, share2)
	req.True(share1.Equal(share2))
	// Change proposer ID.
	share2.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(share1.Equal(share2))
	share2.ProposerID = share1.ProposerID
	// Change receiver ID.
	share2.ReceiverID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(share1.Equal(share2))
	share2.ReceiverID = share1.ReceiverID
	// Change round.
	share2.Round = share1.Round + 1
	req.False(share1.Equal(share2))
	share2.Round = share1.Round
	// Change reset.
	share2.Reset = share1.Reset + 1
	req.False(share1.Equal(share2))
	share2.Reset = share1.Reset
	// Change signature.
	share2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(share1.Equal(share2))
	share2.Signature = share1.Signature
	// Change private share.
	share2.PrivateShare = *cryptoDKG.NewPrivateKey()
	req.False(share1.Equal(share2))
	share2.PrivateShare = share1.PrivateShare
	// They should be equal after chaning fields back.
	req.True(share1.Equal(share2))
}

func (s *DKGTestSuite) TestComplaintEquality() {
	var req = s.Require()
	comp1 := &Complaint{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Round:      1,
		Reset:      2,
		PrivateShare: PrivateShare{
			ProposerID:   types.NodeID{Hash: common.NewRandomHash()},
			ReceiverID:   types.NodeID{Hash: common.NewRandomHash()},
			Round:        2,
			Reset:        3,
			PrivateShare: *cryptoDKG.NewPrivateKey(),
			Signature: crypto.Signature{
				Signature: s.genRandomBytes(),
			},
		},
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy.
	comp2 := &Complaint{}
	s.clone(comp1, comp2)
	req.True(comp1.Equal(comp2))
	// Change proposer ID.
	comp2.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(comp1.Equal(comp2))
	comp2.ProposerID = comp1.ProposerID
	// Change round.
	comp2.Round = comp1.Round + 1
	req.False(comp1.Equal(comp2))
	comp2.Round = comp1.Round
	// Change reset.
	comp2.Reset = comp1.Reset + 1
	req.False(comp1.Equal(comp2))
	comp2.Reset = comp1.Reset
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
	// Change share's reset.
	comp2.PrivateShare.Reset = comp1.PrivateShare.Reset + 1
	req.False(comp1.Equal(comp2))
	comp2.PrivateShare.Reset = comp1.PrivateShare.Reset
	// After changing every field back, should be equal.
	req.True(comp1.Equal(comp2))
}

func (s *DKGTestSuite) TestMPKReadyEquality() {
	var req = s.Require()
	ready1 := &MPKReady{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Round:      1,
		Reset:      2,
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy
	ready2 := &MPKReady{}
	s.clone(ready1, ready2)
	req.True(ready1.Equal(ready2))
	// Change proposer ID.
	ready2.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(ready1.Equal(ready2))
	ready2.ProposerID = ready1.ProposerID
	// Change round.
	ready2.Round = ready1.Round + 1
	req.False(ready1.Equal(ready2))
	ready2.Round = ready1.Round
	// Change reset.
	ready2.Reset = ready1.Reset + 1
	req.False(ready1.Equal(ready2))
	ready2.Reset = ready1.Reset
	// Change signature.
	ready2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(ready1.Equal(ready2))
	ready2.Signature = ready1.Signature
	// After changing every field back, they should be equal.
	req.True(ready1.Equal(ready2))
}

func (s *DKGTestSuite) TestFinalizeEquality() {
	var req = s.Require()
	final1 := &Finalize{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Round:      1,
		Reset:      2,
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy
	final2 := &Finalize{}
	s.clone(final1, final2)
	req.True(final1.Equal(final2))
	// Change proposer ID.
	final2.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(final1.Equal(final2))
	final2.ProposerID = final1.ProposerID
	// Change round.
	final2.Round = final1.Round + 1
	req.False(final1.Equal(final2))
	final2.Round = final1.Round
	// Change reset.
	final2.Reset = final1.Reset + 1
	req.False(final1.Equal(final2))
	final2.Reset = final1.Reset
	// Change signature.
	final2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(final1.Equal(final2))
	final2.Signature = final1.Signature
	// After changing every field back, they should be equal.
	req.True(final1.Equal(final2))
}

func (s *DKGTestSuite) TestSuccessEquality() {
	var req = s.Require()
	success1 := &Success{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Round:      1,
		Reset:      2,
		Signature: crypto.Signature{
			Signature: s.genRandomBytes(),
		},
	}
	// Make a copy
	success2 := &Success{}
	s.clone(success1, success2)
	req.True(success1.Equal(success2))
	// Change proposer ID.
	success2.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	req.False(success1.Equal(success2))
	success2.ProposerID = success1.ProposerID
	// Change round.
	success2.Round = success1.Round + 1
	req.False(success1.Equal(success2))
	success2.Round = success1.Round
	// Change reset.
	success2.Reset = success1.Reset + 1
	req.False(success1.Equal(success2))
	success2.Reset = success1.Reset
	// Change signature.
	success2.Signature = crypto.Signature{
		Signature: s.genRandomBytes(),
	}
	req.False(success1.Equal(success2))
	success2.Signature = success1.Signature
	// After changing every field back, they should be equal.
	req.True(success1.Equal(success2))
}

func TestDKG(t *testing.T) {
	suite.Run(t, new(DKGTestSuite))
}
