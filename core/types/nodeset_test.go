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

package types

import (
	"testing"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/stretchr/testify/suite"
)

type NodeSetTestSuite struct {
	suite.Suite
}

func (s *NodeSetTestSuite) TestGetSubSet() {
	total := 10
	crs := common.NewRandomHash()
	nodes := NewNodeSet()
	for len(nodes.IDs) < total {
		nodes.IDs[NodeID{common.NewRandomHash()}] = struct{}{}
	}
	target := NewNotarySetTarget(crs)
	ranks := make(map[NodeID]*nodeRank, len(nodes.IDs))
	for nID := range nodes.IDs {
		ranks[nID] = newNodeRank(nID, target)
	}
	size := 4
	notarySet := nodes.GetSubSet(size, target)
	for notary := range notarySet {
		win := 0
		rank := ranks[notary].rank
		for node := range nodes.IDs {
			if rank.Cmp(ranks[node].rank) < 0 {
				win++
			}
		}
		s.True(win >= total-size)
	}
}

func (s *NodeSetTestSuite) TestGetSubSetZeroSize() {
	total := 10
	nodes := NewNodeSet()
	for len(nodes.IDs) < total {
		nodes.IDs[NodeID{common.NewRandomHash()}] = struct{}{}
	}
	// Passing nil should not crash.
	emptySet := nodes.GetSubSet(0, nil)
	s.Len(emptySet, 0)
}

func TestNodeSet(t *testing.T) {
	suite.Run(t, new(NodeSetTestSuite))
}
