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

package core

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// Errors for agreement state module.
var (
	ErrNoEnoughVoteInPrepareState = fmt.Errorf("no enough vote in prepare state")
	ErrNoEnoughVoteInAckState     = fmt.Errorf("no enough vote in ack state")
)

// agreementStateType is the state of agreement
type agreementStateType int

// agreementStateType enum.
const (
	stateFast agreementStateType = iota
	stateFastVote
	stateFastRollback
	stateInitial
	statePreCommit
	stateCommit
	stateForward
	statePullVote
	stateSleep
)

var nullBlockHash common.Hash
var skipBlockHash common.Hash

func init() {
	for idx := range skipBlockHash {
		skipBlockHash[idx] = 0xff
	}
}

type agreementState interface {
	state() agreementStateType
	nextState() (agreementState, error)
	clocks() int
}

//----- FastState -----
type fastState struct {
	a *agreementData
}

func newFastState(a *agreementData) *fastState {
	return &fastState{a: a}
}

func (s *fastState) state() agreementStateType { return stateFast }
func (s *fastState) clocks() int               { return 0 }
func (s *fastState) nextState() (agreementState, error) {
	if func() bool {
		s.a.lock.Lock()
		defer s.a.lock.Unlock()
		return s.a.isLeader
	}() {
		hash := s.a.recv.ProposeBlock()
		if hash != nullBlockHash {
			s.a.lock.Lock()
			defer s.a.lock.Unlock()
			s.a.recv.ProposeVote(types.NewVote(types.VoteFast, hash, s.a.period))
		}
	}
	return newFastVoteState(s.a), nil
}

//----- FastVoteState -----
type fastVoteState struct {
	a *agreementData
}

func newFastVoteState(a *agreementData) *fastVoteState {
	return &fastVoteState{a: a}
}

func (s *fastVoteState) state() agreementStateType { return stateFastVote }
func (s *fastVoteState) clocks() int               { return 2 }
func (s *fastVoteState) nextState() (agreementState, error) {
	return newFastRollbackState(s.a), nil
}

//----- FastRollbackState -----
type fastRollbackState struct {
	a *agreementData
}

func newFastRollbackState(a *agreementData) *fastRollbackState {
	return &fastRollbackState{a: a}
}

func (s *fastRollbackState) state() agreementStateType { return stateFastRollback }
func (s *fastRollbackState) clocks() int               { return 1 }
func (s *fastRollbackState) nextState() (agreementState, error) {
	return newInitialState(s.a), nil
}

//----- InitialState -----
type initialState struct {
	a *agreementData
}

func newInitialState(a *agreementData) *initialState {
	return &initialState{a: a}
}

func (s *initialState) state() agreementStateType { return stateInitial }
func (s *initialState) clocks() int               { return 0 }
func (s *initialState) nextState() (agreementState, error) {
	if func() bool {
		s.a.lock.Lock()
		defer s.a.lock.Unlock()
		return !s.a.isLeader
	}() {
		// Leader already proposed block in fastState.
		hash := s.a.recv.ProposeBlock()
		s.a.lock.Lock()
		defer s.a.lock.Unlock()
		s.a.recv.ProposeVote(types.NewVote(types.VoteInit, hash, s.a.period))
	}
	return newPreCommitState(s.a), nil
}

//----- PreCommitState -----
type preCommitState struct {
	a *agreementData
}

func newPreCommitState(a *agreementData) *preCommitState {
	return &preCommitState{a: a}
}

func (s *preCommitState) state() agreementStateType { return statePreCommit }
func (s *preCommitState) clocks() int               { return 2 }
func (s *preCommitState) nextState() (agreementState, error) {
	s.a.lock.RLock()
	defer s.a.lock.RUnlock()
	hash := s.a.lockValue
	if hash == nullBlockHash {
		hash = s.a.leader.leaderBlockHash()
	}
	s.a.recv.ProposeVote(types.NewVote(types.VotePreCom, hash, s.a.period))
	return newCommitState(s.a), nil
}

//----- CommitState -----
type commitState struct {
	a *agreementData
}

func newCommitState(a *agreementData) *commitState {
	return &commitState{a: a}
}

func (s *commitState) state() agreementStateType { return stateCommit }
func (s *commitState) clocks() int               { return 2 }
func (s *commitState) nextState() (agreementState, error) {
	s.a.lock.Lock()
	defer s.a.lock.Unlock()
	hash, ok := s.a.countVoteNoLock(s.a.period, types.VotePreCom)
	if ok && hash != skipBlockHash {
		s.a.lockValue = hash
		s.a.lockRound = s.a.period
	} else {
		hash = skipBlockHash
	}
	s.a.recv.ProposeVote(types.NewVote(types.VoteCom, hash, s.a.period))
	return newForwardState(s.a), nil
}

// ----- ForwardState -----
type forwardState struct {
	a *agreementData
}

func newForwardState(a *agreementData) *forwardState {
	return &forwardState{a: a}
}

func (s *forwardState) state() agreementStateType { return stateForward }
func (s *forwardState) clocks() int               { return 4 }

func (s *forwardState) nextState() (agreementState, error) {
	return newPullVoteState(s.a), nil
}

// ----- PullVoteState -----
// pullVoteState is a special state to ensure the assumption in the consensus
// algorithm that every vote will eventually arrive for all nodes.
type pullVoteState struct {
	a *agreementData
}

func newPullVoteState(a *agreementData) *pullVoteState {
	return &pullVoteState{a: a}
}

func (s *pullVoteState) state() agreementStateType { return statePullVote }
func (s *pullVoteState) clocks() int               { return 4 }

func (s *pullVoteState) nextState() (agreementState, error) {
	return s, nil
}

// ----- SleepState -----
// sleepState is a special state after BA has output and waits for restart.
type sleepState struct {
	a *agreementData
}

func newSleepState(a *agreementData) *sleepState {
	return &sleepState{a: a}
}

func (s *sleepState) state() agreementStateType { return stateSleep }
func (s *sleepState) clocks() int               { return 65536 }

func (s *sleepState) nextState() (agreementState, error) {
	return s, nil
}
