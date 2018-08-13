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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type negativeAck struct {
	// owner is the ID of proposer itself, this is used when deciding
	// a validator to be restricted or not.
	owner types.ValidatorID

	numOfValidators int

	// timeDelay and timeExpire are for nack timeout.
	timeDelay  time.Duration
	timeExpire time.Duration

	// restricteds stores validators which has been restricted and the time it's
	// restricted.
	restricteds map[types.ValidatorID]time.Time

	// lastVotes and lockedVotes store the votes for nack. lastVotes[vid1][vid2]
	// and lockedVotes[vid1][vid2] both mean that vid2 votes vid1. The difference
	// is lockedVotes works only when vid1 is restricted, so that the votes are
	// needed to be locked.
	lastVotes   map[types.ValidatorID]map[types.ValidatorID]struct{}
	lockedVotes map[types.ValidatorID]map[types.ValidatorID]struct{}

	// timeDiffs is the cache for last time stamps. timeDiffs[vid1][vid2] means
	// the last updated timestamps vid1 sees vid2.
	timeDiffs map[types.ValidatorID]map[types.ValidatorID]map[types.ValidatorID]time.Time
}

// newNegativeAck creates a new negaticeAck instance.
func newNegativeAck(vid types.ValidatorID) *negativeAck {
	n := &negativeAck{
		owner:           vid,
		numOfValidators: 0,
		restricteds:     make(map[types.ValidatorID]time.Time),
		lastVotes:       make(map[types.ValidatorID]map[types.ValidatorID]struct{}),
		lockedVotes:     make(map[types.ValidatorID]map[types.ValidatorID]struct{}),
		timeDiffs:       make(map[types.ValidatorID]map[types.ValidatorID]map[types.ValidatorID]time.Time),
	}
	n.addValidator(vid)
	return n
}

// processNewVote is called when a new "vote" occurs, that is, a validator
// sees that other 2f + 1 validators think a validator is slow. "vid" is the
// validator which propesed the block which the timestamps votes and "h" is
// the validator been voted to be nacked.
func (n *negativeAck) processNewVote(
	vid types.ValidatorID,
	h types.ValidatorID,
) []types.ValidatorID {

	nackeds := []types.ValidatorID{}
	if _, exist := n.restricteds[h]; exist {
		n.lockedVotes[h][vid] = struct{}{}
		if len(n.lockedVotes[h]) > 2*(n.numOfValidators-1)/3 {
			nackeds = append(nackeds, h)
			delete(n.restricteds, h)
		}
	} else {
		if n.owner == vid {
			n.restrict(h)
		} else {
			n.lastVotes[h][vid] = struct{}{}
			if len(n.lastVotes[h]) > (n.numOfValidators-1)/3 {
				n.restrict(h)
			}
		}
	}
	return nackeds
}

// processTimestamps process new timestamps of a block which is proposed by
// validator vid, and returns the validators being nacked.
func (n *negativeAck) processTimestamps(
	vid types.ValidatorID,
	ts map[types.ValidatorID]time.Time,
) []types.ValidatorID {

	n.checkRestrictExpire()

	nackeds := []types.ValidatorID{}
	for h := range n.timeDiffs {
		if n.timeDiffs[vid][h][h].Equal(ts[h]) {
			votes := 0
			for hh := range n.timeDiffs {
				if ts[hh].Sub(n.timeDiffs[vid][h][hh]) >= n.timeDelay {
					votes++
				}
			}
			if votes > 2*((n.numOfValidators-1)/3) {
				n.lastVotes[h][vid] = struct{}{}
				nack := n.processNewVote(vid, h)
				for _, i := range nack {
					nackeds = append(nackeds, i)
				}
			} else {
				delete(n.lastVotes[h], vid)
			}
		} else {
			for hh := range n.timeDiffs {
				n.timeDiffs[vid][h][hh] = ts[hh]
			}
			delete(n.lastVotes[h], vid)
		}
	}
	return nackeds
}

func (n *negativeAck) checkRestrictExpire() {
	expired := []types.ValidatorID{}
	now := time.Now()
	for h, t := range n.restricteds {
		if now.Sub(t) >= n.timeExpire {
			expired = append(expired, h)
		}
	}
	for _, h := range expired {
		delete(n.restricteds, h)
	}
}

func (n *negativeAck) restrict(vid types.ValidatorID) {
	if _, exist := n.restricteds[vid]; !exist {
		n.restricteds[vid] = time.Now().UTC()
		n.lockedVotes[vid] = map[types.ValidatorID]struct{}{}
		for h := range n.lastVotes[vid] {
			n.lockedVotes[vid][h] = struct{}{}
		}
	}
}

func (n *negativeAck) getRestrictedValidators() map[types.ValidatorID]struct{} {
	n.checkRestrictExpire()
	ret := map[types.ValidatorID]struct{}{}
	for h := range n.restricteds {
		ret[h] = struct{}{}
	}
	return ret
}

func (n *negativeAck) setTimeDelay(t time.Duration) {
	n.timeDelay = t
}

func (n *negativeAck) setTimeExpire(t time.Duration) {
	n.timeExpire = t
}

func (n *negativeAck) addValidator(vid types.ValidatorID) {
	n.numOfValidators++
	n.lastVotes[vid] = make(map[types.ValidatorID]struct{})
	n.lockedVotes[vid] = make(map[types.ValidatorID]struct{})

	newTimeDiff := make(map[types.ValidatorID]map[types.ValidatorID]time.Time)
	for h := range n.timeDiffs {
		newTimeDiff2 := make(map[types.ValidatorID]time.Time)
		for hh := range n.timeDiffs {
			newTimeDiff2[hh] = time.Time{}
		}
		newTimeDiff[h] = newTimeDiff2
	}
	n.timeDiffs[vid] = newTimeDiff
	for h := range n.timeDiffs {
		n.timeDiffs[h][vid] = make(map[types.ValidatorID]time.Time)
	}
}

func (n *negativeAck) deleteValidator(vid types.ValidatorID) {
	n.numOfValidators--

	delete(n.timeDiffs, vid)

	for h := range n.lastVotes {
		delete(n.lastVotes[h], vid)
	}
	delete(n.lastVotes, vid)
	delete(n.lockedVotes, vid)

	for h := range n.timeDiffs {
		delete(n.timeDiffs[h], vid)
		for hh := range n.timeDiffs[h] {
			delete(n.timeDiffs[h][hh], vid)
		}
	}

	delete(n.restricteds, vid)
}
