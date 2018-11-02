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
	g1, err := NewGovernance(genesisNodes, 100*time.Millisecond)
	req.NoError(err)
	// Create a governance with different lambda.
	g2, err := NewGovernance(genesisNodes, 50*time.Millisecond)
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
}

func (s *GovernanceTestSuite) TestRegisterChange() {
	req := s.Require()
	_, genesisNodes, err := NewKeys(20)
	req.NoError(err)
	g, err := NewGovernance(genesisNodes, 100*time.Millisecond)
	req.NoError(err)
	// Unable to register change for genesis round.
	req.Error(g.RegisterConfigChange(0, StateChangeNumChains, uint32(32)))
	// Make some round prepared.
	g.CatchUpWithRound(4)
	req.Equal(g.Configuration(4).NumChains, uint32(20))
	// Unable to register change for prepared round.
	req.Error(g.RegisterConfigChange(4, StateChangeNumChains, uint32(32)))
	// Unable to register change for next notified round.
	req.Error(g.RegisterConfigChange(5, StateChangeNumChains, uint32(32)))
	// It's ok to make some change when condition is met.
	req.NoError(g.RegisterConfigChange(6, StateChangeNumChains, uint32(32)))
	req.NoError(g.RegisterConfigChange(7, StateChangeNumChains, uint32(40)))
	// In local mode, state for round 6 would be ready after notified with
	// round 5.
	g.NotifyRoundHeight(5, 0)
	// In local mode, state for round 7 would be ready after notified with
	// round 6.
	g.NotifyRoundHeight(6, 0)
	// Notify governance to take a snapshot for round 7's configuration.
	g.NotifyRoundHeight(7, 0)
	req.Equal(g.Configuration(6).NumChains, uint32(32))
	req.Equal(g.Configuration(7).NumChains, uint32(40))
}

func TestGovernance(t *testing.T) {
	suite.Run(t, new(GovernanceTestSuite))
}
