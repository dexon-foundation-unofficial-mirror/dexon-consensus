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

package test

import (
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

type StateTestSuite struct {
	suite.Suite
}

func (s *StateTestSuite) newDKGMasterPublicKey(
	round uint64) *typesDKG.MasterPublicKey {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	pubKey := prvKey.PublicKey()
	nodeID := types.NewNodeID(pubKey)
	_, pubShare := dkg.NewPrivateKeyShares(3)
	dID, err := dkg.BytesID(nodeID.Hash[:])
	s.Require().NoError(err)
	return &typesDKG.MasterPublicKey{
		ProposerID:      nodeID,
		Round:           round,
		DKGID:           dID,
		PublicKeyShares: *pubShare,
	}
}

func (s *StateTestSuite) newDKGComplaint(round uint64) *typesDKG.Complaint {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	nodeID := types.NewNodeID(prvKey.PublicKey())
	comp := &typesDKG.Complaint{
		Round: round,
		PrivateShare: typesDKG.PrivateShare{
			ProposerID:   nodeID,
			ReceiverID:   nodeID,
			Round:        round,
			PrivateShare: *dkg.NewPrivateKey(),
		},
	}
	signer := utils.NewSigner(prvKey)
	s.Require().NoError(signer.SignDKGComplaint(comp))
	s.Require().NoError(signer.SignDKGPrivateShare(&comp.PrivateShare))
	s.Require().False(comp.IsNack())
	return comp
}

func (s *StateTestSuite) newDKGMPKReady(round uint64) *typesDKG.MPKReady {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	ready := &typesDKG.MPKReady{Round: round}
	s.Require().NoError(utils.NewSigner(prvKey).SignDKGMPKReady(ready))
	return ready
}
func (s *StateTestSuite) newDKGFinal(round uint64) *typesDKG.Finalize {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	final := &typesDKG.Finalize{Round: round}
	s.Require().NoError(utils.NewSigner(prvKey).SignDKGFinalize(final))
	return final
}

func (s *StateTestSuite) compareNodes(node1, node2 []crypto.PublicKey) bool {
	id1 := common.Hashes{}
	for _, n := range node1 {
		id1 = append(id1, types.NewNodeID(n).Hash)
	}
	sort.Sort(id1)
	id2 := common.Hashes{}
	for _, n := range node2 {
		id2 = append(id2, types.NewNodeID(n).Hash)
	}
	sort.Sort(id2)
	if len(id1) != len(id2) {
		return false
	}
	for idx, id := range id1 {
		if id != id2[idx] {
			return false
		}
	}
	return true
}

func (s *StateTestSuite) findNode(
	nodes []crypto.PublicKey, node crypto.PublicKey) bool {
	nodeID := types.NewNodeID(node)
	for _, n := range nodes {
		nID := types.NewNodeID(n)
		if nID == nodeID {
			return true
		}
	}
	return false
}

func (s *StateTestSuite) makeDKGChanges(
	st *State,
	masterPubKey *typesDKG.MasterPublicKey,
	ready *typesDKG.MPKReady,
	complaint *typesDKG.Complaint,
	final *typesDKG.Finalize) {
	st.RequestChange(StateAddDKGMasterPublicKey, masterPubKey)
	st.RequestChange(StateAddDKGMPKReady, ready)
	st.RequestChange(StateAddDKGComplaint, complaint)
	st.RequestChange(StateAddDKGFinal, final)
}

func (s *StateTestSuite) makeConfigChanges(st *State) {
	st.RequestChange(StateChangeNumChains, uint32(7))
	st.RequestChange(StateChangeLambdaBA, time.Nanosecond)
	st.RequestChange(StateChangeLambdaDKG, time.Millisecond)
	st.RequestChange(StateChangeRoundInterval, time.Hour)
	st.RequestChange(StateChangeMinBlockInterval, time.Second)
	st.RequestChange(StateChangeK, 1)
	st.RequestChange(StateChangePhiRatio, float32(0.5))
	st.RequestChange(StateChangeNotarySetSize, uint32(5))
	st.RequestChange(StateChangeDKGSetSize, uint32(6))
}

func (s *StateTestSuite) checkConfigChanges(config *types.Config) {
	req := s.Require()
	req.Equal(config.NumChains, uint32(7))
	req.Equal(config.LambdaBA, time.Nanosecond)
	req.Equal(config.LambdaDKG, time.Millisecond)
	req.Equal(config.RoundInterval, time.Hour)
	req.Equal(config.MinBlockInterval, time.Second)
	req.Equal(config.K, 1)
	req.Equal(config.PhiRatio, float32(0.5))
	req.Equal(config.NotarySetSize, uint32(5))
	req.Equal(config.DKGSetSize, uint32(6))
}

func (s *StateTestSuite) TestEqual() {
	var (
		req    = s.Require()
		lambda = 250 * time.Millisecond
	)
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	st := NewState(genesisNodes, lambda, &common.NullLogger{}, true)
	req.NoError(st.Equal(st))
	// One node is missing.
	st1 := NewState(genesisNodes, lambda, &common.NullLogger{}, true)
	for nID := range st1.nodes {
		delete(st1.nodes, nID)
		break
	}
	req.Equal(st.Equal(st1), ErrStateNodeSetNotEqual)
	// Make some changes.
	st2 := st.Clone()
	req.NoError(st.Equal(st2))
	s.makeConfigChanges(st)
	req.Equal(st.Equal(st2), ErrStateConfigNotEqual)
	crs := common.NewRandomHash()
	req.NoError(st.ProposeCRS(1, crs))
	masterPubKey := s.newDKGMasterPublicKey(2)
	ready := s.newDKGMPKReady(2)
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, ready, comp, final)
	// Remove dkg complaints from cloned one to check if equal.
	st3 := st.Clone()
	req.NoError(st.Equal(st3))
	delete(st3.dkgComplaints, uint64(2))
	req.Equal(st.Equal(st3), ErrStateDKGComplaintsNotEqual)
	// Remove dkg master public key from cloned one to check if equal.
	st4 := st.Clone()
	req.NoError(st.Equal(st4))
	delete(st4.dkgMasterPublicKeys, uint64(2))
	req.Equal(st.Equal(st4), ErrStateDKGMasterPublicKeysNotEqual)
	// Remove dkg ready from cloned one to check if equal.
	st4a := st.Clone()
	req.NoError(st.Equal(st4a))
	delete(st4a.dkgReadys, uint64(2))
	req.Equal(st.Equal(st4a), ErrStateDKGMPKReadysNotEqual)
	// Remove dkg finalize from cloned one to check if equal.
	st5 := st.Clone()
	req.NoError(st.Equal(st5))
	delete(st5.dkgFinals, uint64(2))
	req.Equal(st.Equal(st5), ErrStateDKGFinalsNotEqual)
	// Switch to remote mode.
	st.SwitchToRemoteMode()
	// Make some change.
	req.NoError(st.RequestChange(StateChangeK, int(5)))
	st6 := st.Clone()
	req.NoError(st.Equal(st6))
	// Remove the pending change, should not be equal.
	req.Len(st6.ownRequests, 1)
	for k := range st6.ownRequests {
		delete(st6.ownRequests, k)
	}
	req.Error(ErrStatePendingChangesNotEqual, st.Equal(st6))
}

func (s *StateTestSuite) TestPendingChangesEqual() {
	var (
		req    = s.Require()
		lambda = 250 * time.Millisecond
	)
	// Setup a non-local mode State instance.
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	st := NewState(genesisNodes, lambda, &common.NullLogger{}, false)
	req.NoError(st.Equal(st))
	// Apply some changes.
	s.makeConfigChanges(st)
	crs := common.NewRandomHash()
	req.NoError(st.ProposeCRS(1, crs))
	masterPubKey := s.newDKGMasterPublicKey(2)
	ready := s.newDKGMPKReady(2)
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, ready, comp, final)
}

func (s *StateTestSuite) TestLocalMode() {
	// Test State with local mode.
	var (
		req    = s.Require()
		lambda = 250 * time.Millisecond
	)
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	st := NewState(genesisNodes, lambda, &common.NullLogger{}, true)
	config1, nodes1 := st.Snapshot()
	req.True(s.compareNodes(genesisNodes, nodes1))
	// Check settings of config1 affected by genesisNodes and lambda.
	req.Equal(config1.NumChains, uint32(len(genesisNodes)))
	req.Equal(config1.LambdaBA, lambda)
	req.Equal(config1.LambdaDKG, lambda*10)
	req.Equal(config1.RoundInterval, lambda*10000)
	req.Equal(config1.NotarySetSize, uint32(len(genesisNodes)))
	req.Equal(config1.DKGSetSize, uint32(len(genesisNodes)))
	req.Equal(config1.K, 0)
	req.Equal(config1.PhiRatio, float32(0.667))
	// Request some changes, every fields for config should be affected.
	s.makeConfigChanges(st)
	// Add new node.
	prvKey, err := ecdsa.NewPrivateKey()
	req.NoError(err)
	pubKey := prvKey.PublicKey()
	st.RequestChange(StateAddNode, pubKey)
	config2, newNodes := st.Snapshot()
	// Check if config changes are applied.
	s.checkConfigChanges(config2)
	// Check if new node is added.
	req.True(s.findNode(newNodes, pubKey))
	// Test adding CRS.
	crs := common.NewRandomHash()
	req.NoError(st.ProposeCRS(1, crs))
	req.Equal(st.CRS(1), crs)
	// Test adding node set, DKG complaints, final, master public key.
	// Make sure everything is empty before changed.
	req.Empty(st.DKGMasterPublicKeys(2))
	req.False(st.IsDKGMPKReady(2, 0))
	req.Empty(st.DKGComplaints(2))
	req.False(st.IsDKGFinal(2, 0))
	// Add DKG stuffs.
	masterPubKey := s.newDKGMasterPublicKey(2)
	ready := s.newDKGMPKReady(2)
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, ready, comp, final)
	// Check DKGMasterPublicKeys.
	masterKeyForRound := st.DKGMasterPublicKeys(2)
	req.Len(masterKeyForRound, 1)
	req.True(masterKeyForRound[0].Equal(masterPubKey))
	// Check IsDKGMPKReady.
	req.True(st.IsDKGMPKReady(2, 0))
	// Check DKGComplaints.
	compForRound := st.DKGComplaints(2)
	req.Len(compForRound, 1)
	req.True(compForRound[0].Equal(comp))
	// Check IsDKGFinal.
	req.True(st.IsDKGFinal(2, 0))
}

func (s *StateTestSuite) TestPacking() {
	// Make sure everything works when requests are packing
	// and unpacked to apply.
	var (
		req    = s.Require()
		lambda = 250 * time.Millisecond
	)
	// Make config changes.
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	st := NewState(genesisNodes, lambda, &common.NullLogger{}, false)
	s.makeConfigChanges(st)
	// Add new CRS.
	crs := common.NewRandomHash()
	req.NoError(st.ProposeCRS(1, crs))
	// Add new node.
	prvKey, err := ecdsa.NewPrivateKey()
	req.NoError(err)
	pubKey := prvKey.PublicKey()
	st.RequestChange(StateAddNode, pubKey)
	// Add DKG stuffs.
	masterPubKey := s.newDKGMasterPublicKey(2)
	ready := s.newDKGMPKReady(2)
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, ready, comp, final)
	// Make sure everything is empty before changed.
	req.Empty(st.DKGMasterPublicKeys(2))
	req.False(st.IsDKGMPKReady(2, 0))
	req.Empty(st.DKGComplaints(2))
	req.False(st.IsDKGFinal(2, 0))
	// In remote mode, we need to manually convert own requests to global ones.
	_, err = st.PackOwnRequests()
	req.NoError(err)
	// Pack changes into bytes.
	b, err := st.PackRequests()
	req.NoError(err)
	req.NotEmpty(b)
	// Apply those bytes back.
	req.NoError(st.Apply(b))
	// Check if configs are changed.
	config, nodes := st.Snapshot()
	s.checkConfigChanges(config)
	// Check if CRS is added.
	req.Equal(st.CRS(1), crs)
	// Check if new node is added.
	req.True(s.findNode(nodes, pubKey))
	// Check DKGMasterPublicKeys.
	masterKeyForRound := st.DKGMasterPublicKeys(2)
	req.Len(masterKeyForRound, 1)
	req.True(masterKeyForRound[0].Equal(masterPubKey))
	// Check DKGComplaints.
	compForRound := st.DKGComplaints(2)
	req.Len(compForRound, 1)
	req.True(compForRound[0].Equal(comp))
	// Check IsDKGMPKReady.
	req.True(st.IsDKGMPKReady(2, 0))
	// Check IsDKGFinal.
	req.True(st.IsDKGFinal(2, 0))
}

func (s *StateTestSuite) TestRequestBroadcastAndPack() {
	// This test case aims to demonstrate this scenario:
	// - a change request is pending at one node.
	// - that request can be packed by PackOwnRequests and sent to other nodes.
	// - when some other node allow to propose a block, it will pack all those
	//   'own' requests from others into the block's payload.
	// - when all nodes receive that block, all pending requests (including
	//   those 'own' requests) would be cleaned.
	var (
		req    = s.Require()
		lambda = 250 * time.Millisecond
	)
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	st := NewState(genesisNodes, lambda, &common.NullLogger{}, false)
	st1 := NewState(genesisNodes, lambda, &common.NullLogger{}, false)
	req.NoError(st.Equal(st1))
	// Make configuration changes.
	s.makeConfigChanges(st)
	// Add new CRS.
	crs := common.NewRandomHash()
	req.NoError(st.ProposeCRS(1, crs))
	// Add new node.
	prvKey, err := ecdsa.NewPrivateKey()
	req.NoError(err)
	pubKey := prvKey.PublicKey()
	st.RequestChange(StateAddNode, pubKey)
	// Add DKG stuffs.
	masterPubKey := s.newDKGMasterPublicKey(2)
	ready := s.newDKGMPKReady(2)
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, ready, comp, final)
	// Pack those changes into a byte stream, and pass it to other State
	// instance.
	packed, err := st.PackOwnRequests()
	req.NoError(err)
	req.NotEmpty(packed)
	// The second attempt to pack would get empty result.
	emptyPackedAsByte, err := st.PackOwnRequests()
	req.NoError(err)
	emptyPacked, err := st.unpackRequests(emptyPackedAsByte)
	req.NoError(err)
	req.Empty(emptyPacked)
	// Pass it to others.
	req.NoError(st1.AddRequestsFromOthers(packed))
	// These two instance are equal now, because their pending change requests
	// are synced.
	req.NoError(st.Equal(st1))
	// Make them apply those pending changes.
	applyChangesForRemoteState := func(s *State) {
		p, err := s.PackRequests()
		req.NoError(err)
		req.NotEmpty(p)
		req.NoError(s.Apply(p))
	}
	applyChangesForRemoteState(st)
	applyChangesForRemoteState(st1)
	// They should be equal after applying those changes.
	req.NoError(st.Equal(st1))
}

func TestState(t *testing.T) {
	suite.Run(t, new(StateTestSuite))
}
