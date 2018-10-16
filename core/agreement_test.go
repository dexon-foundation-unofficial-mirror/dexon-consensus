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

func (r *agreementTestReceiver) ProposeBlock() {
	block := r.s.proposeBlock(r.agreementIndex)
	r.s.blockChan <- block.Hash
}

func (r *agreementTestReceiver) ConfirmBlock(block common.Hash,
	_ map[types.NodeID]*types.Vote) {
	r.s.confirmChan <- block
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
	ID          types.NodeID
	auths       map[types.NodeID]*Authenticator
	voteChan    chan *types.Vote
	blockChan   chan common.Hash
	confirmChan chan common.Hash
	block       map[common.Hash]*types.Block
	agreement   []*agreement
}

func (s *AgreementTestSuite) SetupTest() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().Nil(err)
	s.ID = types.NewNodeID(prvKey.PublicKey())
	s.auths = map[types.NodeID]*Authenticator{
		s.ID: NewAuthenticator(prvKey),
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
}

func (s *AgreementTestSuite) newAgreement(numNotarySet int) *agreement {
	leader := newLeaderSelector(common.NewRandomHash())
	agreementIdx := len(s.agreement)
	notarySet := make(map[types.NodeID]struct{})
	for i := 0; i < numNotarySet-1; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().Nil(err)
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

func (s *AgreementTestSuite) TestSimpleConfirm() {
	a := s.newAgreement(4)
	// PrepareState
	a.nextState()
	// AckState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().NoError(a.processBlock(block))
	a.nextState()
	// ConfirmState
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteAck, vote.Type)
	for nID := range s.auths {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	a.nextState()
	// Pass1State
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteConfirm, vote.Type)
	for nID := range s.auths {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Confirm-Votes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(blockHash, confirmBlock)
}

func TestAgreement(t *testing.T) {
	suite.Run(t, new(AgreementTestSuite))
}
