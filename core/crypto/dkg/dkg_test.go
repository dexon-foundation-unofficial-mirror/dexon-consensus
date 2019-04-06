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
	"encoding/binary"
	"math/rand"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/dexon-foundation/bls/ffi/go/bls"
	"github.com/dexon-foundation/dexon/rlp"
	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
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
	receiveFrom := make(map[ID][]member)
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
			receiveFrom[receiver.id] = append(receiveFrom[receiver.id], sender)
		}
	}
	// The received order do not need to be the same.
	for _, receiver := range receivers {
		rand.Shuffle(len(senders), func(i, j int) {
			receiveFrom[receiver.id][i], receiveFrom[receiver.id][j] =
				receiveFrom[receiver.id][j], receiveFrom[receiver.id][i]
		})
		for _, sender := range receiveFrom[receiver.id] {
			prvShare, ok := sender.prvShares.Share(receiver.id)
			s.Require().True(ok)
			err := receiver.receivedPrvShares.AddShare(sender.id, prvShare)
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

	prvShares, pubShares := NewPrivateKeyShares(2)
	prvShares.SetParticipants(ids)

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
		members[idx].prvShares, members[idx].pubShares = NewPrivateKeyShares(2)
		members[idx].prvShares.SetParticipants(ids)
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
		members[idx].prvShares, members[idx].pubShares = NewPrivateKeyShares(k)
		members[idx].prvShares.SetParticipants(ids)
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

func (s *DKGTestSuite) TestSignature() {
	prvKey := NewPrivateKey()
	pubKey := prvKey.PublicKey()
	hash := crypto.Keccak256Hash([]byte("🛫"))
	sig, err := prvKey.Sign(hash)
	s.Require().NoError(err)
	s.True(pubKey.VerifySignature(hash, sig))
	sig.Signature[0]++
	s.False(pubKey.VerifySignature(hash, sig))
	sig = crypto.Signature{}
	s.False(pubKey.VerifySignature(hash, sig))
}

func (s *DKGTestSuite) TestPrivateKeyRLPEncodeDecode() {
	k := NewPrivateKey()
	b, err := rlp.EncodeToBytes(k)
	s.Require().NoError(err)

	var kk PrivateKey
	err = rlp.DecodeBytes(b, &kk)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(*k, kk))
}

func (s *DKGTestSuite) TestPublicKeySharesRLPEncodeDecode() {
	p := NewEmptyPublicKeyShares()
	for _, id := range s.genID(1) {
		privkey := NewPrivateKey()
		pubkey := privkey.PublicKey().(PublicKey)
		p.AddShare(id, &pubkey)
		p.masterPublicKey = append(p.masterPublicKey, pubkey.publicKey)
	}

	b, err := rlp.EncodeToBytes(p)
	s.Require().NoError(err)

	var pp PublicKeyShares
	err = rlp.DecodeBytes(b, &pp)
	s.Require().NoError(err)

	bb, err := rlp.EncodeToBytes(&pp)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(b, bb))
}

func (s *DKGTestSuite) TestPrivateKeySharesRLPEncodeDecode() {
	privShares, _ := NewPrivateKeyShares(10)
	privShares.shares = append(privShares.shares, PrivateKey{})
	privShares.shareIndex = map[ID]int{
		ID{}: 0,
	}

	b, err := rlp.EncodeToBytes(privShares)
	s.Require().NoError(err)

	var newPrivShares PrivateKeyShares
	err = rlp.DecodeBytes(b, &newPrivShares)
	s.Require().NoError(err)

	bb, err := rlp.EncodeToBytes(&newPrivShares)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(b, bb))
	s.Require().True(privShares.Equal(&newPrivShares))
}

func (s *DKGTestSuite) TestPublicKeySharesEquality() {
	var req = s.Require()
	IDs := s.genID(2)
	_, pubShares1 := NewPrivateKeyShares(4)
	// Make a copy from an empty share.
	pubShares2 := pubShares1.Clone()
	req.True(pubShares1.Equal(pubShares2))
	// Add two shares.
	prvKey1 := NewPrivateKey()
	pubKey1 := prvKey1.PublicKey().(PublicKey)
	req.NoError(pubShares1.AddShare(IDs[0], &pubKey1))
	prvKey2 := NewPrivateKey()
	pubKey2 := prvKey2.PublicKey().(PublicKey)
	req.True(pubShares1.Equal(pubShares2))
	// Clone the shares.
	req.NoError(pubShares2.AddShare(IDs[0], &pubKey1))
	req.NoError(pubShares2.AddShare(IDs[1], &pubKey2))
	// They should be equal now.
	req.True(pubShares1.Equal(pubShares2))
	req.True(pubShares2.Equal(pubShares1))
}

func (s *DKGTestSuite) TestPublicKeySharesMove() {
	var req = s.Require()
	IDs := s.genID(2)
	_, pubShares1 := NewPrivateKeyShares(4)
	// Make a copy from an empty share.
	pubShares2 := pubShares1.Clone()
	req.True(pubShares1.Equal(pubShares2))
	// Move from pubShare1.
	pubShares3 := pubShares1.Move()
	// Add two shares.
	prvKey1 := NewPrivateKey()
	pubKey1 := prvKey1.PublicKey().(PublicKey)
	req.NoError(pubShares3.AddShare(IDs[0], &pubKey1))
	prvKey2 := NewPrivateKey()
	pubKey2 := prvKey2.PublicKey().(PublicKey)
	req.True(pubShares3.Equal(pubShares2))
	// Clone the shares.
	req.NoError(pubShares2.AddShare(IDs[0], &pubKey1))
	req.NoError(pubShares2.AddShare(IDs[1], &pubKey2))
	// They should be equal now.
	req.True(pubShares3.Equal(pubShares2))
	req.True(pubShares2.Equal(pubShares3))
}

func (s *DKGTestSuite) TestPublicKeySharesConcurrent() {
	t := 5
	n := 10
	IDs := make(IDs, n)
	for i := range IDs {
		id := common.NewRandomHash()
		IDs[i] = NewID(id[:])
	}
	_, pubShare := NewPrivateKeyShares(t)
	for _, id := range IDs {
		go pubShare.Share(id)
	}
}

func (s *DKGTestSuite) TestPrivateKeySharesEquality() {
	var req = s.Require()
	IDs := s.genID(2)
	prvShares1, _ := NewPrivateKeyShares(4)
	// Make a copy of empty share.
	prvShares2 := NewEmptyPrivateKeyShares()
	req.False(prvShares1.Equal(prvShares2))
	// Clone the master private key.
	for _, m := range prvShares1.masterPrivateKey {
		var key bls.SecretKey
		req.NoError(key.SetLittleEndian(m.GetLittleEndian()))
		prvShares2.masterPrivateKey = append(prvShares2.masterPrivateKey, key)
	}
	// Add two shares.
	prvKey1 := NewPrivateKey()
	req.NoError(prvShares1.AddShare(IDs[0], prvKey1))
	prvKey2 := NewPrivateKey()
	req.NoError(prvShares1.AddShare(IDs[1], prvKey2))
	// They are not equal now.
	req.False(prvShares1.Equal(prvShares2))
	// Clone the shares.
	req.NoError(prvShares2.AddShare(IDs[0], prvKey1))
	req.NoError(prvShares2.AddShare(IDs[1], prvKey2))
	// They should be equal now.
	req.True(prvShares1.Equal(prvShares2))
	req.True(prvShares2.Equal(prvShares1))
}

func (s *DKGTestSuite) TestPublicKeySharesClone() {
	_, pubShares1 := NewPrivateKeyShares(4)
	IDs := s.genID(2)
	prvKey1 := NewPrivateKey()
	pubKey1 := prvKey1.PublicKey().(PublicKey)
	s.Require().NoError(pubShares1.AddShare(IDs[0], &pubKey1))
	pubShares2 := pubShares1.Clone()
	s.Require().True(pubShares1.Equal(pubShares2))
}

func TestDKG(t *testing.T) {
	suite.Run(t, new(DKGTestSuite))
}

func BenchmarkDKGProtocol(b *testing.B) {
	t := 33
	n := 100
	s := new(DKGTestSuite)

	self := member{}
	members := make([]*member, n-1)
	ids := make(IDs, n)

	b.Run("DKG", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			self.id = s.genID(1)[0]
			self.receivedPubShares = make(map[ID]*PublicKeyShares, n)
			for idx, id := range s.genID(n - 1) {
				ids[idx] = id
			}
			for idx := range members {
				members[idx] = &member{
					id:                ids[idx],
					receivedPubShares: make(map[ID]*PublicKeyShares),
					receivedPrvShares: NewEmptyPrivateKeyShares(),
				}
			}
			ids[n-1] = self.id
			prvShares := make(map[ID]*PrivateKey, n)
			for idx := range members {
				members[idx].prvShares, members[idx].pubShares = NewPrivateKeyShares(t)
				members[idx].prvShares.SetParticipants(ids)
				prvShare, ok := members[idx].prvShares.Share(self.id)
				if !ok {
					b.FailNow()
				}
				prvShares[members[idx].id] = prvShare
			}

			b.StartTimer()
			self.prvShares, self.pubShares = NewPrivateKeyShares(t)
			self.prvShares.SetParticipants(ids)
			self.receivedPrvShares = NewEmptyPrivateKeyShares()
			for _, member := range members {
				self.receivedPubShares[member.id] = member.pubShares
			}
			self.receivedPubShares[self.id] = self.pubShares
			prvShare, ok := self.prvShares.Share(self.id)
			if !ok {
				b.FailNow()
			}
			prvShares[self.id] = prvShare
			for id, prvShare := range prvShares {
				ok, err := self.receivedPubShares[id].VerifyPrvShare(self.id, prvShare)
				if err != nil {
					b.Fatalf("%v", err)
				}
				if !ok {
					b.FailNow()
				}
				if err := self.receivedPrvShares.AddShare(id, prvShare); err != nil {
					b.Fatalf("%v", err)
				}
			}
			if _, err := self.receivedPrvShares.RecoverPrivateKey(ids); err != nil {
				b.Fatalf("%v", err)
			}
		}
	})

	hash := crypto.Keccak256Hash([]byte("🏖"))
	b.Run("Share-Sign", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			b.StopTimer()
			prvKey, err := self.receivedPrvShares.RecoverPrivateKey(ids)
			if err != nil {
				b.Fatalf("%v", err)
			}
			b.StartTimer()
			if _, err := prvKey.Sign(hash); err != nil {
				b.Fatalf("%v", err)
			}
		}
	})

	sendKey := func(sender *member, receiver *member, b *testing.B) {
		receiver.receivedPubShares[sender.id] = sender.pubShares
		prvShare, ok := sender.prvShares.Share(receiver.id)
		if !ok {
			b.FailNow()
		}
		ok, err := receiver.receivedPubShares[sender.id].VerifyPrvShare(
			receiver.id, prvShare)
		if err != nil {
			b.Fatalf("%v", err)
		}
		if !ok {
			b.FailNow()
		}
		if err := receiver.receivedPrvShares.AddShare(sender.id, prvShare); err != nil {
			b.Fatalf("%v", err)
		}
	}

	members = append(members, &self)

	for _, sender := range members {
		wg := sync.WaitGroup{}
		for _, receiver := range members {
			if sender == receiver {
				continue
			}
			wg.Add(1)
			go func(receiver *member) {
				sendKey(sender, receiver, b)
				wg.Done()
			}(receiver)
		}
		wg.Wait()
	}
	wg := sync.WaitGroup{}
	for _, m := range members {
		wg.Add(1)
		go func(member *member) {
			sendKey(member, member, b)
			wg.Done()
		}(m)
	}
	wg.Wait()

	sign := func(member *member) PartialSignature {
		prvKey, err := member.receivedPrvShares.RecoverPrivateKey(ids)
		if err != nil {
			b.Fatalf("%v", err)
		}
		sig, err := prvKey.Sign(hash)
		if err != nil {
			b.Fatalf("%v", err)
		}
		return PartialSignature(sig)
	}

	b.Run("Combine-Sign", func(b *testing.B) {
		b.StopTimer()
		sigs := make([]PartialSignature, n)
		for idx, member := range members {
			sigs[idx] = sign(member)
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			if _, err := RecoverSignature(sigs, ids); err != nil {
				b.Fatalf("%v", err)
			}
		}
	})

	b.Run("Recover-GroupPK", func(b *testing.B) {
		b.StopTimer()
		pubShares := make([]*PublicKeyShares, 0, len(members))
		for _, member := range members {
			pubShares = append(pubShares, member.pubShares)
		}
		b.StartTimer()
		for i := 0; i < b.N; i++ {
			RecoverGroupPublicKey(pubShares)
		}
	})
}

func BenchmarkGPKShare81_121(b *testing.B) { benchmarkGPKShare(b, 81, 121) }

func benchmarkGPKShare(b *testing.B, t, n int) {
	_, pubShare := NewPrivateKeyShares(t)
	IDs := make(IDs, n)
	for i := range IDs {
		id := common.NewRandomHash()
		IDs[i] = NewID(id[:])
	}

	for _, id := range IDs {
		_, err := pubShare.Share(id)
		if err != nil {
			panic(err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, id := range IDs {
			pubShare.Share(id)
		}
	}
}

func BenchmarkGPKAddShare81_121(b *testing.B) { benchmarkGPKAddShare(b, 81, 121) }

func benchmarkGPKAddShare(b *testing.B, t, n int) {
	IDs := make(IDs, n)
	for i := range IDs {
		id := common.NewRandomHash()
		IDs[i] = NewID(id[:])
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		_, pubShare := NewPrivateKeyShares(t)
		b.StartTimer()
		for _, id := range IDs {
			pubShare.Share(id)
		}
	}
}
