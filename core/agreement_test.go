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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

// agreementTestReceiver implements core.agreementReceiveer
type agreementTestReceiver struct {
	s              *AgreementTestSuite
	agreementIndex int
}

func (r *agreementTestReceiver) ProposeVote(vote *types.Vote) {
	r.s.voteChan <- vote
}

func (r *agreementTestReceiver) ProposeBlock() common.Hash {
	block := r.s.proposeBlock(r.agreementIndex)
	r.s.blockChan <- block.Hash
	return block.Hash
}

func (r *agreementTestReceiver) ConfirmBlock(block common.Hash,
	_ map[types.NodeID]*types.Vote) {
	r.s.confirmChan <- block
}

func (r *agreementTestReceiver) PullBlocks(hashes common.Hashes) {
	for _, hash := range hashes {
		r.s.pulledBlocks[hash] = struct{}{}
	}

}

func (s *AgreementTestSuite) proposeBlock(
	agreementIdx int) *types.Block {
	block := &types.Block{
		ProposerID: s.ID,
		Hash:       common.NewRandomHash(),
	}
	s.block[block.Hash] = block
	s.Require().NoError(s.auths[s.ID].SignCRS(
		block, s.agreement[agreementIdx].data.leader.hashCRS))
	return block
}

type AgreementTestSuite struct {
	suite.Suite
	ID           types.NodeID
	auths        map[types.NodeID]*Authenticator
	voteChan     chan *types.Vote
	blockChan    chan common.Hash
	confirmChan  chan common.Hash
	block        map[common.Hash]*types.Block
	pulledBlocks map[common.Hash]struct{}
	agreement    []*agreement
}

func (s *AgreementTestSuite) SetupTest() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	s.ID = types.NewNodeID(prvKey.PublicKey())
	s.auths = map[types.NodeID]*Authenticator{
		s.ID: NewAuthenticator(prvKey),
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
	s.pulledBlocks = make(map[common.Hash]struct{})
}

func (s *AgreementTestSuite) newAgreement(numNotarySet int) *agreement {
	leader := newLeaderSelector(common.NewRandomHash(), func(*types.Block) bool {
		return true
	})
	agreementIdx := len(s.agreement)
	notarySet := make(map[types.NodeID]struct{})
	for i := 0; i < numNotarySet-1; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		notarySet[nID] = struct{}{}
		s.auths[nID] = NewAuthenticator(prvKey)
	}
	notarySet[s.ID] = struct{}{}
	agreement := newAgreement(
		s.ID,
		&agreementTestReceiver{
			s:              s,
			agreementIndex: agreementIdx,
		},
		notarySet,
		leader,
		s.auths[s.ID],
	)
	agreement.restart(notarySet, types.Position{})
	s.agreement = append(s.agreement, agreement)
	return agreement
}

func (s *AgreementTestSuite) copyVote(
	vote *types.Vote, proposer types.NodeID) *types.Vote {
	v := vote.Clone()
	s.auths[proposer].SignVote(v)
	return v
}

func (s *AgreementTestSuite) prepareVote(
	nID types.NodeID, voteType types.VoteType, blockHash common.Hash,
	period uint64) (
	vote *types.Vote) {
	vote = &types.Vote{
		Type:      voteType,
		BlockHash: blockHash,
		Period:    period,
	}
	s.Require().NoError(s.auths[nID].SignVote(vote))
	return
}

func (s *AgreementTestSuite) TestSimpleConfirm() {
	a := s.newAgreement(4)
	// InitialState
	a.nextState()
	// PreCommitState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().NoError(a.processBlock(block))
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteInit, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	a.nextState()
	// CommitState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	for nID := range s.auths {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	a.nextState()
	// ForwardState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Equal(blockHash, a.data.lockValue)
	s.Equal(uint64(1), a.data.lockRound)
	for nID := range s.auths {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Com-Votes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(blockHash, confirmBlock)
}

func (s *AgreementTestSuite) TestPartitionOnCommitVote() {
	a := s.newAgreement(4)
	// InitialState
	a.nextState()
	// PreCommitState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().NoError(a.processBlock(block))
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteInit, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	a.nextState()
	// CommitState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	for nID := range s.auths {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	a.nextState()
	// ForwardState
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Equal(blockHash, a.data.lockValue)
	s.Equal(uint64(1), a.data.lockRound)
	// RepeateVoteState
	a.nextState()
	s.True(a.pullVotes())
	s.Require().Len(s.voteChan, 0)
}

func (s *AgreementTestSuite) TestFastForwardCond1() {
	votes := 0
	a := s.newAgreement(4)
	a.data.lockRound = 1
	a.data.period = 3
	hash := common.NewRandomHash()
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, hash, uint64(2))
		s.Require().NoError(a.processVote(vote))
		if votes++; votes == 3 {
			break
		}
	}

	select {
	case <-a.done():
	default:
		s.FailNow("Expecting fast forward.")
	}
	s.Equal(hash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockRound)
	s.Equal(uint64(4), a.data.period)

	// No fast forward if vote.BlockHash == SKIP
	a.data.lockRound = 6
	a.data.period = 8
	a.data.lockValue = nullBlockHash
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, skipBlockHash, uint64(7))
		s.Require().NoError(a.processVote(vote))
	}

	select {
	case <-a.done():
		s.FailNow("Unexpected fast forward.")
	default:
	}

	// No fast forward if lockValue == vote.BlockHash.
	a.data.lockRound = 11
	a.data.period = 13
	a.data.lockValue = hash
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, hash, uint64(12))
		s.Require().NoError(a.processVote(vote))
	}

	select {
	case <-a.done():
		s.FailNow("Unexpected fast forward.")
	default:
	}
}

func (s *AgreementTestSuite) TestFastForwardCond2() {
	votes := 0
	a := s.newAgreement(4)
	a.data.period = 1
	hash := common.NewRandomHash()
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, hash, uint64(2))
		s.Require().NoError(a.processVote(vote))
		if votes++; votes == 3 {
			break
		}
	}

	select {
	case <-a.done():
	default:
		s.FailNow("Expecting fast forward.")
	}
	s.Equal(hash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockRound)
	s.Equal(uint64(2), a.data.period)

	// No fast forward if vote.BlockHash == SKIP
	a.data.period = 6
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, skipBlockHash, uint64(7))
		s.Require().NoError(a.processVote(vote))
	}

	select {
	case <-a.done():
		s.FailNow("Unexpected fast forward.")
	default:
	}
}

func (s *AgreementTestSuite) TestFastForwardCond3() {
	numVotes := 0
	votes := []*types.Vote{}
	a := s.newAgreement(4)
	a.data.period = 1
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteCom, common.NewRandomHash(), uint64(2))
		votes = append(votes, vote)
		s.Require().NoError(a.processVote(vote))
		if numVotes++; numVotes == 3 {
			break
		}
	}

	select {
	case <-a.done():
	default:
		s.FailNow("Expecting fast forward.")
	}
	s.Equal(uint64(3), a.data.period)

	s.Len(s.pulledBlocks, 3)
	for _, vote := range votes {
		_, exist := s.pulledBlocks[vote.BlockHash]
		s.True(exist)
	}
}

func (s *AgreementTestSuite) TestDecide() {
	votes := 0
	a := s.newAgreement(4)
	a.data.period = 5

	// No decide if com-vote on SKIP.
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteCom, skipBlockHash, uint64(2))
		s.Require().NoError(a.processVote(vote))
		if votes++; votes == 3 {
			break
		}
	}
	s.Require().Len(s.confirmChan, 0)

	// Normal decide.
	hash := common.NewRandomHash()
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteCom, hash, uint64(3))
		s.Require().NoError(a.processVote(vote))
		if votes++; votes == 3 {
			break
		}
	}
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(hash, confirmBlock)
}

func TestAgreement(t *testing.T) {
	suite.Run(t, new(AgreementTestSuite))
}
