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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

type GovernanceTestSuite struct {
	suite.Suite
}

func (s *GovernanceTestSuite) TestEqual() {
	var req = s.Require()
	// Setup a base governance.
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	g1, err := NewGovernance(NewState(
		1, genesisNodes, 100*time.Millisecond, &common.NullLogger{}, true), 2)
	req.NoError(err)
	// Create a governance with different lambda.
	g2, err := NewGovernance(NewState(
		1, genesisNodes, 50*time.Millisecond, &common.NullLogger{}, true), 2)
	req.NoError(err)
	req.False(g1.Equal(g2, true))
	// Create configs for 3 rounds for g1.
	g1.CatchUpWithRound(3)
	// Make a clone.
	g3 := g1.Clone()
	req.True(g1.Equal(g3, true))
	// Create a new round for g1.
	g1.CatchUpWithRound(4)
	req.False(g1.Equal(g3, true))
	// Make another clone.
	g4 := g1.Clone()
	req.True(g1.Equal(g4, true))
	// Add a node to g4.
	_, newNodes, err := NewKeys(1)
	req.NoError(err)
	g4.State().RequestChange(StateAddNode, newNodes[0])
	g1.CatchUpWithRound(5)
	g4.CatchUpWithRound(5)
	req.False(g1.Equal(g4, true))
	// Make a clone.
	g5 := g1.Clone()
	// Change its roundShift
	g5.roundShift = 3
	req.False(g1.Equal(g5, true))
}

func (s *GovernanceTestSuite) TestRegisterChange() {
	req := s.Require()
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	g, err := NewGovernance(NewState(
		1, genesisNodes, 100*time.Millisecond, &common.NullLogger{}, true), 2)
	req.NoError(err)
	// Unable to register change for genesis round.
	req.Error(g.RegisterConfigChange(0, StateChangeDKGSetSize, uint32(32)))
	// Make some round prepared.
	g.CatchUpWithRound(4)
	req.Equal(g.Configuration(4).DKGSetSize, uint32(20))
	// Unable to register change for prepared round.
	req.Error(g.RegisterConfigChange(4, StateChangeDKGSetSize, uint32(32)))
	// It's ok to make some change when condition is met.
	req.NoError(g.RegisterConfigChange(5, StateChangeDKGSetSize, uint32(32)))
	req.NoError(g.RegisterConfigChange(6, StateChangeDKGSetSize, uint32(32)))
	req.NoError(g.RegisterConfigChange(7, StateChangeDKGSetSize, uint32(40)))
	// In local mode, state for round 6 would be ready after notified with
	// round 2.
	g.NotifyRound(2)
	g.NotifyRound(3)
	// In local mode, state for round 7 would be ready after notified with
	// round 6.
	g.NotifyRound(4)
	// Notify governance to take a snapshot for round 7's configuration.
	g.NotifyRound(5)
	req.Equal(g.Configuration(6).DKGSetSize, uint32(32))
	req.Equal(g.Configuration(7).DKGSetSize, uint32(40))
}

func (s *GovernanceTestSuite) TestProhibit() {
	round := uint64(1)
	prvKeys, genesisNodes, err := NewKeys(4)
	s.Require().NoError(err)
	gov, err := NewGovernance(NewState(
		1, genesisNodes, 100*time.Millisecond, &common.NullLogger{}, true), 2)
	s.Require().NoError(err)
	// Test MPK.
	proposeMPK := func(k crypto.PrivateKey) {
		signer := utils.NewSigner(k)
		_, pubShare := dkg.NewPrivateKeyShares(utils.GetDKGThreshold(
			gov.Configuration(round)))
		mpk := &typesDKG.MasterPublicKey{
			Round:           round,
			DKGID:           typesDKG.NewID(types.NewNodeID(k.PublicKey())),
			PublicKeyShares: *pubShare,
		}
		s.Require().NoError(signer.SignDKGMasterPublicKey(mpk))
		gov.AddDKGMasterPublicKey(round, mpk)
	}
	proposeMPK(prvKeys[0])
	s.Require().Len(gov.DKGMasterPublicKeys(round), 1)
	gov.Prohibit(StateAddDKGMasterPublicKey)
	proposeMPK(prvKeys[1])
	s.Require().Len(gov.DKGMasterPublicKeys(round), 1)
	gov.Unprohibit(StateAddDKGMasterPublicKey)
	proposeMPK(prvKeys[1])
	s.Require().Len(gov.DKGMasterPublicKeys(round), 2)
	// Test Complaint.
	proposeComplaint := func(k crypto.PrivateKey) {
		signer := utils.NewSigner(k)
		comp := &typesDKG.Complaint{
			ProposerID: types.NewNodeID(k.PublicKey()),
			Round:      round,
		}
		s.Require().NoError(signer.SignDKGComplaint(comp))
		gov.AddDKGComplaint(round, comp)
	}
	proposeComplaint(prvKeys[0])
	s.Require().Len(gov.DKGComplaints(round), 1)
	gov.Prohibit(StateAddDKGComplaint)
	proposeComplaint(prvKeys[1])
	s.Require().Len(gov.DKGComplaints(round), 1)
	gov.Unprohibit(StateAddDKGComplaint)
	proposeComplaint(prvKeys[1])
	s.Require().Len(gov.DKGComplaints(round), 2)
	// Test DKG Final.
	proposeFinal := func(k crypto.PrivateKey) {
		signer := utils.NewSigner(k)
		final := &typesDKG.Finalize{
			Round:      round,
			ProposerID: types.NewNodeID(k.PublicKey()),
		}
		s.Require().NoError(signer.SignDKGFinalize(final))
		gov.AddDKGFinalize(round, final)
	}
	gov.Prohibit(StateAddDKGFinal)
	for _, k := range prvKeys {
		proposeFinal(k)
	}
	s.Require().False(gov.IsDKGFinal(round))
	gov.Unprohibit(StateAddDKGFinal)
	for _, k := range prvKeys {
		proposeFinal(k)
	}
	s.Require().True(gov.IsDKGFinal(round))
}

func TestGovernance(t *testing.T) {
	suite.Run(t, new(GovernanceTestSuite))
}
