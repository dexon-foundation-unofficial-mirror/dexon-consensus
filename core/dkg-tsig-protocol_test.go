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

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
)

type DKGTSIGProtocolTestSuite struct {
	suite.Suite

	nIDs    types.NodeIDs
	dkgIDs  map[types.NodeID]dkg.ID
	prvKeys map[types.NodeID]crypto.PrivateKey
}

type testDKGReceiver struct {
	s *DKGTSIGProtocolTestSuite

	prvKey     crypto.PrivateKey
	complaints map[types.NodeID]*types.DKGComplaint
	mpk        *types.DKGMasterPublicKey
	prvShare   map[types.NodeID]*types.DKGPrivateShare
}

func newTestDKGReceiver(
	s *DKGTSIGProtocolTestSuite, prvKey crypto.PrivateKey) *testDKGReceiver {
	return &testDKGReceiver{
		s:          s,
		prvKey:     prvKey,
		complaints: make(map[types.NodeID]*types.DKGComplaint),
		prvShare:   make(map[types.NodeID]*types.DKGPrivateShare),
	}
}

func (r *testDKGReceiver) ProposeDKGComplaint(complaint *types.DKGComplaint) {
	var err error
	complaint.Signature, err = r.prvKey.Sign(hashDKGComplaint(complaint))
	r.s.Require().NoError(err)
	r.complaints[complaint.PrivateShare.ProposerID] = complaint
}

func (r *testDKGReceiver) ProposeDKGMasterPublicKey(
	mpk *types.DKGMasterPublicKey) {
	var err error
	mpk.Signature, err = r.prvKey.Sign(hashDKGMasterPublicKey(mpk))
	r.s.Require().NoError(err)
	r.mpk = mpk
}
func (r *testDKGReceiver) ProposeDKGPrivateShare(
	to types.NodeID, prv *types.DKGPrivateShare) {
	var err error
	prv.Signature, err = r.prvKey.Sign(hashDKGPrivateShare(prv))
	r.s.Require().NoError(err)
	r.prvShare[to] = prv
}

func (s *DKGTSIGProtocolTestSuite) setupDKGParticipants(n int) {
	s.nIDs = make(types.NodeIDs, 0, n)
	s.prvKeys = make(map[types.NodeID]crypto.PrivateKey, n)
	s.dkgIDs = make(map[types.NodeID]dkg.ID)
	ids := make(dkg.IDs, 0, n)
	for i := 0; i < n; i++ {
		prvKey, err := eth.NewPrivateKey()
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
			eth.SigToPub,
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
	k := 3
	n := 10
	round := uint64(1)
	gov, err := test.NewGovernance(5, 100)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

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

	// DKG is fininished.
	gpk, err := newDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k, eth.SigToPub,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.qualifyIDs, n)
	qualifyIDs := make(map[dkg.ID]struct{}, len(gpk.qualifyIDs))
	for _, id := range gpk.qualifyIDs {
		qualifyIDs[id] = struct{}{}
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

	tsig := newTSigProtocol(gpk)
	msgHash := crypto.Keccak256Hash([]byte("🏖🍹"))
	for nID, shareSecret := range shareSecrets {
		psig := &types.DKGPartialSignature{
			ProposerID:       nID,
			Round:            round,
			PartialSignature: shareSecret.sign(msgHash),
		}
		var err error
		psig.Signature, err = s.prvKeys[nID].Sign(hashDKGPartialSignature(psig))
		s.Require().NoError(err)
		s.Require().NoError(tsig.processPartialSignature(msgHash, psig))
		if len(tsig.sigs) > k {
			break
		}
	}

	sig, err := tsig.signature()
	s.Require().NoError(err)
	s.True(gpk.verifySignature(msgHash, sig))
}

func (s *DKGTSIGProtocolTestSuite) TestNackComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	gov, err := test.NewGovernance(5, 100)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

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
		s.Require().True(exist)
		s.True(verifyDKGComplaintSignature(complaint, eth.SigToPub))
	}
}

// TestComplaint tests if the received private share is not valid, a complaint
// should be proposed.
func (s *DKGTSIGProtocolTestSuite) TestComplaint() {
	k := 3
	n := 10
	round := uint64(1)
	gov, err := test.NewGovernance(5, 100)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

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
	err = protocol.processPrivateShare(&types.DKGPrivateShare{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Round:      round,
	})
	s.Error(ErrNotDKGParticipant, err)
	err = protocol.processPrivateShare(&types.DKGPrivateShare{
		ProposerID: byzantineID,
		Round:      round,
	})
	s.Error(ErrIncorrectPrivateShareSignature, err)

	// Byzantine node is sending incorrect private share.
	err = protocol.processPrivateShare(receivers[byzantineID].prvShare[byzantineID])
	s.NoError(err)
	s.Require().Len(receiver.complaints, 1)
	complaint, exist := receiver.complaints[byzantineID]
	s.True(exist)
	s.Equal(byzantineID, complaint.PrivateShare.ProposerID)
}

// TestQualifyIDs tests if there is a id with t+1 complaints, it should not be
// in the qualifyIDs.
func (s *DKGTSIGProtocolTestSuite) TestQualifyIDs() {
	k := 3
	n := 10
	round := uint64(1)
	gov, err := test.NewGovernance(5, 100)
	s.Require().NoError(err)

	receivers, _ := s.newProtocols(k, n, round)

	byzantineID := s.nIDs[0]

	for _, receiver := range receivers {
		gov.AddDKGMasterPublicKey(receiver.mpk)
	}

	complaints := make([]*types.DKGComplaint, k+1)
	for i := range complaints {
		nID := s.nIDs[i]
		complaints[i] = &types.DKGComplaint{
			ProposerID: nID,
			Round:      round,
			PrivateShare: types.DKGPrivateShare{
				ProposerID: byzantineID,
				Round:      round,
			},
		}
	}

	gpk, err := newDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints,
		k, eth.SigToPub,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk.qualifyIDs, n-1)
	for _, id := range gpk.qualifyIDs {
		s.NotEqual(id, byzantineID)
	}

	gpk2, err := newDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), complaints[:k],
		k, eth.SigToPub,
	)
	s.Require().NoError(err)
	s.Require().Len(gpk2.qualifyIDs, n)
}

// TestPartialSignature tests if tsigProtocol can handle incorrect partial
// signature and report error.
func (s *DKGTSIGProtocolTestSuite) TestPartialSignature() {
	k := 3
	n := 10
	round := uint64(1)
	gov, err := test.NewGovernance(5, 100)
	s.Require().NoError(err)

	receivers, protocols := s.newProtocols(k, n, round)

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
	gpk, err := newDKGGroupPublicKey(round,
		gov.DKGMasterPublicKeys(round), gov.DKGComplaints(round),
		k, eth.SigToPub,
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

	tsig := newTSigProtocol(gpk)
	msgHash := crypto.Keccak256Hash([]byte("🏖🍹"))
	byzantineID2 := s.nIDs[1]
	for nID, shareSecret := range shareSecrets {
		psig := &types.DKGPartialSignature{
			ProposerID:       nID,
			Round:            round,
			PartialSignature: shareSecret.sign(msgHash),
		}
		if nID == byzantineID2 {
			psig.PartialSignature[0]++
		}
		var err error
		psig.Signature, err = s.prvKeys[nID].Sign(hashDKGPartialSignature(psig))
		s.Require().NoError(err)
		err = tsig.processPartialSignature(msgHash, psig)
		if nID == byzantineID {
			s.Require().Error(ErrNotQualifyDKGParticipant, err)
		} else if nID == byzantineID2 {
			s.Require().Error(ErrIncorrectPartialSignature, err)
		} else {
			s.Require().NoError(err)
		}
	}

	sig, err := tsig.signature()
	s.Require().NoError(err)
	s.True(gpk.verifySignature(msgHash, sig))
}

func TestDKGTSIGProtocol(t *testing.T) {
	suite.Run(t, new(DKGTSIGProtocolTestSuite))
}
