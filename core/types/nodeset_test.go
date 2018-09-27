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

package types

import (
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/stretchr/testify/suite"
)

type NodeSetTestSuite struct {
	suite.Suite
}

func (s *NodeSetTestSuite) TestGetSubSet() {
	total := 10
	crs := common.NewRandomHash()
	nodes := NewNodeSet()
	for len(nodes.Nodes) < total {
		nodes.Nodes[NodeID{common.NewRandomHash()}] = struct{}{}
	}
	target := NewNotarySetTarget(crs[:], 0, 0)
	ranks := make(map[NodeID]*nodeRank, len(nodes.Nodes))
	for nID := range nodes.Nodes {
		ranks[nID] = newNodeRank(nID, target)
	}
	size := 4
	notarySet := nodes.GetSubSet(size, target)
	for _, notary := range notarySet {
		win := 0
		rank := ranks[notary].rank
		for node := range nodes.Nodes {
			if rank.Cmp(ranks[node].rank) < 0 {
				win++
			}
		}
		s.True(win >= total-size)
	}
}

func TestNodeSet(t *testing.T) {
	suite.Run(t, new(NodeSetTestSuite))
}
