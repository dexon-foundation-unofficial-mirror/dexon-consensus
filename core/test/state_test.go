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

package test

import (
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus-core/core/types/dkg"
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
	pubKey := prvKey.PublicKey()
	nodeID := types.NewNodeID(pubKey)
	// TODO(mission): sign it, and it doesn't make sense to complaint self.
	return &typesDKG.Complaint{
		ProposerID: nodeID,
		Round:      round,
		PrivateShare: typesDKG.PrivateShare{
			ProposerID:   nodeID,
			ReceiverID:   nodeID,
			Round:        round,
			PrivateShare: *dkg.NewPrivateKey(),
		},
	}
}

func (s *StateTestSuite) newDKGFinal(round uint64) *typesDKG.Finalize {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	pubKey := prvKey.PublicKey()
	nodeID := types.NewNodeID(pubKey)
	// TODO(mission): sign it.
	return &typesDKG.Finalize{
		ProposerID: nodeID,
		Round:      round,
	}
}

func (s *StateTestSuite) genNodes(count int) (nodes []crypto.PublicKey) {
	for i := 0; i < count; i++ {
		prv, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nodes = append(nodes, prv.PublicKey())
	}
	return
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
	complaint *typesDKG.Complaint,
	final *typesDKG.Finalize) {
	st.RequestChange(StateAddDKGMasterPublicKey, masterPubKey)
	st.RequestChange(StateAddDKGComplaint, complaint)
	st.RequestChange(StateAddDKGFinal, final)
}

func (s *StateTestSuite) makeConfigChanges(st *State) {
	st.RequestChange(StateChangeNumChains, uint32(7))
	st.RequestChange(StateChangeLambdaBA, time.Nanosecond)
	st.RequestChange(StateChangeLambdaDKG, time.Millisecond)
	st.RequestChange(StateChangeRoundInterval, time.Hour)
	st.RequestChange(StateChangeMinBlockInterval, time.Second)
	st.RequestChange(StateChangeMaxBlockInterval, time.Minute)
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
	req.Equal(config.MaxBlockInterval, time.Minute)
	req.Equal(config.K, 1)
	req.Equal(config.PhiRatio, float32(0.5))
	req.Equal(config.NotarySetSize, uint32(5))
	req.Equal(config.DKGSetSize, uint32(6))
}

func (s *StateTestSuite) TestLocalMode() {
	// Test State with local mode.
	var (
		req    = s.Require()
		lambda = 250 * time.Millisecond
	)
	genesisNodes := s.genNodes(20)
	st := NewState(genesisNodes, lambda, true)
	config1, nodes1 := st.Snapshot()
	req.True(s.compareNodes(genesisNodes, nodes1))
	// Check settings of config1 affected by genesisNodes and lambda.
	req.Equal(config1.NumChains, uint32(len(genesisNodes)))
	req.Equal(config1.LambdaBA, lambda)
	req.Equal(config1.LambdaDKG, lambda*10)
	req.Equal(config1.RoundInterval, lambda*10000)
	req.Equal(config1.MaxBlockInterval, lambda*8)
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
	req.Empty(st.DKGComplaints(2))
	req.False(st.IsDKGFinal(2, 0))
	// Add DKG stuffs.
	masterPubKey := s.newDKGMasterPublicKey(2)
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, comp, final)
	// Check DKGMasterPublicKeys.
	masterKeyForRound := st.DKGMasterPublicKeys(2)
	req.Len(masterKeyForRound, 1)
	req.True(masterKeyForRound[0].Equal(masterPubKey))
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
	genesisNodes := s.genNodes(20)
	st := NewState(genesisNodes, lambda, false)
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
	comp := s.newDKGComplaint(2)
	final := s.newDKGFinal(2)
	s.makeDKGChanges(st, masterPubKey, comp, final)
	// Make sure everything is empty before changed.
	req.Empty(st.DKGMasterPublicKeys(2))
	req.Empty(st.DKGComplaints(2))
	req.False(st.IsDKGFinal(2, 0))
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
	// Check IsDKGFinal.
	req.True(st.IsDKGFinal(2, 0))
}

func TestState(t *testing.T) {
	suite.Run(t, new(StateTestSuite))
}
