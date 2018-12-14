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

package core

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

type DKGTSIGProtocolTestSuite struct {
	suite.Suite

	nIDs    types.NodeIDs
	dkgIDs  map[types.NodeID]dkg.ID
	prvKeys map[types.NodeID]crypto.PrivateKey
}

type testDKGReceiver struct {
	s *DKGTSIGProtocolTestSuite

	prvKey         crypto.PrivateKey
	complaints     map[types.NodeID]*typesDKG.Complaint
	mpk            *typesDKG.MasterPublicKey
	prvShare       map[types.NodeID]*typesDKG.PrivateShare
	antiComplaints map[types.NodeID]*typesDKG.PrivateShare
	final          []*typesDKG.Finalize
}

func newTestDKGReceiver(
	s *DKGTSIGProtocolTestSuite, prvKey crypto.PrivateKey) *testDKGReceiver {
	return &testDKGReceiver{
		s:              s,
		prvKey:         prvKey,
		complaints:     make(map[types.NodeID]*typesDKG.Complaint),
		prvShare:       make(map[types.NodeID]*typesDKG.PrivateShare),
		antiComplaints: make(map[types.NodeID]*typesDKG.PrivateShare),
	}
}

func (r *testDKGReceiver) ProposeDKGComplaint(complaint *typesDKG.Complaint) {
	var err error
	complaint.Signature, err = r.prvKey.Sign(hashDKGComplaint(complaint))
	r.s.Require().NoError(err)
	r.complaints[complaint.PrivateShare.ProposerID] = complaint
}

func (r *testDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *typesDKG.MasterPublicKey) {
	var err error
	mpk.Signature, err = r.prvKey.Sign(hashDKGMasterPublicKey(mpk))
	r.s.Require().NoError(err)
	r.mpk = mpk
}

func (r *testDKGReceiver) ProposeDKGPrivateShare(
	prv *typesDKG.PrivateShare) {
	var err error
	prv.Signature, err = r.prvKey.Sign(hashDKGPrivateShare(prv))
	r.s.Require().NoError(err)
	r.prvShare[prv.ReceiverID] = prv
}

func (r *testDKGReceiver) ProposeDKGAntiNackComplaint(
	prv *typesDKG.PrivateShare) {
	var err error
	prv.Signature, err = r.prvKey.Sign(hashDKGPrivateShare(prv))
	r.s.Require().NoError(err)
	r.antiComplaints[prv.ReceiverID] = prv
}

func (r *testDKGReceiver) ProposeDKGFinalize(final *typesDKG.Finalize) {
	r.final = append(r.final, final)
}

func (s *DKGTSIGProtocolTestSuite) setupDKGParticipants(n int) {
	s.nIDs = make(types.NodeIDs, 0, n)
	s.prvKeys = make(map[types.NodeID]crypto.PrivateKey, n)
	s.dkgIDs = make(map[types.NodeID]dkg.ID)
	ids := make(dkg.IDs, 0, n)
	for i := 0; i < n; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		s.nIDs = append(s.nIDs, nID)
		s.prvKeys[nID] = prvKey
		id := dkg.NewID(nID.Hash[:])
		ids = append(ids, id)
		s.dkgIDs[nID] = id
	}
}

func (s *DKGTSIGProtocolTestSuite) newProtocols(k, n int, round uint64) (
	map[types.NodeID]*testDKGReceiver, map[types.NodeID]*dkgProtocol) {
	s.setupDKGParticipants(n)

	receivers := make(map[types.NodeID]*testDKGReceiver, n)
	protocols := make(map[types.NodeID]*dkgProtocol, n)
	for _, nID := range s.nIDs {
		receivers[nID] = newTestDKGReceiver(s, s.prvKeys[nID])
		protocols[nID] = newDKGProtocol(
			nID,
			receivers[nID],
			round,
			k,
		)
		s.Require().NotNil(receivers[nID].mpk)
	}
	return receivers, protocols
}

// TestDKGTSIGProtocol will test the entire DKG+TISG protocol including
// exchanging private shares, recovering share secret, creating partial sign and
// recovering threshold signature.
// All participants are good people in this test.
func (s *DKGTSIGProtocolTestSuite) TestDKGTSIGProtocol() {
	k := 2
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	for _, protocol := range protocols {
		s.Require().NoError(
			protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round)))
	}

	for _, receiver := range receivers {
		s.Require().Len(receiver.prvShare, n)
		for nID, prvShare := range receiver.prvShare {
			s.Require().NoError(protocols[nID].processPrivateShare(prvShare))
		}
	}

	for _, protocol := range protocols {
		protocol.proposeNackComplaints()
	}

	for _, recv := range receivers {
		s.Require().Len(recv.complaints, 0)
	}

	for _, protocol := range protocols {
		s.Require().NoError(protocol.processNackComplaints(
			gov.DKGComplaints(round)))
	}

	for _, recv := range receivers {
		s.Require().Len(recv.antiComplaints, 0)
	}

	for _, protocol := range protocols {
		protocol.enforceNackComplaints(gov.DKGComplaints(round))
	}

	for _, recv := range receivers {
		s.Require().Len(recv.complaints, 0)
	}

	// DKG is fininished.
	gpk, err := NewDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.qualifyIDs, n)
	qualifyIDs := make(map[dkg.ID]struct{}, len(gpk.qualifyIDs))
	for _, id := range gpk.qualifyIDs {
		qualifyIDs[id] = struct{}{}
	}

	for nID := range gpk.qualifyNodeIDs {
		id, exist := gpk.idMap[nID]
		s.Require().True(exist)
		_, exist = qualifyIDs[id]
		s.Require().True(exist)
	}

	shareSecrets := make(
		map[types.NodeID]*dkgShareSecret, len(qualifyIDs))

	for nID, protocol := range protocols {
		_, exist := qualifyIDs[s.dkgIDs[nID]]
		s.Require().True(exist)
		var err error
		shareSecrets[nID], err = protocol.recoverShareSecret(gpk.qualifyIDs)
		s.Require().NoError(err)
	}

	msgHash := crypto.Keccak256Hash([]byte("🏖🍹"))
	tsig := newTSigProtocol(gpk, msgHash)
	for nID, shareSecret := range shareSecrets {
		psig := &typesDKG.PartialSignature{
			ProposerID:       nID,
			Round:            round,
			Hash:             msgHash,
			PartialSignature: shareSecret.sign(msgHash),
		}
		var err error
		psig.Signature, err = s.prvKeys[nID].Sign(hashDKGPartialSignature(psig))
		s.Require().NoError(err)
		s.Require().NoError(tsig.processPartialSignature(psig))
		if len(tsig.sigs) >= k {
			break
		}
	}

	sig, err := tsig.signature()
	s.Require().NoError(err)
	s.True(gpk.VerifySignature(msgHash, sig))
}

func (s *DKGTSIGProtocolTestSuite) TestNackComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	for _, protocol := range protocols {
		s.Require().NoError(
			protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round)))
	}

	for senderID, receiver := range receivers {
		s.Require().Len(receiver.prvShare, n)
		if senderID == byzantineID {
			continue
		}
		for nID, prvShare := range receiver.prvShare {
			s.Require().NoError(protocols[nID].processPrivateShare(prvShare))
		}
	}

	for _, protocol := range protocols {
		protocol.proposeNackComplaints()
	}

	for _, recv := range receivers {
		complaint, exist := recv.complaints[byzantineID]
		s.True(complaint.IsNack())
		s.Require().True(exist)
		s.True(VerifyDKGComplaintSignature(complaint))
	}
}

// TestComplaint tests if the received private share is not valid, a complaint
// should be proposed.
func (s *DKGTSIGProtocolTestSuite) TestComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]
	targetID := s.nIDs[1]
	receiver := receivers[targetID]
	protocol := protocols[targetID]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	for _, protocol := range protocols {
		s.Require().NoError(
			protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round)))
	}

	// These messages are not valid.
	err = protocol.processPrivateShare(&typesDKG.PrivateShare{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		ReceiverID: targetID,
		Round:      round,
	})
	s.Equal(ErrNotDKGParticipant, err)
	receivers[byzantineID].ProposeDKGPrivateShare(&typesDKG.PrivateShare{
		ProposerID: byzantineID,
		ReceiverID: targetID,
		Round:      round,
	})
	invalidShare := receivers[byzantineID].prvShare[targetID]
	invalidShare.ReceiverID = s.nIDs[2]
	err = protocol.processPrivateShare(invalidShare)
	s.Equal(ErrIncorrectPrivateShareSignature, err)
	delete(receivers[byzantineID].prvShare, targetID)

	// Byzantine node is sending incorrect private share.
	receivers[byzantineID].ProposeDKGPrivateShare(&typesDKG.PrivateShare{
		ProposerID:   byzantineID,
		ReceiverID:   targetID,
		Round:        round,
		PrivateShare: *dkg.NewPrivateKey(),
	})
	invalidShare = receivers[byzantineID].prvShare[targetID]
	s.Require().NoError(protocol.processPrivateShare(invalidShare))
	s.Require().Len(receiver.complaints, 1)
	complaint, exist := receiver.complaints[byzantineID]
	s.True(exist)
	s.Equal(byzantineID, complaint.PrivateShare.ProposerID)

	// Sending the incorrect private share again should not complain.
	delete(receiver.complaints, byzantineID)
	s.Require().NoError(protocol.processPrivateShare(invalidShare))
	s.Len(receiver.complaints, 0)
}

// TestDuplicateComplaint tests if the duplicated complaint is process properly.
func (s *DKGTSIGProtocolTestSuite) TestDuplicateComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, _ := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]
	victomID := s.nIDs[1]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	// Test for nack complaints.
	complaints := make([]*typesDKG.Complaint, k+1)
	for i := range complaints {
		complaints[i] = &typesDKG.Complaint{
			ProposerID: byzantineID,
			Round:      round,
			PrivateShare: typesDKG.PrivateShare{
				ProposerID: victomID,
				Round:      round,
			},
		}
		s.Require().True(complaints[i].IsNack())
	}

	gpk, err := NewDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints,
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.qualifyIDs, n)
}

// TestAntiComplaint tests if a nack complaint is received,
// create an anti complaint.
func (s *DKGTSIGProtocolTestSuite) TestAntiComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]
	targetID := s.nIDs[1]
	thirdPerson := s.nIDs[2]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	for _, protocol := range protocols {
		s.Require().NoError(
			protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round)))
	}

	// Creating Nack complaint.
	protocols[targetID].proposeNackComplaints()
	protocols[thirdPerson].proposeNackComplaints()
	complaint, exist := receivers[targetID].complaints[byzantineID]
	s.Require().True(exist)
	s.Require().True(complaint.IsNack())
	s.Require().Equal(byzantineID, complaint.PrivateShare.ProposerID)

	complaint2, exist := receivers[thirdPerson].complaints[byzantineID]
	s.Require().True(exist)
	s.Require().True(complaint2.IsNack())
	s.Require().Equal(byzantineID, complaint2.PrivateShare.ProposerID)

	// Creating an anti-nack complaint.
	err = protocols[byzantineID].processNackComplaints(
		[]*typesDKG.Complaint{complaint})
	s.Require().NoError(err)
	s.Require().Len(receivers[byzantineID].antiComplaints, 1)
	antiComplaint, exist := receivers[byzantineID].antiComplaints[targetID]
	s.Require().True(exist)
	s.Require().Equal(targetID, antiComplaint.ReceiverID)

	// The anti-complaint should be successfully verified by all others.
	receivers[targetID].complaints = make(map[types.NodeID]*typesDKG.Complaint)
	s.Require().NoError(protocols[targetID].processPrivateShare(antiComplaint))
	s.Len(receivers[targetID].complaints, 0)

	receivers[thirdPerson].complaints = make(map[types.NodeID]*typesDKG.Complaint)
	s.Require().NoError(protocols[thirdPerson].processPrivateShare(antiComplaint))
	s.Len(receivers[thirdPerson].complaints, 0)
}

// TestEncorceNackComplaint tests if the nack complaint is enforced properly.
func (s *DKGTSIGProtocolTestSuite) TestEncorceNackComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]
	targetID := s.nIDs[1]
	thirdPerson := s.nIDs[2]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	for _, protocol := range protocols {
		s.Require().NoError(
			protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round)))
	}

	// Creating nack complaint.
	protocols[targetID].proposeNackComplaints()
	complaint, exist := receivers[targetID].complaints[byzantineID]
	s.Require().True(exist)
	s.Require().True(complaint.IsNack())
	s.Require().Equal(byzantineID, complaint.PrivateShare.ProposerID)

	// Encorce nack complaint.
	protocols[thirdPerson].enforceNackComplaints([]*typesDKG.Complaint{complaint})
	complaint2, exist := receivers[thirdPerson].complaints[byzantineID]
	s.Require().True(exist)
	s.Require().True(complaint2.IsNack())
	s.Require().Equal(byzantineID, complaint2.PrivateShare.ProposerID)

	// Received valid private share, do not enforce nack complaint.
	delete(receivers[thirdPerson].complaints, byzantineID)
	err = protocols[byzantineID].processNackComplaints(
		[]*typesDKG.Complaint{complaint})
	s.Require().NoError(err)
	antiComplaint, exist := receivers[byzantineID].antiComplaints[targetID]
	s.Require().True(exist)
	s.Require().Equal(targetID, antiComplaint.ReceiverID)
	s.Require().NoError(protocols[thirdPerson].processPrivateShare(antiComplaint))
	protocols[thirdPerson].enforceNackComplaints([]*typesDKG.Complaint{complaint})
	_, exist = receivers[thirdPerson].complaints[byzantineID]
	s.Require().False(exist)
}

// TestQualifyIDs tests if there is a id with t+1 nack complaints
// or a complaint, it should not be in the qualifyIDs.
func (s *DKGTSIGProtocolTestSuite) TestQualifyIDs() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, _ := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	// Test for nack complaints.
	complaints := make([]*typesDKG.Complaint, k+1)
	for i := range complaints {
		nID := s.nIDs[i]
		complaints[i] = &typesDKG.Complaint{
			ProposerID: nID,
			Round:      round,
			PrivateShare: typesDKG.PrivateShare{
				ProposerID: byzantineID,
				Round:      round,
			},
		}
		s.Require().True(complaints[i].IsNack())
	}

	gpk, err := NewDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints,
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.qualifyIDs, n-1)
	for _, id := range gpk.qualifyIDs {
		s.NotEqual(id, byzantineID)
	}

	gpk2, err := NewDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints[:k],
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk2.qualifyIDs, n)

	// Test for complaint.
	complaints[0].PrivateShare.Signature = crypto.Signature{Signature: []byte{0}}
	s.Require().False(complaints[0].IsNack())
	gpk3, err := NewDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints[:1],
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk3.qualifyIDs, n-1)
	for _, id := range gpk3.qualifyIDs {
		s.NotEqual(id, byzantineID)
	}
}

// TestPartialSignature tests if tsigProtocol can handle incorrect partial
// signature and report error.
func (s *DKGTSIGProtocolTestSuite) TestPartialSignature() {
	k := 3
	n := 10
	round := uint64(1)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(round, receiver.mpk)
	}

	for _, protocol := range protocols {
		s.Require().NoError(
			protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round)))
	}

	for senderID, receiver := range receivers {
		s.Require().Len(receiver.prvShare, n)
		if senderID == byzantineID {
			continue
		}
		for nID, prvShare := range receiver.prvShare {
			s.Require().NoError(protocols[nID].processPrivateShare(prvShare))
		}
	}

	for _, protocol := range protocols {
		protocol.proposeNackComplaints()
	}

	for _, recv := range receivers {
		s.Require().Len(recv.complaints, 1)
		complaint, exist := recv.complaints[byzantineID]
		s.Require().True(exist)
		gov.AddDKGComplaint(round, complaint)
	}

	// DKG is fininished.
	gpk, err := NewDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.qualifyIDs, n-1)
	qualifyIDs := make(map[dkg.ID]struct{}, len(gpk.qualifyIDs))
	for _, id := range gpk.qualifyIDs {
		qualifyIDs[id] = struct{}{}
	}

	shareSecrets := make(
		map[types.NodeID]*dkgShareSecret, len(qualifyIDs))

	for nID, protocol := range protocols {
		_, exist := qualifyIDs[s.dkgIDs[nID]]
		if nID == byzantineID {
			exist = !exist
		}
		s.Require().True(exist)
		var err error
		shareSecrets[nID], err = protocol.recoverShareSecret(gpk.qualifyIDs)
		s.Require().NoError(err)
	}

	msgHash := crypto.Keccak256Hash([]byte("🏖🍹"))
	tsig := newTSigProtocol(gpk, msgHash)
	byzantineID2 := s.nIDs[1]
	byzantineID3 := s.nIDs[2]
	for nID, shareSecret := range shareSecrets {
		psig := &typesDKG.PartialSignature{
			ProposerID:       nID,
			Round:            round,
			Hash:             msgHash,
			PartialSignature: shareSecret.sign(msgHash),
		}
		switch nID {
		case byzantineID2:
			psig.PartialSignature = shareSecret.sign(
				crypto.Keccak256Hash([]byte("💣")))
		case byzantineID3:
			psig.Hash = common.NewRandomHash()
		}
		var err error
		psig.Signature, err = s.prvKeys[nID].Sign(hashDKGPartialSignature(psig))
		s.Require().NoError(err)
		err = tsig.processPartialSignature(psig)
		switch nID {
		case byzantineID:
			s.Require().Equal(ErrNotQualifyDKGParticipant, err)
		case byzantineID2:
			s.Require().Equal(ErrIncorrectPartialSignature, err)
		case byzantineID3:
			s.Require().Equal(ErrMismatchPartialSignatureHash, err)
		default:
			s.Require().NoError(err)
		}
	}

	sig, err := tsig.signature()
	s.Require().NoError(err)
	s.True(gpk.VerifySignature(msgHash, sig))
}

func (s *DKGTSIGProtocolTestSuite) TestProposeFinalize() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	recv := newTestDKGReceiver(s, prvKey)
	nID := types.NewNodeID(prvKey.PublicKey())
	protocol := newDKGProtocol(nID, recv, 1, 2)
	protocol.proposeFinalize()
	s.Require().Len(recv.final, 1)
	final := recv.final[0]
	s.Equal(&typesDKG.Finalize{
		ProposerID: nID,
		Round:      1,
	}, final)
}

func (s *DKGTSIGProtocolTestSuite) TestTSigVerifierCache() {
	k := 3
	n := 10
	_, pubKeys, err := test.NewKeys(n)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)
	gov.CatchUpWithRound(10)
	for i := 0; i < 10; i++ {
		round := uint64(i + 1)
		receivers, protocols := s.newProtocols(k, n, round)

		for _, receiver := range receivers {
			gov.AddDKGMasterPublicKey(round, receiver.mpk)
		}

		for _, protocol := range protocols {
			protocol.proposeFinalize()
		}

		for _, recv := range receivers {
			s.Require().Len(recv.final, 1)
			gov.AddDKGFinalize(recv.final[0].Round, recv.final[0])
		}
		s.Require().True(gov.IsDKGFinal(round))
	}

	cache := NewTSigVerifierCache(gov, 3)
	for i := 0; i < 5; i++ {
		round := uint64(i + 1)
		ok, err := cache.Update(round)
		s.Require().NoError(err)
		s.True(ok)
	}
	s.Len(cache.verifier, 3)

	for i := 0; i < 2; i++ {
		round := uint64(i + 1)
		_, exist := cache.Get(round)
		s.False(exist)
	}

	for i := 3; i < 5; i++ {
		round := uint64(i + 1)
		_, exist := cache.Get(round)
		s.True(exist)
	}

	ok, err := cache.Update(uint64(1))
	s.Require().Equal(ErrRoundAlreadyPurged, err)

	cache = NewTSigVerifierCache(gov, 1)
	ok, err = cache.Update(uint64(3))
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(uint64(3), cache.minRound)

	ok, err = cache.Update(uint64(5))
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(uint64(5), cache.minRound)
}

func TestDKGTSIGProtocol(t *testing.T) {
	suite.Run(t, new(DKGTSIGProtocolTestSuite))
}
