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

package core

import (
	"fmt"
	"sync/atomic"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
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
	statePrepare agreementStateType = iota
	stateAck
	stateConfirm
	statePass1
	statePass2
)

var nullBlockHash = common.Hash{}

type agreementState interface {
	state() agreementStateType
	nextState() (agreementState, error)
	receiveVote() error
	clocks() int
	terminate()
}

//----- PrepareState -----
type prepareState struct {
	a *agreementData
}

func newPrepareState(a *agreementData) *prepareState {
	return &prepareState{a: a}
}

func (s *prepareState) state() agreementStateType { return statePrepare }
func (s *prepareState) clocks() int               { return 0 }
func (s *prepareState) terminate()                {}
func (s *prepareState) nextState() (agreementState, error) {
	hash := common.Hash{}
	if s.a.period == 1 {
		hash = s.a.blockProposer().Hash
	} else {
		var proposed bool
		hash, proposed = s.a.countVote(s.a.period-1, types.VotePass)
		if !proposed {
			return nil, ErrNoEnoughVoteInPrepareState
		}
		if hash == nullBlockHash {
			hash = s.a.blocks[s.a.ID].Hash
		} else {
			delete(s.a.blocks, s.a.ID)
		}
	}
	s.a.blockChan <- hash
	return newAckState(s.a), nil
}
func (s *prepareState) receiveVote() error { return nil }

// ----- AckState -----
type ackState struct {
	a *agreementData
}

func newAckState(a *agreementData) *ackState {
	return &ackState{a: a}
}

func (s *ackState) state() agreementStateType { return stateAck }
func (s *ackState) clocks() int               { return 2 }
func (s *ackState) terminate()                {}
func (s *ackState) nextState() (agreementState, error) {
	acked := false
	hash := common.Hash{}
	if s.a.period == 1 {
		acked = true
	} else {
		hash, acked = s.a.countVote(s.a.period-1, types.VotePass)
	}
	if !acked {
		return nil, ErrNoEnoughVoteInAckState
	}
	if hash == nullBlockHash {
		hash = s.a.leader.leaderBlockHash()
	}
	s.a.voteChan <- &types.Vote{
		Type:      types.VoteAck,
		BlockHash: hash,
		Period:    s.a.period,
	}
	return newConfirmState(s.a), nil
}
func (s *ackState) receiveVote() error { return nil }

// ----- ConfirmState -----
type confirmState struct {
	a     *agreementData
	voted *atomic.Value
}

func newConfirmState(a *agreementData) *confirmState {
	voted := &atomic.Value{}
	voted.Store(false)
	return &confirmState{
		a:     a,
		voted: voted,
	}
}

func (s *confirmState) state() agreementStateType { return stateConfirm }
func (s *confirmState) clocks() int               { return 2 }
func (s *confirmState) terminate()                {}
func (s *confirmState) nextState() (agreementState, error) {
	return newPass1State(s.a), nil
}
func (s *confirmState) receiveVote() error {
	if s.voted.Load().(bool) {
		return nil
	}
	hash, ok := s.a.countVote(s.a.period, types.VoteAck)
	if !ok {
		return nil
	}
	if hash != nullBlockHash {
		s.a.voteChan <- &types.Vote{
			Type:      types.VoteConfirm,
			BlockHash: hash,
			Period:    s.a.period,
		}
	}
	s.voted.Store(true)
	return nil
}

// ----- Pass1State -----
type pass1State struct {
	a *agreementData
}

func newPass1State(a *agreementData) *pass1State {
	return &pass1State{a: a}
}

func (s *pass1State) state() agreementStateType { return statePass1 }
func (s *pass1State) clocks() int               { return 0 }
func (s *pass1State) terminate()                {}
func (s *pass1State) nextState() (agreementState, error) {
	voteDefault := false
	s.a.votesLock.RLock()
	defer s.a.votesLock.RUnlock()
	if vote, exist :=
		s.a.votes[s.a.period][types.VoteConfirm][s.a.ID]; exist {
		s.a.voteChan <- &types.Vote{
			Type:      types.VotePass,
			BlockHash: vote.BlockHash,
			Period:    s.a.period,
		}
	} else if s.a.period == 1 {
		voteDefault = true
	} else {
		hash, ok := s.a.countVote(s.a.period-1, types.VotePass)
		if ok {
			if hash == nullBlockHash {
				s.a.voteChan <- &types.Vote{
					Type:      types.VotePass,
					BlockHash: hash,
					Period:    s.a.period,
				}
			} else {
				voteDefault = true
			}
		} else {
			voteDefault = true
		}
	}
	if voteDefault {
		s.a.voteChan <- &types.Vote{
			Type:      types.VotePass,
			BlockHash: s.a.defaultBlock,
			Period:    s.a.period,
		}
	}
	return newPass2State(s.a), nil
}
func (s *pass1State) receiveVote() error { return nil }

// ----- Pass2State -----
type pass2State struct {
	a              *agreementData
	voted          *atomic.Value
	enoughPassVote chan common.Hash
	terminateChan  chan struct{}
}

func newPass2State(a *agreementData) *pass2State {
	voted := &atomic.Value{}
	voted.Store(false)
	return &pass2State{
		a:              a,
		voted:          voted,
		enoughPassVote: make(chan common.Hash),
		terminateChan:  make(chan struct{}),
	}
}

func (s *pass2State) state() agreementStateType { return statePass2 }
func (s *pass2State) clocks() int               { return 0 }
func (s *pass2State) terminate() {
	s.terminateChan <- struct{}{}
}
func (s *pass2State) nextState() (agreementState, error) {
	select {
	case <-s.terminateChan:
		break
	case hash := <-s.enoughPassVote:
		s.a.votesLock.RLock()
		defer s.a.votesLock.RUnlock()
		s.a.defaultBlock = hash
		s.a.period++
		oldBlock := s.a.blocks[s.a.ID]
		s.a.blocks = map[types.ValidatorID]*types.Block{
			s.a.ID: oldBlock,
		}
	}
	return newPrepareState(s.a), nil
}
func (s *pass2State) receiveVote() error {
	if s.voted.Load().(bool) {
		return nil
	}
	ackHash, ok := s.a.countVote(s.a.period, types.VoteAck)
	if ok && ackHash != nullBlockHash {
		s.a.voteChan <- &types.Vote{
			Type:      types.VotePass,
			BlockHash: ackHash,
			Period:    s.a.period,
		}
	} else if s.a.period > 1 {
		if _, exist :=
			s.a.votes[s.a.period][types.VoteConfirm][s.a.ID]; !exist {
			hash, ok := s.a.countVote(s.a.period-1, types.VotePass)
			if ok && hash == nullBlockHash {
				s.a.voteChan <- &types.Vote{
					Type:      types.VotePass,
					BlockHash: hash,
					Period:    s.a.period,
				}
			}
		}
	}
	go func() {
		hash, ok := s.a.countVote(s.a.period, types.VotePass)
		if ok {
			s.enoughPassVote <- hash
		}
	}()
	s.voted.Store(true)
	return nil
}
