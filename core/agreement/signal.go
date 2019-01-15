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

package agreement

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/ethereum/go-ethereum/common"
)

// SignalType is the type of agreemnt signal.
type SignalType byte

// SignalType enum.
const (
	// SignalInvalid is just a type guard, not intend to be used.
	SignalInvalid SignalType = iota
	// SignalLock is triggered when receiving more than 2t+1 pre-commit votes
	// for one value.
	SignalLock
	// SignalForward is triggered when receiving more than 2t+1 commit votes for
	// different values.
	SignalForward
	// SignalDecide is triggered when receiving more than 2t+1 commit
	// votes for one value.
	SignalDecide
	// SignalFork is triggered when detecting a fork vote.
	SignalFork
	// Do not add any type below MaxSignalType.
	maxSignalType
)

func (t SignalType) String() string {
	switch t {
	case SignalLock:
		return "Lock"
	case SignalForward:
		return "Forward"
	case SignalDecide:
		return "Decide"
	case SignalFork:
		return "Fork"
	}
	panic(fmt.Errorf("attempting to dump unknown agreement signal type: %d", t))
}

// Signal represents an agreement signal, ex. fast forward, decide,
// fork, ..., which would trigger state change of agreement module.
type Signal struct {
	Position  types.Position
	Period    uint64
	Votes     []types.Vote
	Type      SignalType
	VType     types.VoteType
	BlockHash common.Hash
}

func (s *Signal) String() string {
	return fmt.Sprintf(
		"AgreementSignal{Type:%s,vType:%s,Pos:%s,Period:%d,Hash:%s",
		s.Type, s.VType, &s.Position, s.Period, s.BlockHash.String()[:6])
}

// NewSignal constructs an Signal instance.
func NewSignal(t SignalType, votes []types.Vote) *Signal {
	return &Signal{
		Position: votes[0].Position,
		Period:   votes[0].Period,
		Type:     t,
		VType:    votes[0].Type,
		Votes:    votes,
	}
}
