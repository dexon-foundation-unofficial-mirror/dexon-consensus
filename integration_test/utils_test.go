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

package integration

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type UtilsTestSuite struct {
	suite.Suite
}

func (s *UtilsTestSuite) TestDecideOwnChains() {
	// Basic test for each node index.
	s.Empty(decideOwnChains(1, 1, 1))
	s.Equal(decideOwnChains(1, 1, 0), []uint32{0})
	s.Equal(decideOwnChains(30, 7, 4), []uint32{4, 11, 18, 25})
	// Make sure every chain is covered.
	isAllCovered := func(numChains uint32, numNodes int) bool {
		if numNodes == 0 {
			decideOwnChains(numChains, numNodes, 0)
			return false
		}
		covered := make(map[uint32]struct{})
		for i := 0; i < numNodes; i++ {
			for _, chainID := range decideOwnChains(numChains, numNodes, i) {
				s.Require().True(chainID < numChains)
				covered[chainID] = struct{}{}
			}
		}
		return uint32(len(covered)) == numChains
	}
	s.True(isAllCovered(100, 33))
	s.True(isAllCovered(100, 200))
	s.True(isAllCovered(100, 50))
	s.True(isAllCovered(100, 1))
	s.Panics(func() {
		isAllCovered(100, 0)
	})
}

func TestUtils(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
