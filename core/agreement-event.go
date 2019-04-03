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

package core

import (
	"encoding/hex"
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// agreementEventType is the type of agreemnt event.
type agreementEventType byte

// agreementEventType enum.
const (
	// agreementEventInvalid is just a type guard, not intend to be used.
	agreementEventInvalid agreementEventType = iota
	// agreementEventLock is triggered when receiving more than 2t+1 pre-commit
	// votes for one value.
	agreementEventLock
	// agreementEventForward is triggered when receiving more than 2t+1 commit
	// votes for different values.
	agreementEventForward
	// agreementEventDecide is triggered when receiving more than 2t+1 commit
	// votes for one value.
	agreementEventDecide
	// agreementEventFork is triggered when detecting a fork vote.
	agreementEventFork
	// Do not add any type below maxAgreementEventType.
	maxAgreementEventType
)

func (t agreementEventType) String() string {
	switch t {
	case agreementEventLock:
		return "Lock"
	case agreementEventForward:
		return "Forward"
	case agreementEventDecide:
		return "Decide"
	case agreementEventFork:
		return "Fork"
	default:
		return fmt.Sprintf("Unknown-%d", t)
	}
}

// agreementEvent represents an agreement event, ex. fast forward, decide,
// fork, ..., which would trigger state change of agreement module.
type agreementEvent struct {
	types.AgreementResult

	period   uint64
	evtType  agreementEventType
	voteType types.VoteType
}

func (e agreementEvent) toAgreementResult() types.AgreementResult {
	if e.evtType != agreementEventDecide {
		panic(fmt.Errorf("invalid event to extract agreement result: %s", e))
	}
	return e.AgreementResult
}

func (e agreementEvent) String() string {
	if len(e.Randomness) > 0 {
		return fmt.Sprintf(
			"agreementEvent{[%s:%s:%s:%d],Empty:%t,Hash:%s,Rand:%s}",
			e.evtType, e.voteType, e.Position, e.period, e.IsEmptyBlock,
			e.BlockHash.String()[:6], hex.EncodeToString(e.Randomness)[:6])
	}
	return fmt.Sprintf(
		"agreementEvent{[%s:%s:%s:%d],Empty:%t,Hash:%s}",
		e.evtType, e.voteType, e.Position, e.period, e.IsEmptyBlock,
		e.BlockHash.String()[:6])
}

// newAgreementEvent constructs an agreementEvent instance.
func newAgreementEvent(
	t agreementEventType, votes []types.Vote) *agreementEvent {
	return &agreementEvent{
		AgreementResult: types.AgreementResult{
			Position:     votes[0].Position,
			Votes:        votes,
			IsEmptyBlock: votes[0].BlockHash == types.NullBlockHash,
			BlockHash:    votes[0].BlockHash,
		},
		period:   votes[0].Period,
		evtType:  t,
		voteType: votes[0].Type,
	}
}

func newAgreementEventFromTSIG(
	position types.Position,
	hash common.Hash,
	tSig []byte,
	isEmpty bool) *agreementEvent {
	return &agreementEvent{
		AgreementResult: types.AgreementResult{
			Position:     position,
			BlockHash:    hash,
			IsEmptyBlock: isEmpty,
			Randomness:   tSig,
		},
		period:   0,
		evtType:  agreementEventDecide,
		voteType: types.MaxVoteType,
	}
}
