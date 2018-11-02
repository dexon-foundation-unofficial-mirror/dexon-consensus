// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package core

import (
	"time"

	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type negativeAck struct {
	// owner is the ID of proposer itself, this is used when deciding
	// a node to be restricted or not.
	owner types.NodeID

	numOfNodes int

	// timeDelay and timeExpire are for nack timeout.
	timeDelay  time.Duration
	timeExpire time.Duration

	// restricteds stores nodes which has been restricted and the time it's
	// restricted.
	restricteds map[types.NodeID]time.Time

	// lastVotes and lockedVotes store the votes for nack. lastVotes[nid1][nid2]
	// and lockedVotes[nid1][nid2] both mean that nid2 votes nid1. The difference
	// is lockedVotes works only when nid1 is restricted, so that the votes are
	// needed to be locked.
	lastVotes   map[types.NodeID]map[types.NodeID]struct{}
	lockedVotes map[types.NodeID]map[types.NodeID]struct{}

	// timeDiffs is the cache for last time stamps. timeDiffs[nid1][nid2] means
	// the last updated timestamps nid1 sees nid2.
	timeDiffs map[types.NodeID]map[types.NodeID]map[types.NodeID]time.Time
}

// newNegativeAck creates a new negaticeAck instance.
func newNegativeAck(nid types.NodeID) *negativeAck {
	n := &negativeAck{
		owner:       nid,
		numOfNodes:  0,
		restricteds: make(map[types.NodeID]time.Time),
		lastVotes:   make(map[types.NodeID]map[types.NodeID]struct{}),
		lockedVotes: make(map[types.NodeID]map[types.NodeID]struct{}),
		timeDiffs:   make(map[types.NodeID]map[types.NodeID]map[types.NodeID]time.Time),
	}
	n.addNode(nid)
	return n
}

// processNewVote is called when a new "vote" occurs, that is, a node
// sees that other 2f + 1 nodes think a node is slow. "nid" is the
// node which propesed the block which the timestamps votes and "h" is
// the node been voted to be nacked.
func (n *negativeAck) processNewVote(
	nid types.NodeID,
	h types.NodeID,
) []types.NodeID {

	nackeds := []types.NodeID{}
	if _, exist := n.restricteds[h]; exist {
		n.lockedVotes[h][nid] = struct{}{}
		if len(n.lockedVotes[h]) > 2*(n.numOfNodes-1)/3 {
			nackeds = append(nackeds, h)
			delete(n.restricteds, h)
		}
	} else {
		if n.owner == nid {
			n.restrict(h)
		} else {
			n.lastVotes[h][nid] = struct{}{}
			if len(n.lastVotes[h]) > (n.numOfNodes-1)/3 {
				n.restrict(h)
			}
		}
	}
	return nackeds
}

// processTimestamps process new timestamps of a block which is proposed by
// node nid, and returns the nodes being nacked.
func (n *negativeAck) processTimestamps(
	nid types.NodeID,
	ts map[types.NodeID]time.Time,
) []types.NodeID {

	n.checkRestrictExpire()

	nackeds := []types.NodeID{}
	for h := range n.timeDiffs {
		if n.timeDiffs[nid][h][h].Equal(ts[h]) {
			votes := 0
			for hh := range n.timeDiffs {
				if ts[hh].Sub(n.timeDiffs[nid][h][hh]) >= n.timeDelay {
					votes++
				}
			}
			if votes > 2*((n.numOfNodes-1)/3) {
				n.lastVotes[h][nid] = struct{}{}
				nack := n.processNewVote(nid, h)
				for _, i := range nack {
					nackeds = append(nackeds, i)
				}
			} else {
				delete(n.lastVotes[h], nid)
			}
		} else {
			for hh := range n.timeDiffs {
				n.timeDiffs[nid][h][hh] = ts[hh]
			}
			delete(n.lastVotes[h], nid)
		}
	}
	return nackeds
}

func (n *negativeAck) checkRestrictExpire() {
	expired := []types.NodeID{}
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

func (n *negativeAck) restrict(nid types.NodeID) {
	if _, exist := n.restricteds[nid]; !exist {
		n.restricteds[nid] = time.Now().UTC()
		n.lockedVotes[nid] = map[types.NodeID]struct{}{}
		for h := range n.lastVotes[nid] {
			n.lockedVotes[nid][h] = struct{}{}
		}
	}
}

func (n *negativeAck) getRestrictedNodes() map[types.NodeID]struct{} {
	n.checkRestrictExpire()
	ret := map[types.NodeID]struct{}{}
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

func (n *negativeAck) addNode(nid types.NodeID) {
	n.numOfNodes++
	n.lastVotes[nid] = make(map[types.NodeID]struct{})
	n.lockedVotes[nid] = make(map[types.NodeID]struct{})

	newTimeDiff := make(map[types.NodeID]map[types.NodeID]time.Time)
	for h := range n.timeDiffs {
		newTimeDiff2 := make(map[types.NodeID]time.Time)
		for hh := range n.timeDiffs {
			newTimeDiff2[hh] = time.Time{}
		}
		newTimeDiff[h] = newTimeDiff2
	}
	n.timeDiffs[nid] = newTimeDiff
	for h := range n.timeDiffs {
		n.timeDiffs[h][nid] = make(map[types.NodeID]time.Time)
	}
}

func (n *negativeAck) deleteNode(nid types.NodeID) {
	n.numOfNodes--

	delete(n.timeDiffs, nid)

	for h := range n.lastVotes {
		delete(n.lastVotes[h], nid)
	}
	delete(n.lastVotes, nid)
	delete(n.lockedVotes, nid)

	for h := range n.timeDiffs {
		delete(n.timeDiffs[h], nid)
		for hh := range n.timeDiffs[h] {
			delete(n.timeDiffs[h][hh], nid)
		}
	}

	delete(n.restricteds, nid)
}
