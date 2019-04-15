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
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

type AgreementStateTestSuite struct {
	suite.Suite
	ID          types.NodeID
	signers     map[types.NodeID]*utils.Signer
	voteChan    chan *types.Vote
	blockChan   chan common.Hash
	confirmChan chan common.Hash
	block       map[common.Hash]*types.Block
}

type agreementStateTestReceiver struct {
	s      *AgreementStateTestSuite
	leader *leaderSelector
}

func (r *agreementStateTestReceiver) VerifyPartialSignature(*types.Vote) (bool, bool) {
	return true, false
}

func (r *agreementStateTestReceiver) ProposeVote(vote *types.Vote) {
	r.s.voteChan <- vote
}

func (r *agreementStateTestReceiver) ProposeBlock() common.Hash {
	block := r.s.proposeBlock(r.leader)
	r.s.blockChan <- block.Hash
	return block.Hash
}

func (r *agreementStateTestReceiver) ConfirmBlock(block common.Hash,
	_ map[types.NodeID]*types.Vote) {
	r.s.confirmChan <- block
}

func (r *agreementStateTestReceiver) PullBlocks(common.Hashes) {}

func (r *agreementStateTestReceiver) ReportForkVote(v1, v2 *types.Vote)   {}
func (r *agreementStateTestReceiver) ReportForkBlock(b1, b2 *types.Block) {}

func (s *AgreementStateTestSuite) proposeBlock(
	leader *leaderSelector) *types.Block {
	block := &types.Block{
		ProposerID: s.ID,
		Position:   types.Position{Height: types.GenesisHeight},
		Hash:       common.NewRandomHash(),
	}
	s.Require().NoError(s.signers[s.ID].SignCRS(block, leader.hashCRS))
	s.Require().NoError(s.signers[s.ID].SignBlock(block))
	s.block[block.Hash] = block
	return block
}

func (s *AgreementStateTestSuite) prepareVote(
	nID types.NodeID, voteType types.VoteType, blockHash common.Hash,
	period uint64) (
	vote *types.Vote) {
	vote = types.NewVote(voteType, blockHash, period)
	s.Require().NoError(s.signers[nID].SignVote(vote))
	return
}

func (s *AgreementStateTestSuite) SetupTest() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	s.ID = types.NewNodeID(prvKey.PublicKey())
	s.signers = map[types.NodeID]*utils.Signer{
		s.ID: utils.NewSigner(prvKey),
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
}

func (s *AgreementStateTestSuite) newAgreement(numNode int) *agreement {
	logger := &common.NullLogger{}
	leader := newLeaderSelector(func(*types.Block, common.Hash) (bool, error) {
		return true, nil
	}, logger)
	notarySet := make(map[types.NodeID]struct{})
	for i := 0; i < numNode-1; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		notarySet[nID] = struct{}{}
		s.signers[nID] = utils.NewSigner(prvKey)
	}
	notarySet[s.ID] = struct{}{}
	agreement := newAgreement(
		s.ID,
		&agreementStateTestReceiver{
			s:      s,
			leader: leader,
		},
		leader,
		s.signers[s.ID],
		logger,
	)
	agreement.restart(notarySet,
		utils.GetBAThreshold(&types.Config{
			NotarySetSize: uint32(len(notarySet)),
		}),
		types.Position{Height: types.GenesisHeight},
		types.NodeID{}, common.NewRandomHash())
	return agreement
}

func (s *AgreementStateTestSuite) TestFastStateLeader() {
	a := s.newAgreement(4)
	state := newFastState(a.data)
	s.Equal(stateFast, state.state())
	s.Equal(0, state.clocks())

	// Proposing a new block if it's leader.
	a.data.period = 1
	a.data.isLeader = true
	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.blockChan, 1)
	proposedBlock := <-s.blockChan
	s.NotEqual(common.Hash{}, proposedBlock)
	s.Require().NoError(a.processBlock(s.block[proposedBlock]))
	s.Require().Len(s.voteChan, 1)
	proposedVote := <-s.voteChan
	s.Equal(proposedBlock, proposedVote.BlockHash)
	s.Equal(types.VoteFast, proposedVote.Type)
	s.Equal(stateFastVote, newState.state())
}

func (s *AgreementStateTestSuite) TestFastStateNotLeader() {
	a := s.newAgreement(4)
	state := newFastState(a.data)
	s.Equal(stateFast, state.state())
	s.Equal(0, state.clocks())

	// Not proposing any block if it's not leader.
	a.data.period = 1
	a.data.isLeader = false
	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.blockChan, 0)
	s.Equal(stateFastVote, newState.state())
}

func (s *AgreementStateTestSuite) TestFastVoteState() {
	a := s.newAgreement(4)
	state := newFastVoteState(a.data)
	s.Equal(stateFastVote, state.state())
	s.Equal(3, state.clocks())

	// The vote proposed is not implemented inside state.
	a.data.period = 1
	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 0)
	s.Equal(stateInitial, newState.state())
}

func (s *AgreementStateTestSuite) TestInitialState() {
	a := s.newAgreement(4)
	state := newInitialState(a.data)
	s.Equal(stateInitial, state.state())
	s.Equal(0, state.clocks())

	// Proposing a new block.
	a.data.period = 1
	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.blockChan, 1)
	proposedBlock := <-s.blockChan
	s.NotEqual(common.Hash{}, proposedBlock)
	s.Require().NoError(a.processBlock(s.block[proposedBlock]))
	s.Equal(statePreCommit, newState.state())
}

func (s *AgreementStateTestSuite) TestPreCommitState() {
	a := s.newAgreement(4)
	state := newPreCommitState(a.data)
	s.Equal(statePreCommit, state.state())
	s.Equal(2, state.clocks())

	blocks := make([]*types.Block, 3)
	for i := range blocks {
		blocks[i] = s.proposeBlock(a.data.leader)
		prv, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		signer := utils.NewSigner(prv)
		blocks[i].ProposerID = types.NewNodeID(prv.PublicKey())
		s.Require().NoError(signer.SignCRS(
			blocks[i], a.data.leader.hashCRS))
		s.Require().NoError(signer.SignBlock(blocks[i]))
		s.Require().NoError(a.processBlock(blocks[i]))
	}

	// If lockvalue == null, propose preCom-vote for the leader block.
	a.data.lockValue = types.NullBlockHash
	a.data.period = 1
	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.NotEqual(types.SkipBlockHash, vote.BlockHash)
	s.Equal(stateCommit, newState.state())

	// If lockvalue == SKIP, propose preCom-vote for the leader block.
	a.data.lockValue = types.SkipBlockHash
	a.data.period = 2
	newState, err = state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.NotEqual(types.SkipBlockHash, vote.BlockHash)
	s.Equal(stateCommit, newState.state())

	// Else, preCom-vote on lockValue.
	a.data.period = 3
	hash := common.NewRandomHash()
	a.data.lockValue = hash
	newState, err = state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.Equal(hash, vote.BlockHash)
	s.Equal(stateCommit, newState.state())
}

func (s *AgreementStateTestSuite) TestCommitState() {
	a := s.newAgreement(4)
	state := newCommitState(a.data)
	s.Equal(stateCommit, state.state())
	s.Equal(2, state.clocks())

	// Commit on lock value.
	a.data.period = 1
	a.data.lockValue = common.NewRandomHash()
	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteCom, vote.Type)
	s.Equal(a.data.lockValue, vote.BlockHash)
	s.Equal(stateForward, newState.state())
}

func (s *AgreementStateTestSuite) TestForwardState() {
	a := s.newAgreement(4)
	state := newForwardState(a.data)
	s.Equal(stateForward, state.state())
	s.Equal(4, state.clocks())

	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 0)
	s.Equal(statePullVote, newState.state())
}

func (s *AgreementStateTestSuite) TestPullVoteState() {
	a := s.newAgreement(4)
	state := newPullVoteState(a.data)
	s.Equal(statePullVote, state.state())
	s.Equal(4, state.clocks())

	newState, err := state.nextState()
	s.Require().NoError(err)
	s.Require().Len(s.voteChan, 0)
	s.Equal(statePullVote, newState.state())
}

func TestAgreementState(t *testing.T) {
	suite.Run(t, new(AgreementStateTestSuite))
}
