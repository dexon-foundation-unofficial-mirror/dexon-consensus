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
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

type DKGTSIGProtocolTestSuite struct {
	suite.Suite

	nIDs    types.NodeIDs
	dkgIDs  map[types.NodeID]dkg.ID
	signers map[types.NodeID]*utils.Signer
}

type testDKGReceiver struct {
	s *DKGTSIGProtocolTestSuite

	signer         *utils.Signer
	complaints     map[types.NodeID]*typesDKG.Complaint
	mpk            *typesDKG.MasterPublicKey
	prvShare       map[types.NodeID]*typesDKG.PrivateShare
	antiComplaints map[types.NodeID]*typesDKG.PrivateShare
	ready          []*typesDKG.MPKReady
	final          []*typesDKG.Finalize
	success        []*typesDKG.Success
}

func newTestDKGReceiver(s *DKGTSIGProtocolTestSuite,
	signer *utils.Signer) *testDKGReceiver {
	return &testDKGReceiver{
		s:              s,
		signer:         signer,
		complaints:     make(map[types.NodeID]*typesDKG.Complaint),
		prvShare:       make(map[types.NodeID]*typesDKG.PrivateShare),
		antiComplaints: make(map[types.NodeID]*typesDKG.PrivateShare),
	}
}

func (r *testDKGReceiver) ProposeDKGComplaint(complaint *typesDKG.Complaint) {
	complaint = test.CloneDKGComplaint(complaint)
	err := r.signer.SignDKGComplaint(complaint)
	r.s.Require().NoError(err)
	r.complaints[complaint.PrivateShare.ProposerID] = complaint
}

func (r *testDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *typesDKG.MasterPublicKey) {
	mpk = test.CloneDKGMasterPublicKey(mpk)
	err := r.signer.SignDKGMasterPublicKey(mpk)
	r.s.Require().NoError(err)
	r.mpk = mpk
}

func (r *testDKGReceiver) ProposeDKGPrivateShare(
	prv *typesDKG.PrivateShare) {
	prv = test.CloneDKGPrivateShare(prv)
	err := r.signer.SignDKGPrivateShare(prv)
	r.s.Require().NoError(err)
	r.prvShare[prv.ReceiverID] = prv
}

func (r *testDKGReceiver) ProposeDKGAntiNackComplaint(
	prv *typesDKG.PrivateShare) {
	prv = test.CloneDKGPrivateShare(prv)
	err := r.signer.SignDKGPrivateShare(prv)
	r.s.Require().NoError(err)
	r.antiComplaints[prv.ReceiverID] = prv
}

func (r *testDKGReceiver) ProposeDKGMPKReady(ready *typesDKG.MPKReady) {
	r.ready = append(r.ready, ready)
}

func (r *testDKGReceiver) ProposeDKGFinalize(final *typesDKG.Finalize) {
	r.final = append(r.final, final)
}

func (r *testDKGReceiver) ProposeDKGSuccess(success *typesDKG.Success) {
	r.success = append(r.success, success)
}

func (s *DKGTSIGProtocolTestSuite) setupDKGParticipants(n int) {
	s.nIDs = make(types.NodeIDs, 0, n)
	s.signers = make(map[types.NodeID]*utils.Signer, n)
	s.dkgIDs = make(map[types.NodeID]dkg.ID)
	ids := make(dkg.IDs, 0, n)
	for i := 0; i < n; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		s.nIDs = append(s.nIDs, nID)
		s.signers[nID] = utils.NewSigner(prvKey)
		id := dkg.NewID(nID.Hash[:])
		ids = append(ids, id)
		s.dkgIDs[nID] = id
	}
}

func (s *DKGTSIGProtocolTestSuite) newGov(
	pubKeys []crypto.PublicKey,
	round, reset uint64) *test.Governance {
	// NOTE: this method doesn't make the tip round in governance to the input
	//       one.
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)
	for i := uint64(0); i < reset; i++ {
		s.Require().NoError(gov.State().RequestChange(test.StateResetDKG,
			common.NewRandomHash()))
	}
	s.Require().Equal(gov.DKGResetCount(round), reset)
	return gov
}

func (s *DKGTSIGProtocolTestSuite) newProtocols(k, n int, round, reset uint64) (
	map[types.NodeID]*testDKGReceiver, map[types.NodeID]*dkgProtocol) {
	s.setupDKGParticipants(n)

	receivers := make(map[types.NodeID]*testDKGReceiver, n)
	protocols := make(map[types.NodeID]*dkgProtocol, n)
	for _, nID := range s.nIDs {
		receivers[nID] = newTestDKGReceiver(s, s.signers[nID])
		protocols[nID] = newDKGProtocol(
			nID,
			receivers[nID],
			round,
			reset,
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
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
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

	for _, receiver := range receivers {
		for _, complaint := range receiver.complaints {
			gov.AddDKGComplaint(complaint)
		}
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
	gpk, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k)
	s.Require().NoError(err)
	s.Require().Len(gpk.QualifyIDs, n)
	qualifyIDs := make(map[dkg.ID]struct{}, len(gpk.QualifyIDs))
	for _, id := range gpk.QualifyIDs {
		qualifyIDs[id] = struct{}{}
	}

	for nID := range gpk.QualifyNodeIDs {
		id, exist := gpk.IDMap[nID]
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
		shareSecrets[nID], err = protocol.recoverShareSecret(gpk.QualifyIDs)
		s.Require().NoError(err)
	}

	npks, err := typesDKG.NewNodePublicKeys(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round), k)
	s.Require().NoError(err)
	msgHash := crypto.Keccak256Hash([]byte("🏖🍹"))
	tsig := newTSigProtocol(npks, msgHash)
	for nID, shareSecret := range shareSecrets {
		psig := &typesDKG.PartialSignature{
			ProposerID:       nID,
			Round:            round,
			Hash:             msgHash,
			PartialSignature: shareSecret.sign(msgHash),
		}
		err := s.signers[nID].SignDKGPartialSignature(psig)
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

func (s *DKGTSIGProtocolTestSuite) TestErrMPKRegistered() {
	k := 2
	n := 10
	round := uint64(1)
	reset := uint64(2)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)
	notRegisterID := s.nIDs[0]
	errRegisterID := s.nIDs[1]

	for ID, receiver := range receivers {
		if ID == notRegisterID {
			continue
		}
		if ID == errRegisterID {
			_, mpk := dkg.NewPrivateKeyShares(k)
			receiver.ProposeDKGMasterPublicKey(&typesDKG.MasterPublicKey{
				Round:           round,
				Reset:           reset,
				DKGID:           typesDKG.NewID(ID),
				PublicKeyShares: *mpk.Move(),
			})
		}
		gov.AddDKGMasterPublicKey(receiver.mpk)
	}

	for ID, protocol := range protocols {
		err := protocol.processMasterPublicKeys(gov.DKGMasterPublicKeys(round))
		if ID == notRegisterID {
			s.Require().Equal(ErrSelfMPKNotRegister, err)
		} else if ID == errRegisterID {
			s.Require().Equal(ErrSelfPrvShareMismatch, err)
		} else {
			s.Require().NoError(err)
		}
	}

	for ID, receiver := range receivers {
		if ID == notRegisterID || ID == errRegisterID {
			continue
		}
		s.Require().Len(receiver.prvShare, n-1)
		for nID, prvShare := range receiver.prvShare {
			s.Require().NoError(protocols[nID].processPrivateShare(prvShare))
		}
	}

	for ID, protocol := range protocols {
		if ID == notRegisterID {
			continue
		}
		protocol.proposeNackComplaints()
	}

	for ID, recv := range receivers {
		if ID == notRegisterID || ID == errRegisterID {
			continue
		}
		s.Require().Len(recv.complaints, 1)
		for _, complaint := range recv.complaints {
			s.Require().True(complaint.IsNack())
			s.Require().Equal(errRegisterID, complaint.PrivateShare.ProposerID)
		}
	}

	for _, receiver := range receivers {
		for _, complaint := range receiver.complaints {
			gov.AddDKGComplaint(complaint)
		}
	}

	s.Require().Len(gov.DKGComplaints(round), n-1)

	for ID, protocol := range protocols {
		err := protocol.processNackComplaints(gov.DKGComplaints(round))
		if ID == notRegisterID {
			s.Require().Equal(ErrSelfMPKNotRegister, err)
		} else if ID == errRegisterID {
			s.Require().Equal(ErrSelfPrvShareMismatch, err)
		} else {
			s.Require().NoError(err)
		}
	}

	for _, recv := range receivers {
		s.Require().Len(recv.antiComplaints, 0)
	}

	for _, protocol := range protocols {
		protocol.enforceNackComplaints(gov.DKGComplaints(round))
	}

	for ID, recv := range receivers {
		if ID == notRegisterID || ID == errRegisterID {
			continue
		}
		s.Require().Len(recv.complaints, 1)
		for _, complaint := range recv.complaints {
			s.Require().True(complaint.IsNack())
			s.Require().Equal(errRegisterID, complaint.PrivateShare.ProposerID)
		}
	}

	// DKG is fininished.
	gpk, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.QualifyIDs, n-2)
	qualifyIDs := make(map[dkg.ID]struct{}, len(gpk.QualifyIDs))
	for _, id := range gpk.QualifyIDs {
		qualifyIDs[id] = struct{}{}
	}

	for nID := range gpk.QualifyNodeIDs {
		if nID == notRegisterID || nID == errRegisterID {
			continue
		}
		id, exist := gpk.IDMap[nID]
		s.Require().True(exist)
		_, exist = qualifyIDs[id]
		s.Require().True(exist)
	}

}

func (s *DKGTSIGProtocolTestSuite) TestNackComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
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
		s.True(utils.VerifyDKGComplaintSignature(complaint))
	}
}

// TestComplaint tests if the received private share is not valid, a complaint
// should be proposed.
func (s *DKGTSIGProtocolTestSuite) TestComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]
	targetID := s.nIDs[1]
	receiver := receivers[targetID]
	protocol := protocols[targetID]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
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
		Reset:      reset,
	})
	s.Equal(ErrNotDKGParticipant, err)
	receivers[byzantineID].ProposeDKGPrivateShare(&typesDKG.PrivateShare{
		ProposerID: byzantineID,
		ReceiverID: targetID,
		Round:      round,
		Reset:      reset,
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
		Reset:        reset,
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
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, _ := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]
	victomID := s.nIDs[1]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
	}

	// Test for nack complaints.
	complaints := make([]*typesDKG.Complaint, k+1)
	for i := range complaints {
		complaints[i] = &typesDKG.Complaint{
			ProposerID: byzantineID,
			Round:      round,
			Reset:      reset,
			PrivateShare: typesDKG.PrivateShare{
				ProposerID: victomID,
				Round:      round,
				Reset:      reset,
			},
		}
		s.Require().True(complaints[i].IsNack())
	}

	gpk, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints,
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.QualifyIDs, n)
}

// TestAntiComplaint tests if a nack complaint is received,
// create an anti complaint.
func (s *DKGTSIGProtocolTestSuite) TestAntiComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]
	targetID := s.nIDs[1]
	thirdPerson := s.nIDs[2]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
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
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]
	targetID := s.nIDs[1]
	thirdPerson := s.nIDs[2]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
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
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, _ := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
	}

	// Test for nack complaints.
	complaints := make([]*typesDKG.Complaint, k+1)
	for i := range complaints {
		nID := s.nIDs[i]
		complaints[i] = &typesDKG.Complaint{
			ProposerID: nID,
			Round:      round,
			Reset:      reset,
			PrivateShare: typesDKG.PrivateShare{
				ProposerID: byzantineID,
				Round:      round,
				Reset:      reset,
			},
		}
		s.Require().True(complaints[i].IsNack())
	}

	gpk, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints,
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.QualifyIDs, n-1)
	for _, id := range gpk.QualifyIDs {
		s.NotEqual(id, byzantineID)
	}

	gpk2, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints[:k-1],
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk2.QualifyIDs, n)

	// Test for complaint.
	complaints[0].PrivateShare.Signature = crypto.Signature{Signature: []byte{0}}
	s.Require().False(complaints[0].IsNack())
	gpk3, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints[:1],
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk3.QualifyIDs, n-1)
	for _, id := range gpk3.QualifyIDs {
		s.NotEqual(id, byzantineID)
	}
}

// TestPartialSignature tests if tsigProtocol can handle incorrect partial
// signature and report error.
func (s *DKGTSIGProtocolTestSuite) TestPartialSignature() {
	k := 3
	n := 10
	round := uint64(1)
	reset := uint64(3)
	_, pubKeys, err := test.NewKeys(5)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)

	receivers, protocols := s.newProtocols(k, n, round, reset)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
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
		gov.AddDKGComplaint(complaint)
	}

	// DKG is fininished.
	gpk, err := typesDKG.NewGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.QualifyIDs, n-1)
	qualifyIDs := make(map[dkg.ID]struct{}, len(gpk.QualifyIDs))
	for _, id := range gpk.QualifyIDs {
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
		shareSecrets[nID], err = protocol.recoverShareSecret(gpk.QualifyIDs)
		s.Require().NoError(err)
	}

	msgHash := crypto.Keccak256Hash([]byte("🏖🍹"))
	npks, err := typesDKG.NewNodePublicKeys(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round), k)
	s.Require().NoError(err)
	tsig := newTSigProtocol(npks, msgHash)
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
		err := s.signers[nID].SignDKGPartialSignature(psig)
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

func (s *DKGTSIGProtocolTestSuite) TestProposeReady() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	recv := newTestDKGReceiver(s, utils.NewSigner(prvKey))
	nID := types.NewNodeID(prvKey.PublicKey())
	protocol := newDKGProtocol(nID, recv, 1, 3, 2)
	protocol.proposeMPKReady()
	s.Require().Len(recv.ready, 1)
	ready := recv.ready[0]
	s.Equal(&typesDKG.MPKReady{
		ProposerID: nID,
		Round:      1,
		Reset:      3,
	}, ready)
}

func (s *DKGTSIGProtocolTestSuite) TestProposeFinalize() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	recv := newTestDKGReceiver(s, utils.NewSigner(prvKey))
	nID := types.NewNodeID(prvKey.PublicKey())
	protocol := newDKGProtocol(nID, recv, 1, 3, 2)
	protocol.proposeFinalize()
	s.Require().Len(recv.final, 1)
	final := recv.final[0]
	s.Equal(&typesDKG.Finalize{
		ProposerID: nID,
		Round:      1,
		Reset:      3,
	}, final)
}

func (s *DKGTSIGProtocolTestSuite) TestProposeSuccess() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	recv := newTestDKGReceiver(s, utils.NewSigner(prvKey))
	nID := types.NewNodeID(prvKey.PublicKey())
	protocol := newDKGProtocol(nID, recv, 1, 3, 2)
	protocol.proposeSuccess()
	s.Require().Len(recv.success, 1)
	success := recv.success[0]
	s.Equal(&typesDKG.Success{
		ProposerID: nID,
		Round:      1,
		Reset:      3,
	}, success)
}

func (s *DKGTSIGProtocolTestSuite) TestTSigVerifierCache() {
	k := 3
	n := 10
	round := uint64(10)
	reset := uint64(0)
	_, pubKeys, err := test.NewKeys(n)
	s.Require().NoError(err)
	gov := s.newGov(pubKeys, round, reset)
	gov.CatchUpWithRound(round)
	for i := 0; i < 10; i++ {
		round := uint64(i + 1)
		receivers, protocols := s.newProtocols(k, n, round, reset)

		for _, receiver := range receivers {
			gov.AddDKGMasterPublicKey(receiver.mpk)
		}

		for _, protocol := range protocols {
			protocol.proposeMPKReady()
		}
		for _, recv := range receivers {
			s.Require().Len(recv.ready, 1)
			gov.AddDKGMPKReady(recv.ready[0])
		}
		s.Require().True(gov.IsDKGMPKReady(round))

		for _, protocol := range protocols {
			protocol.proposeFinalize()
		}

		for _, recv := range receivers {
			s.Require().Len(recv.final, 1)
			gov.AddDKGFinalize(recv.final[0])
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

	cache.Delete(uint64(5))
	s.Len(cache.verifier, 2)
	_, exist := cache.Get(uint64(5))
	s.False(exist)

	cache = NewTSigVerifierCache(gov, 1)
	ok, err = cache.Update(uint64(3))
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(uint64(3), cache.minRound)

	ok, err = cache.Update(uint64(5))
	s.Require().NoError(err)
	s.Require().True(ok)
	s.Equal(uint64(5), cache.minRound)

	cache.Purge(5)
	s.Require().Len(cache.verifier, 0)
	s.Require().Equal(uint64(5), cache.minRound)
}

func (s *DKGTSIGProtocolTestSuite) TestUnexpectedDKGResetCount() {
	// MPKs and private shares from unexpected reset count should be ignored.
	k := 2
	n := 10
	round := uint64(1)
	reset := uint64(3)
	receivers, protocols := s.newProtocols(k, n, round, reset)
	var sourceID, targetID types.NodeID
	for sourceID = range receivers {
		break
	}
	for targetID = range receivers {
		break
	}
	// Test private share
	s.Require().NoError(protocols[targetID].processMasterPublicKeys(
		[]*typesDKG.MasterPublicKey{
			receivers[targetID].mpk,
			receivers[sourceID].mpk}))
	receivers[sourceID].ProposeDKGPrivateShare(&typesDKG.PrivateShare{
		ProposerID:   sourceID,
		ReceiverID:   targetID,
		Round:        round,
		Reset:        reset + 1,
		PrivateShare: *dkg.NewPrivateKey(),
	})
	err := protocols[targetID].processPrivateShare(
		receivers[sourceID].prvShare[targetID])
	s.Require().IsType(ErrUnexpectedDKGResetCount{}, err)
	// Test MPK.
	_, mpk := dkg.NewPrivateKeyShares(k)
	receivers[sourceID].ProposeDKGMasterPublicKey(&typesDKG.MasterPublicKey{
		Round:           round,
		Reset:           reset + 1,
		DKGID:           typesDKG.NewID(sourceID),
		PublicKeyShares: *mpk.Move(),
	})
	err = protocols[sourceID].processMasterPublicKeys(
		[]*typesDKG.MasterPublicKey{receivers[sourceID].mpk})
	s.Require().IsType(ErrUnexpectedDKGResetCount{}, err)
}

func TestDKGTSIGProtocol(t *testing.T) {
	suite.Run(t, new(DKGTSIGProtocolTestSuite))
}

func BenchmarkGPK4_7(b *testing.B)    { benchmarkDKGGroupPubliKey(4, 7, b) }
func BenchmarkGPK9_13(b *testing.B)   { benchmarkDKGGroupPubliKey(9, 13, b) }
func BenchmarkGPK17_24(b *testing.B)  { benchmarkDKGGroupPubliKey(17, 24, b) }
func BenchmarkGPK81_121(b *testing.B) { benchmarkDKGGroupPubliKey(81, 121, b) }

func benchmarkDKGGroupPubliKey(k, n int, b *testing.B) {
	round := uint64(1)
	reset := uint64(0)
	_, pubKeys, err := test.NewKeys(n)
	if err != nil {
		panic(err)
	}
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	if err != nil {
		panic(err)
	}

	for _, pk := range pubKeys {
		_, pubShare := dkg.NewPrivateKeyShares(k)
		gov.AddDKGMasterPublicKey(&typesDKG.MasterPublicKey{
			ProposerID:      types.NewNodeID(pk),
			Round:           round,
			Reset:           reset,
			DKGID:           typesDKG.NewID(types.NewNodeID(pk)),
			PublicKeyShares: *pubShare.Move(),
		})
	}

	mpk := gov.DKGMasterPublicKeys(round)
	comp := gov.DKGComplaints(round)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// DKG is fininished.
		gpk, err := typesDKG.NewGroupPublicKey(round, mpk, comp, k)
		if err != nil {
			panic(err)
		}
		if len(gpk.QualifyIDs) != n {
			panic("not enough of qualify id")
		}
	}
}

func BenchmarkNPKs4_7(b *testing.B)    { benchmarkDKGNodePubliKeys(4, 7, b) }
func BenchmarkNPKs9_13(b *testing.B)   { benchmarkDKGNodePubliKeys(9, 13, b) }
func BenchmarkNPKs17_24(b *testing.B)  { benchmarkDKGNodePubliKeys(17, 24, b) }
func BenchmarkNPKs81_121(b *testing.B) { benchmarkDKGNodePubliKeys(81, 121, b) }

func benchmarkDKGNodePubliKeys(k, n int, b *testing.B) {
	round := uint64(1)
	reset := uint64(0)
	_, pubKeys, err := test.NewKeys(n)
	if err != nil {
		panic(err)
	}
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	if err != nil {
		panic(err)
	}

	for _, pk := range pubKeys {
		_, pubShare := dkg.NewPrivateKeyShares(k)
		gov.AddDKGMasterPublicKey(&typesDKG.MasterPublicKey{
			ProposerID:      types.NewNodeID(pk),
			Round:           round,
			Reset:           reset,
			DKGID:           typesDKG.NewID(types.NewNodeID(pk)),
			PublicKeyShares: *pubShare.Move(),
		})
	}

	mpk := gov.DKGMasterPublicKeys(round)
	comp := gov.DKGComplaints(round)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// DKG is fininished.
		npks, err := typesDKG.NewNodePublicKeys(round, mpk, comp, k)
		if err != nil {
			panic(err)
		}
		if len(npks.QualifyIDs) != n {
			panic("not enough of qualify id")
		}
	}
}

func BenchmarkCalcQ4_7(b *testing.B)    { benchmarkCalcQualified(4, 7, b) }
func BenchmarkCalcQ9_13(b *testing.B)   { benchmarkCalcQualified(9, 13, b) }
func BenchmarkCalcQ17_24(b *testing.B)  { benchmarkCalcQualified(17, 24, b) }
func BenchmarkCalcQ81_121(b *testing.B) { benchmarkCalcQualified(81, 121, b) }

func benchmarkCalcQualified(k, n int, b *testing.B) {
	round := uint64(1)
	reset := uint64(0)
	_, pubKeys, err := test.NewKeys(n)
	if err != nil {
		panic(err)
	}
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, 100, &common.NullLogger{}, true), ConfigRoundShift)
	if err != nil {
		panic(err)
	}

	for _, pk := range pubKeys {
		_, pubShare := dkg.NewPrivateKeyShares(k)
		gov.AddDKGMasterPublicKey(&typesDKG.MasterPublicKey{
			ProposerID:      types.NewNodeID(pk),
			Round:           round,
			Reset:           reset,
			DKGID:           typesDKG.NewID(types.NewNodeID(pk)),
			PublicKeyShares: *pubShare.Move(),
		})
	}

	mpk := gov.DKGMasterPublicKeys(round)
	comp := gov.DKGComplaints(round)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// DKG is fininished.
		_, q, err := typesDKG.CalcQualifyNodes(mpk, comp, k)
		if err != nil {
			panic(err)
		}
		if len(q) != n {
			panic("not enough of qualify id")
		}
	}
}
