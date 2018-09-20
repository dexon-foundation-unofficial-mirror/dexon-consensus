// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var (
	baseTime   = time.Now().UTC()
	timeDelay  = 2 * time.Second
	timeExpire = 100 * time.Millisecond
)

type NegativeAckTest struct {
	suite.Suite
}

func (s *NegativeAckTest) SetupSuite() {

}

func (s *NegativeAckTest) SetupTest() {

}

func (s *NegativeAckTest) checkLastVotes(
	nids []types.NodeID,
	vs map[types.NodeID]map[types.NodeID]struct{},
	a [][]bool,
) {

	for i := 0; i < len(nids); i++ {
		for j := 0; j < len(nids); j++ {
			_, exist := vs[nids[i]][nids[j]]
			s.Require().Equal(a[i][j], exist)
		}
	}
}

func (s *NegativeAckTest) checkTimeDiff(
	nids []types.NodeID,
	ts map[types.NodeID]map[types.NodeID]time.Time,
	a [][]int,
) {

	for i := 0; i < len(nids); i++ {
		for j := 0; j < len(nids); j++ {
			s.Require().Equal(
				time.Duration(a[i][j])*timeDelay,
				ts[nids[i]][nids[j]].Sub(baseTime),
			)
		}
	}
}

func genTimestamp(nids []types.NodeID, a []int) map[types.NodeID]time.Time {
	ts := map[types.NodeID]time.Time{}
	for i := 0; i < len(nids); i++ {
		ts[nids[i]] = baseTime.Add(time.Duration(a[i]) * timeDelay)
	}
	return ts
}

func genTestNegativeAck(num int) (*negativeAck, []types.NodeID) {
	nids := test.GenerateRandomNodeIDs(num)
	n := newNegativeAck(nids[0])
	for i := 1; i < num; i++ {
		n.addNode(nids[i])
	}
	return n, nids
}

func (s *NegativeAckTest) TestProcessTimestamps() {
	n, nids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(nids[0], genTimestamp(nids, []int{1, 1, 1, 0}))
	s.checkTimeDiff(nids, n.timeDiffs[nids[0]], [][]int{
		{1, 1, 1, 0},
		{1, 1, 1, 0},
		{1, 1, 1, 0},
		{1, 1, 1, 0},
	})
	s.checkLastVotes(nids, n.lastVotes, [][]bool{
		{false, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
	})

	n.processTimestamps(nids[0], genTimestamp(nids, []int{3, 1, 2, 1}))
	s.checkTimeDiff(nids, n.timeDiffs[nids[0]], [][]int{
		{3, 1, 2, 1},
		{1, 1, 1, 0},
		{3, 1, 2, 1},
		{3, 1, 2, 1},
	})
	s.checkLastVotes(nids, n.lastVotes, [][]bool{
		{false, false, false, false},
		{true, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
	})

	n.processTimestamps(nids[0], genTimestamp(nids, []int{5, 1, 2, 2}))
	s.checkTimeDiff(nids, n.timeDiffs[nids[0]], [][]int{
		{5, 1, 2, 2},
		{1, 1, 1, 0},
		{3, 1, 2, 1},
		{5, 1, 2, 2},
	})
	s.checkLastVotes(nids, n.lastVotes, [][]bool{
		{false, false, false, false},
		{true, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
	})
}

func (s *NegativeAckTest) TestRestrictBySelf() {
	var exist bool
	n, nids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(nids[0], genTimestamp(nids, []int{1, 1, 1, 0}))
	_, exist = n.getRestrictedNodes()[nids[1]]
	s.Require().False(exist)

	n.processTimestamps(nids[0], genTimestamp(nids, []int{3, 1, 2, 1}))
	_, exist = n.getRestrictedNodes()[nids[1]]
	s.Require().True(exist)
}

func (s *NegativeAckTest) TestRestrictByVoting() {
	var nackeds []types.NodeID
	var exist bool

	n, nids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(nids[0], genTimestamp(nids, []int{1, 1, 1, 1}))
	n.processTimestamps(nids[0], genTimestamp(nids, []int{2, 2, 2, 2}))

	n.processTimestamps(nids[1], genTimestamp(nids, []int{1, 1, 1, 1}))
	n.processTimestamps(nids[2], genTimestamp(nids, []int{1, 1, 1, 1}))
	n.processTimestamps(nids[3], genTimestamp(nids, []int{1, 1, 1, 1}))

	nackeds = n.processTimestamps(nids[1], genTimestamp(nids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedNodes()[nids[0]]
	s.Require().False(exist)
	s.Require().Equal(0, len(nackeds))

	nackeds = n.processTimestamps(nids[2], genTimestamp(nids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedNodes()[nids[0]]
	s.Require().True(exist)
	s.Require().Equal(0, len(nackeds))

	nackeds = n.processTimestamps(nids[3], genTimestamp(nids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedNodes()[nids[0]]
	s.Require().False(exist)
	s.Require().Equal(1, len(nackeds))
	s.Require().Equal(nids[0], nackeds[0])
}

func (s *NegativeAckTest) TestExpire() {
	var exist bool

	n, nids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(nids[0], genTimestamp(nids, []int{1, 1, 1, 1}))
	n.processTimestamps(nids[1], genTimestamp(nids, []int{1, 1, 1, 1}))
	n.processTimestamps(nids[2], genTimestamp(nids, []int{1, 1, 1, 1}))
	n.processTimestamps(nids[3], genTimestamp(nids, []int{1, 1, 1, 1}))

	n.processTimestamps(nids[1], genTimestamp(nids, []int{1, 3, 3, 3}))
	n.processTimestamps(nids[2], genTimestamp(nids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedNodes()[nids[0]]
	s.Require().True(exist)

	time.Sleep(2 * timeExpire)

	n.processTimestamps(nids[0], genTimestamp(nids, []int{2, 2, 2, 2}))

	_, exist = n.getRestrictedNodes()[nids[0]]
	s.Require().False(exist)
}

func (s *NegativeAckTest) TestAddDeleteNode() {
	n, nids := genTestNegativeAck(10)
	s.Require().Equal(10, len(n.timeDiffs))
	s.Require().Equal(10, len(n.timeDiffs[nids[0]]))

	for _, nid := range nids {
		n.deleteNode(nid)
	}
	s.Require().Equal(0, len(n.timeDiffs))
}

func TestNegativeAck(t *testing.T) {
	suite.Run(t, new(NegativeAckTest))
}
