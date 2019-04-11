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
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// VoteFilter filters votes that are useless for now.
// To maximize performance, this structure is not thread-safe and will never be.
type VoteFilter struct {
	Voted    map[types.VoteHeader]struct{}
	Position types.Position
	LockIter uint64
	Period   uint64
	Confirm  bool
}

// NewVoteFilter creates a new vote filter instance.
func NewVoteFilter() *VoteFilter {
	return &VoteFilter{
		Voted: make(map[types.VoteHeader]struct{}),
	}
}

// Filter checks if the vote should be filtered out.
func (vf *VoteFilter) Filter(vote *types.Vote) bool {
	if vote.Type == types.VoteInit {
		return true
	}
	if vote.Position.Older(vf.Position) {
		return true
	} else if vote.Position.Newer(vf.Position) {
		// It's impossible to check the vote of other height.
		return false
	}
	if vf.Confirm {
		return true
	}
	if vote.Type == types.VotePreCom && vote.Period < vf.LockIter {
		return true
	}
	if vote.Type == types.VoteCom &&
		vote.Period < vf.Period &&
		vote.BlockHash == types.SkipBlockHash {
		return true
	}
	if _, exist := vf.Voted[vote.VoteHeader]; exist {
		return true
	}
	return false
}

// AddVote to the filter so the same vote will be filtered.
func (vf *VoteFilter) AddVote(vote *types.Vote) {
	vf.Voted[vote.VoteHeader] = struct{}{}
}
