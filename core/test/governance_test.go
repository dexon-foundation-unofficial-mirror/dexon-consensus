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

func TestGovernance(t *testing.T) {
	suite.Run(t, new(GovernanceTestSuite))
}
