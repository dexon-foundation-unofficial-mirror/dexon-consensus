// Copyright 2019 The dexon-consensus Authors
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

package utils

import (
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type VoteFilterTestSuite struct {
	suite.Suite
}

func (s *VoteFilterTestSuite) TestFilterVotePass() {
	filter := NewVoteFilter()
	filter.Position.Height = uint64(6)
	filter.Period = uint64(3)
	filter.LockIter = uint64(3)
	// Pass with higher Height.
	vote := types.NewVote(types.VotePreCom, common.NewRandomHash(), uint64(1))
	vote.Position.Height = filter.Position.Height + 1
	s.Require().False(filter.Filter(vote))
	// Pass with VotePreCom.
	vote = types.NewVote(types.VotePreCom, common.NewRandomHash(),
		filter.LockIter)
	vote.Position.Height = filter.Position.Height
	s.Require().False(filter.Filter(vote))
	// Pass with VoteCom.
	vote = types.NewVote(types.VoteCom, common.NewRandomHash(),
		filter.Period)
	vote.Position.Height = filter.Position.Height
	s.Require().False(filter.Filter(vote))
	vote.Period--
	s.Require().False(filter.Filter(vote))
}

func (s *VoteFilterTestSuite) TestFilterVoteInit() {
	filter := NewVoteFilter()
	vote := types.NewVote(types.VoteInit, common.NewRandomHash(), uint64(1))
	s.True(filter.Filter(vote))
}

func (s *VoteFilterTestSuite) TestFilterVotePreCom() {
	filter := NewVoteFilter()
	filter.LockIter = uint64(3)
	vote := types.NewVote(types.VotePreCom, common.NewRandomHash(), uint64(1))
	s.True(filter.Filter(vote))
}

func (s *VoteFilterTestSuite) TestFilterVoteCom() {
	filter := NewVoteFilter()
	filter.Period = uint64(3)
	vote := types.NewVote(types.VoteCom, types.SkipBlockHash, uint64(1))
	s.True(filter.Filter(vote))
}

func (s *VoteFilterTestSuite) TestFilterConfirm() {
	filter := NewVoteFilter()
	filter.Confirm = true
	vote := types.NewVote(types.VoteCom, common.NewRandomHash(), uint64(1))
	s.True(filter.Filter(vote))
}
func (s *VoteFilterTestSuite) TestFilterLowerHeight() {
	filter := NewVoteFilter()
	filter.Position.Height = uint64(10)
	vote := types.NewVote(types.VoteCom, common.NewRandomHash(), uint64(1))
	vote.Position.Height = filter.Position.Height - 1
	s.True(filter.Filter(vote))
}

func (s *VoteFilterTestSuite) TestFilterSameVote() {
	filter := NewVoteFilter()
	vote := types.NewVote(types.VoteCom, common.NewRandomHash(), uint64(5))
	s.False(filter.Filter(vote))
	filter.AddVote(vote)
	s.True(filter.Filter(vote))
}

func TestVoteFilter(t *testing.T) {
	suite.Run(t, new(VoteFilterTestSuite))
}
