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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
)

type AgreementStateTestSuite struct {
	suite.Suite
	ID          types.NodeID
	prvKey      map[types.NodeID]crypto.PrivateKey
	voteChan    chan *types.Vote
	blockChan   chan common.Hash
	confirmChan chan common.Hash
	block       map[common.Hash]*types.Block
}

type agreementStateTestReceiver struct {
	s *AgreementStateTestSuite
}

func (r *agreementStateTestReceiver) ProposeVote(vote *types.Vote) {
	r.s.voteChan <- vote
}

func (r *agreementStateTestReceiver) ProposeBlock(block common.Hash) {
	r.s.blockChan <- block
}

func (r *agreementStateTestReceiver) ConfirmBlock(block common.Hash) {
	r.s.confirmChan <- block
}

func (s *AgreementStateTestSuite) proposeBlock(
	leader *leaderSelector) *types.Block {
	block := &types.Block{
		ProposerID: s.ID,
		Hash:       common.NewRandomHash(),
	}
	s.block[block.Hash] = block
	s.Require().Nil(leader.prepareBlock(block, s.prvKey[s.ID]))
	return block
}

func (s *AgreementStateTestSuite) prepareVote(
	nID types.NodeID, voteType types.VoteType, blockHash common.Hash,
	period uint64) (
	vote *types.Vote) {
	prvKey, exist := s.prvKey[nID]
	s.Require().True(exist)
	vote = &types.Vote{
		ProposerID: nID,
		Type:       voteType,
		BlockHash:  blockHash,
		Period:     period,
	}
	var err error
	vote.Signature, err = prvKey.Sign(hashVote(vote))
	s.Require().Nil(err)
	return
}

func (s *AgreementStateTestSuite) SetupTest() {
	prvKey, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	s.ID = types.NewNodeID(prvKey.PublicKey())
	s.prvKey = map[types.NodeID]crypto.PrivateKey{
		s.ID: prvKey,
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
}

func (s *AgreementStateTestSuite) newAgreement(numNode int) *agreement {
	leader := newGenesisLeaderSelector("I ❤️ DEXON", eth.SigToPub)
	blockProposer := func() *types.Block {
		return s.proposeBlock(leader)
	}

	notarySet := make(types.NodeIDs, numNode-1)
	for i := range notarySet {
		prvKey, err := eth.NewPrivateKey()
		s.Require().Nil(err)
		notarySet[i] = types.NewNodeID(prvKey.PublicKey())
		s.prvKey[notarySet[i]] = prvKey
	}
	notarySet = append(notarySet, s.ID)
	agreement := newAgreement(
		s.ID,
		&agreementStateTestReceiver{s},
		notarySet,
		leader,
		eth.SigToPub,
		blockProposer,
	)
	return agreement
}

func (s *AgreementStateTestSuite) TestPrepareState() {
	a := s.newAgreement(4)
	state := newPrepareState(a.data)
	s.Equal(statePrepare, state.state())
	s.Equal(0, state.clocks())

	// For period == 1, proposing a new block.
	a.data.period = 1
	newState, err := state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.blockChan) > 0)
	proposedBlock := <-s.blockChan
	s.NotEqual(common.Hash{}, proposedBlock)
	s.Require().Nil(a.processBlock(s.block[proposedBlock]))
	s.Equal(stateAck, newState.state())

	// For period >= 2, if the pass-vote for block b equal to {}
	// is more than 2f+1, proposing the block previously proposed.
	a.data.period = 2
	_, err = state.nextState()
	s.Equal(ErrNoEnoughVoteInPrepareState, err)

	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, common.Hash{}, 1)
		s.Require().Nil(a.processVote(vote))
	}

	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Equal(stateAck, newState.state())

	// For period >= 2, if the pass-vote for block v not equal to {}
	// is more than 2f+1, proposing the block v.
	a.data.period = 3
	block := s.proposeBlock(a.data.leader)
	prv, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	block.ProposerID = types.NewNodeID(prv.PublicKey())
	s.Require().Nil(a.data.leader.prepareBlock(block, prv))
	s.Require().Nil(a.processBlock(block))
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, block.Hash, 2)
		s.Require().Nil(a.processVote(vote))
	}

	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Equal(stateAck, newState.state())
}

func (s *AgreementStateTestSuite) TestAckState() {
	a := s.newAgreement(4)
	state := newAckState(a.data)
	s.Equal(stateAck, state.state())
	s.Equal(2, state.clocks())

	blocks := make([]*types.Block, 3)
	for i := range blocks {
		blocks[i] = s.proposeBlock(a.data.leader)
		prv, err := eth.NewPrivateKey()
		s.Require().Nil(err)
		blocks[i].ProposerID = types.NewNodeID(prv.PublicKey())
		s.Require().Nil(a.data.leader.prepareBlock(blocks[i], prv))
		s.Require().Nil(a.processBlock(blocks[i]))
	}

	// For period 1, propose ack-vote for the block having largest potential.
	a.data.period = 1
	newState, err := state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote := <-s.voteChan
	s.Equal(types.VoteAck, vote.Type)
	s.NotEqual(common.Hash{}, vote.BlockHash)
	s.Equal(stateConfirm, newState.state())

	// For period >= 2, if block v equal to {} has more than 2f+1 pass-vote
	// in period 1, propose ack-vote for the block having largest potential.
	a.data.period = 2
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, common.Hash{}, 1)
		s.Require().Nil(a.processVote(vote))
	}
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VoteAck, vote.Type)
	s.NotEqual(common.Hash{}, vote.BlockHash)
	s.Equal(stateConfirm, newState.state())

	// For period >= 2, if block v not equal to {} has more than 2f+1 pass-vote
	// in period 1, propose ack-vote for block v.
	hash := blocks[0].Hash
	a.data.period = 3
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, hash, 2)
		s.Require().Nil(a.processVote(vote))
	}
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VoteAck, vote.Type)
	s.Equal(hash, vote.BlockHash)
	s.Equal(stateConfirm, newState.state())
}

func (s *AgreementStateTestSuite) TestConfirmState() {
	a := s.newAgreement(4)
	state := newConfirmState(a.data)
	s.Equal(stateConfirm, state.state())
	s.Equal(2, state.clocks())

	// If there are 2f+1 ack-votes for block v not equal to {},
	// propose a confirm-vote for block v.
	a.data.period = 1
	block := s.proposeBlock(a.data.leader)
	s.Require().Nil(a.processBlock(block))
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteAck, block.Hash, 1)
		s.Require().Nil(a.processVote(vote))
	}
	s.Require().Nil(state.receiveVote())
	newState, err := state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote := <-s.voteChan
	s.Equal(types.VoteConfirm, vote.Type)
	s.Equal(block.Hash, vote.BlockHash)
	s.Equal(statePass1, newState.state())

	// Else, no vote is propose in this state.
	a.data.period = 2
	s.Require().Nil(state.receiveVote())
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) == 0)
	s.Equal(statePass1, newState.state())

	// If there are 2f+1 ack-vote for block v equal to {},
	// no vote should be proposed.
	a.data.period = 3
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteAck, common.Hash{}, 3)
		s.Require().Nil(a.processVote(vote))
	}
	s.Require().Nil(state.receiveVote())
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) == 0)
	s.Equal(statePass1, newState.state())
}

func (s *AgreementStateTestSuite) TestPass1State() {
	a := s.newAgreement(4)
	state := newPass1State(a.data)
	s.Equal(statePass1, state.state())
	s.Equal(0, state.clocks())

	// If confirm-vote was proposed in the same period,
	// propose pass-vote with same block.
	a.data.period = 1
	hash := common.NewRandomHash()
	vote := s.prepareVote(s.ID, types.VoteConfirm, hash, 1)
	s.Require().Nil(a.processVote(vote))
	newState, err := state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VotePass, vote.Type)
	s.Equal(hash, vote.BlockHash)
	s.Equal(statePass2, newState.state())

	// Else if period >= 2 and has 2f+1 pass-vote in period-1 for block {},
	// propose pass-vote for block {}.
	a.data.period = 2
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, common.Hash{}, 1)
		s.Require().Nil(a.processVote(vote))
	}
	vote = s.prepareVote(s.ID, types.VoteAck, common.Hash{}, 2)
	s.Require().Nil(a.processVote(vote))
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VotePass, vote.Type)
	s.Equal(common.Hash{}, vote.BlockHash)
	s.Equal(statePass2, newState.state())

	// Else, propose pass-vote for default block.
	a.data.period = 3
	block := s.proposeBlock(a.data.leader)
	a.data.defaultBlock = block.Hash
	hash = common.NewRandomHash()
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, hash, 2)
		s.Require().Nil(a.processVote(vote))
	}
	vote = s.prepareVote(s.ID, types.VoteAck, common.Hash{}, 3)
	s.Require().Nil(a.processVote(vote))
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VotePass, vote.Type)
	s.Equal(block.Hash, vote.BlockHash)
	s.Equal(statePass2, newState.state())

	// Period == 1 is also else condition.
	a = s.newAgreement(4)
	state = newPass1State(a.data)
	a.data.period = 1
	block = s.proposeBlock(a.data.leader)
	a.data.defaultBlock = block.Hash
	hash = common.NewRandomHash()
	vote = s.prepareVote(s.ID, types.VoteAck, common.Hash{}, 1)
	s.Require().Nil(a.processVote(vote))
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VotePass, vote.Type)
	s.Equal(block.Hash, vote.BlockHash)
	s.Equal(statePass2, newState.state())

	// No enought pass-vote for period-1.
	a.data.period = 4
	vote = s.prepareVote(s.ID, types.VoteAck, common.Hash{}, 4)
	s.Require().Nil(a.processVote(vote))
	newState, err = state.nextState()
	s.Require().Nil(err)
	s.Require().True(len(s.voteChan) > 0)
	vote = <-s.voteChan
	s.Equal(types.VotePass, vote.Type)
	s.Equal(block.Hash, vote.BlockHash)
	s.Equal(statePass2, newState.state())
}

func (s *AgreementStateTestSuite) TestPass2State() {
	a := s.newAgreement(4)
	state := newPass2State(a.data)
	s.Equal(statePass2, state.state())

	// If there are 2f+1 ack-vote for block v not equal to {},
	// propose pass-vote for v.
	block := s.proposeBlock(a.data.leader)
	s.Require().Nil(a.processBlock(block))
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteAck, block.Hash, 1)
		s.Require().Nil(a.processVote(vote))
	}
	s.Require().Nil(state.receiveVote())
	// Only propose one vote.
	s.Require().Nil(state.receiveVote())
	s.Require().True(len(s.voteChan) == 0)

	// If period >= 2 and
	// there are 2f+1 pass-vote in period-1 for block v equal to {} and
	// no confirm-vote is proposed, propose pass-vote for {}.
	a = s.newAgreement(4)
	state = newPass2State(a.data)
	a.data.period = 2
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, common.Hash{}, 1)
		s.Require().Nil(a.processVote(vote))
	}
	vote := s.prepareVote(s.ID, types.VoteAck, common.Hash{}, 2)
	s.Require().Nil(a.processVote(vote))
	s.Require().Nil(state.receiveVote())
	// Test terminate.
	ok := make(chan struct{})
	go func() {
		go state.terminate()
		newState, err := state.nextState()
		s.Require().Nil(err)
		s.Equal(statePrepare, newState.state())
		ok <- struct{}{}
	}()
	select {
	case <-ok:
	case <-time.After(50 * time.Millisecond):
		s.FailNow("Terminate fail.\n")
	}

	// If there are 2f+1 pass-vote, proceed to next period
	a = s.newAgreement(4)
	state = newPass2State(a.data)
	a.data.period = 1
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePass, common.Hash{}, 1)
		s.Require().Nil(a.processVote(vote))
	}
	s.Require().Nil(state.receiveVote())
	go func() {
		newState, err := state.nextState()
		s.Require().Nil(err)
		s.Equal(statePrepare, newState.state())
		s.Equal(uint64(2), a.data.period)
		ok <- struct{}{}
	}()
	select {
	case <-ok:
	case <-time.After(50 * time.Millisecond):
		s.FailNow("Unable to proceed to next state.\n")
	}
}

func TestAgreementState(t *testing.T) {
	suite.Run(t, new(AgreementStateTestSuite))
}
