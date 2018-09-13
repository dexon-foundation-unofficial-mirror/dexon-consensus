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

package dkg

import (
	"encoding/binary"
	"math/rand"
	"sort"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

type DKGTestSuite struct {
	suite.Suite
}

type member struct {
	id                ID
	prvShares         *PrivateKeyShares
	pubShares         *PublicKeyShares
	receivedPrvShares *PrivateKeyShares
	receivedPubShares map[ID]*PublicKeyShares
}

func (s *DKGTestSuite) genID(k int) IDs {
	IDs := make(IDs, 0, k)
	for i := 0; i < k; i++ {
		id := make([]byte, 8)
		binary.LittleEndian.PutUint64(id, rand.Uint64())
		IDs = append(IDs, NewID(id))
	}
	return IDs
}

func (s *DKGTestSuite) sendKey(senders []member, receivers []member) {
	for _, sender := range senders {
		for _, receiver := range receivers {
			// Here's the demonstration of DKG protocol. `pubShares` is broadcasted
			// and all the receiver would save it to the `receivedPubShares`.
			// Do not optimize the memory usage of this part.
			receiver.receivedPubShares[sender.id] = sender.pubShares
			prvShare, ok := sender.prvShares.Share(receiver.id)
			s.Require().True(ok)
			pubShare, err := sender.pubShares.Share(receiver.id)
			s.Require().NoError(err)
			valid, err := receiver.receivedPubShares[sender.id].
				VerifyPrvShare(receiver.id, prvShare)
			s.Require().NoError(err)
			s.Require().True(valid)
			valid, err = receiver.receivedPubShares[sender.id].
				VerifyPubShare(receiver.id, pubShare)
			s.Require().NoError(err)
			s.Require().True(valid)
			err = receiver.receivedPrvShares.AddShare(sender.id, prvShare)
			s.Require().NoError(err)
		}
	}
}

func (s *DKGTestSuite) signWithQualifyIDs(
	member member, qualifyIDs IDs, hash common.Hash) PartialSignature {
	prvKey, err := member.receivedPrvShares.RecoverPrivateKey(qualifyIDs)
	s.Require().NoError(err)
	sig, err := prvKey.Sign(hash)
	s.Require().NoError(err)
	return PartialSignature(sig)
}

func (s *DKGTestSuite) verifySigWithQualifyIDs(
	members []member, qualifyIDs IDs,
	signer ID, hash common.Hash, sig PartialSignature) bool {
	membersIdx := make(map[ID]int)
	for idx, member := range members {
		membersIdx[member.id] = idx
	}
	pubShares := NewEmptyPublicKeyShares()
	for _, id := range qualifyIDs {
		idx, exist := membersIdx[id]
		s.Require().True(exist)
		member := members[idx]
		pubShare, err := member.pubShares.Share(signer)
		s.Require().NoError(err)
		err = pubShares.AddShare(id, pubShare)
		s.Require().NoError(err)
	}
	pubKey, err := pubShares.RecoverPublicKey(qualifyIDs)
	s.Require().NoError(err)
	return pubKey.VerifySignature(hash, crypto.Signature(sig))
}

func (s *DKGTestSuite) TestVerifyKeyShares() {
	invalidID := NewID([]byte{0})
	ids := []ID{NewID([]byte{1}), NewID([]byte{2}), NewID([]byte{3})}
	members := []member{}
	for _, id := range ids {
		members = append(members, member{
			id:                id,
			receivedPubShares: make(map[ID]*PublicKeyShares),
		})
	}

	prvShares, pubShares := NewPrivateKeyShares(2, ids)

	_, ok := prvShares.Share(invalidID)
	s.False(ok)
	for _, id := range ids {
		prvShare, ok := prvShares.Share(id)
		s.Require().True(ok)
		valid, err := pubShares.VerifyPrvShare(id, prvShare)
		s.Require().NoError(err)
		s.True(valid)
		pubShare, err := pubShares.Share(id)
		s.Require().NoError(err)
		valid, err = pubShares.VerifyPubShare(id, pubShare)
		s.Require().NoError(err)
		s.True(valid)
	}

	// Test of faulty private/public key.
	invalidPrvShare := NewPrivateKey()
	valid, err := pubShares.VerifyPrvShare(ids[0], invalidPrvShare)
	s.Require().NoError(err)
	s.False(valid)

	invalidPubShare, ok := invalidPrvShare.PublicKey().(PublicKey)
	s.Require().True(ok)
	valid, err = pubShares.VerifyPubShare(ids[0], &invalidPubShare)
	s.Require().NoError(err)
	s.False(valid)

	// Test of faulty signature.
	for idx := range members {
		members[idx].prvShares, members[idx].pubShares = NewPrivateKeyShares(2, ids)
		members[idx].receivedPrvShares = NewEmptyPrivateKeyShares()
	}
	s.sendKey(members, members)
	hash := crypto.Keccak256Hash([]byte("👾👾👾👾👾👾"))
	sig, err := invalidPrvShare.Sign(hash)
	s.Require().NoError(err)
	psig := PartialSignature(sig)
	for _, member := range members {
		valid = s.verifySigWithQualifyIDs(members, ids, member.id, hash, psig)
		s.False(valid)
	}

	// Test of faulty group signature.
	groupPubShares := make([]*PublicKeyShares, 0, len(members))
	sigs := make([]PartialSignature, 0, len(members))
	for _, member := range members {
		sigs = append(sigs, s.signWithQualifyIDs(member, ids, hash))
		groupPubShares = append(groupPubShares, member.pubShares)
	}
	sigs[0] = psig
	recoverSig, err := RecoverSignature(sigs, ids)
	s.Require().NoError(err)

	pubKey := RecoverGroupPublicKey(groupPubShares)
	s.False(pubKey.VerifySignature(hash, recoverSig))
}

func (s *DKGTestSuite) TestDKGProtocol() {
	k := 5
	members := []member{}
	ids := s.genID((k + 1) * 2)
	for _, id := range ids {
		members = append(members, member{
			id:                id,
			receivedPubShares: make(map[ID]*PublicKeyShares),
		})
	}

	for idx := range members {
		members[idx].prvShares, members[idx].pubShares = NewPrivateKeyShares(k, ids)
		members[idx].receivedPrvShares = NewEmptyPrivateKeyShares()
	}
	// Randomly select non-disqualified members.
	nums := make([]int, len(members))
	for i := range nums {
		nums[i] = i
	}
	rand.Shuffle(len(nums), func(i, j int) {
		nums[i], nums[j] = nums[j], nums[i]
	})
	nums = nums[:rand.Intn(len(members))]
	sort.Ints(nums)
	qualify := make([]member, 0, len(nums))
	for _, idx := range nums {
		qualify = append(qualify, members[idx])
	}
	// TODO(jimmy-dexon): Remove below line after finishing test of random select.
	qualify = members
	// Members are partitioned into two groups.
	grp1, grp2 := members[:k+1], members[k+1:]
	collectIDs := func(members []member) IDs {
		IDs := make(IDs, 0, len(members))
		for _, member := range members {
			IDs = append(IDs, member.id)
		}
		return IDs
	}
	signMsg := func(
		members []member, qualify []member, hash common.Hash) []PartialSignature {
		ids := collectIDs(qualify)
		sigs := make([]PartialSignature, 0, len(members))
		for _, member := range members {
			sig := s.signWithQualifyIDs(member, ids, hash)
			sigs = append(sigs, sig)
		}
		return sigs
	}
	verifySig := func(
		members []member,
		signer []ID, sig []PartialSignature, qualify []member, hash common.Hash) bool {
		ids := collectIDs(qualify)
		for i := range sig {
			if !s.verifySigWithQualifyIDs(members, ids, signer[i], hash, sig[i]) {
				return false
			}
		}
		return true
	}
	s.sendKey(qualify, grp1)
	s.sendKey(qualify, grp2)
	hash := crypto.Keccak256Hash([]byte("🛫"))
	sig1 := signMsg(grp1, qualify, hash)
	sig2 := signMsg(grp2, qualify, hash)
	s.True(verifySig(members, collectIDs(grp1), sig1, qualify, hash))
	s.True(verifySig(members, collectIDs(grp2), sig2, qualify, hash))
	recoverSig1, err := RecoverSignature(sig1, collectIDs(grp1))
	s.Require().NoError(err)
	recoverSig2, err := RecoverSignature(sig2, collectIDs(grp2))
	s.Require().NoError(err)
	s.Equal(recoverSig1, recoverSig2)

	pubShares := make([]*PublicKeyShares, 0, len(members))
	for _, member := range members {
		pubShares = append(pubShares, member.pubShares)
	}
	groupPK := RecoverGroupPublicKey(pubShares)
	s.True(groupPK.VerifySignature(hash, recoverSig1))
	s.True(groupPK.VerifySignature(hash, recoverSig2))
}

func TestDKG(t *testing.T) {
	suite.Run(t, new(DKGTestSuite))
}
