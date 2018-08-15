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
	vids []types.ValidatorID,
	vs map[types.ValidatorID]map[types.ValidatorID]struct{},
	a [][]bool,
) {

	for i := 0; i < len(vids); i++ {
		for j := 0; j < len(vids); j++ {
			_, exist := vs[vids[i]][vids[j]]
			s.Require().Equal(a[i][j], exist)
		}
	}
}

func (s *NegativeAckTest) checkTimeDiff(
	vids []types.ValidatorID,
	ts map[types.ValidatorID]map[types.ValidatorID]time.Time,
	a [][]int,
) {

	for i := 0; i < len(vids); i++ {
		for j := 0; j < len(vids); j++ {
			s.Require().Equal(
				time.Duration(a[i][j])*timeDelay,
				ts[vids[i]][vids[j]].Sub(baseTime),
			)
		}
	}
}

func genTimestamp(vids []types.ValidatorID, a []int) map[types.ValidatorID]time.Time {
	ts := map[types.ValidatorID]time.Time{}
	for i := 0; i < len(vids); i++ {
		ts[vids[i]] = baseTime.Add(time.Duration(a[i]) * timeDelay)
	}
	return ts
}

func genTestNegativeAck(num int) (*negativeAck, []types.ValidatorID) {
	vids := test.GenerateRandomValidatorIDs(num)
	n := newNegativeAck(vids[0])
	for i := 1; i < num; i++ {
		n.addValidator(vids[i])
	}
	return n, vids
}

func (s *NegativeAckTest) TestProcessTimestamps() {
	n, vids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(vids[0], genTimestamp(vids, []int{1, 1, 1, 0}))
	s.checkTimeDiff(vids, n.timeDiffs[vids[0]], [][]int{
		{1, 1, 1, 0},
		{1, 1, 1, 0},
		{1, 1, 1, 0},
		{1, 1, 1, 0},
	})
	s.checkLastVotes(vids, n.lastVotes, [][]bool{
		{false, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
	})

	n.processTimestamps(vids[0], genTimestamp(vids, []int{3, 1, 2, 1}))
	s.checkTimeDiff(vids, n.timeDiffs[vids[0]], [][]int{
		{3, 1, 2, 1},
		{1, 1, 1, 0},
		{3, 1, 2, 1},
		{3, 1, 2, 1},
	})
	s.checkLastVotes(vids, n.lastVotes, [][]bool{
		{false, false, false, false},
		{true, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
	})

	n.processTimestamps(vids[0], genTimestamp(vids, []int{5, 1, 2, 2}))
	s.checkTimeDiff(vids, n.timeDiffs[vids[0]], [][]int{
		{5, 1, 2, 2},
		{1, 1, 1, 0},
		{3, 1, 2, 1},
		{5, 1, 2, 2},
	})
	s.checkLastVotes(vids, n.lastVotes, [][]bool{
		{false, false, false, false},
		{true, false, false, false},
		{false, false, false, false},
		{false, false, false, false},
	})
}

func (s *NegativeAckTest) TestRestrictBySelf() {
	var exist bool
	n, vids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(vids[0], genTimestamp(vids, []int{1, 1, 1, 0}))
	_, exist = n.getRestrictedValidators()[vids[1]]
	s.Require().False(exist)

	n.processTimestamps(vids[0], genTimestamp(vids, []int{3, 1, 2, 1}))
	_, exist = n.getRestrictedValidators()[vids[1]]
	s.Require().True(exist)
}

func (s *NegativeAckTest) TestRestrictByVoting() {
	var nackeds []types.ValidatorID
	var exist bool

	n, vids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(vids[0], genTimestamp(vids, []int{1, 1, 1, 1}))
	n.processTimestamps(vids[0], genTimestamp(vids, []int{2, 2, 2, 2}))

	n.processTimestamps(vids[1], genTimestamp(vids, []int{1, 1, 1, 1}))
	n.processTimestamps(vids[2], genTimestamp(vids, []int{1, 1, 1, 1}))
	n.processTimestamps(vids[3], genTimestamp(vids, []int{1, 1, 1, 1}))

	nackeds = n.processTimestamps(vids[1], genTimestamp(vids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedValidators()[vids[0]]
	s.Require().False(exist)
	s.Require().Equal(0, len(nackeds))

	nackeds = n.processTimestamps(vids[2], genTimestamp(vids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedValidators()[vids[0]]
	s.Require().True(exist)
	s.Require().Equal(0, len(nackeds))

	nackeds = n.processTimestamps(vids[3], genTimestamp(vids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedValidators()[vids[0]]
	s.Require().False(exist)
	s.Require().Equal(1, len(nackeds))
	s.Require().Equal(vids[0], nackeds[0])
}

func (s *NegativeAckTest) TestExpire() {
	var exist bool

	n, vids := genTestNegativeAck(4)
	n.setTimeDelay(timeDelay)
	n.setTimeExpire(timeExpire)

	n.processTimestamps(vids[0], genTimestamp(vids, []int{1, 1, 1, 1}))
	n.processTimestamps(vids[1], genTimestamp(vids, []int{1, 1, 1, 1}))
	n.processTimestamps(vids[2], genTimestamp(vids, []int{1, 1, 1, 1}))
	n.processTimestamps(vids[3], genTimestamp(vids, []int{1, 1, 1, 1}))

	n.processTimestamps(vids[1], genTimestamp(vids, []int{1, 3, 3, 3}))
	n.processTimestamps(vids[2], genTimestamp(vids, []int{1, 3, 3, 3}))
	_, exist = n.getRestrictedValidators()[vids[0]]
	s.Require().True(exist)

	time.Sleep(2 * timeExpire)

	n.processTimestamps(vids[0], genTimestamp(vids, []int{2, 2, 2, 2}))

	_, exist = n.getRestrictedValidators()[vids[0]]
	s.Require().False(exist)
}

func (s *NegativeAckTest) TestAddDeleteValidator() {
	n, vids := genTestNegativeAck(10)
	s.Require().Equal(10, len(n.timeDiffs))
	s.Require().Equal(10, len(n.timeDiffs[vids[0]]))

	for _, vid := range vids {
		n.deleteValidator(vid)
	}
	s.Require().Equal(0, len(n.timeDiffs))
}

func TestNegativeAck(t *testing.T) {
	suite.Run(t, new(NegativeAckTest))
}
