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
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

// agreementTestReceiver implements core.agreementReceiveer
type agreementTestReceiver struct {
	s *AgreementTestSuite
}

func (r *agreementTestReceiver) ProposeVote(vote *types.Vote) {
	r.s.voteChan <- vote
}

func (r *agreementTestReceiver) ProposeBlock(block common.Hash) {
	r.s.blockChan <- block
}

func (r *agreementTestReceiver) ConfirmBlock(block common.Hash) {
	r.s.confirmChan <- block
}

func (s *AgreementTestSuite) proposeBlock(
	agreementIdx int) *types.Block {
	block := &types.Block{
		ProposerID: s.ID,
		Hash:       common.NewRandomHash(),
	}
	s.block[block.Hash] = block
	s.Require().Nil(s.agreement[agreementIdx].prepareBlock(block, s.prvKey[s.ID]))
	return block
}

type AgreementTestSuite struct {
	suite.Suite
	ID          types.ValidatorID
	prvKey      map[types.ValidatorID]crypto.PrivateKey
	voteChan    chan *types.Vote
	blockChan   chan common.Hash
	confirmChan chan common.Hash
	block       map[common.Hash]*types.Block
	agreement   []*agreement
}

func (s *AgreementTestSuite) SetupTest() {
	prvKey, err := eth.NewPrivateKey()
	s.Require().Nil(err)
	s.ID = types.NewValidatorID(prvKey.PublicKey())
	s.prvKey = map[types.ValidatorID]crypto.PrivateKey{
		s.ID: prvKey,
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
}

func (s *AgreementTestSuite) newAgreement(numValidator int) *agreement {
	leader := newGenesisLeaderSelector("🖖👽", eth.SigToPub)
	agreementIdx := len(s.agreement)
	blockProposer := func() *types.Block {
		return s.proposeBlock(agreementIdx)
	}

	validators := make(types.ValidatorIDs, numValidator-1)
	for i := range validators {
		prvKey, err := eth.NewPrivateKey()
		s.Require().Nil(err)
		validators[i] = types.NewValidatorID(prvKey.PublicKey())
		s.prvKey[validators[i]] = prvKey
	}
	validators = append(validators, s.ID)
	agreement := newAgreement(
		s.ID,
		&agreementTestReceiver{s},
		validators,
		leader,
		eth.SigToPub,
		blockProposer,
	)
	s.agreement = append(s.agreement, agreement)
	return agreement
}

func (s *AgreementTestSuite) prepareVote(vote *types.Vote) {
	prvKey, exist := s.prvKey[vote.ProposerID]
	s.Require().True(exist)
	hash := hashVote(vote)
	var err error
	vote.Signature, err = prvKey.Sign(hash)
	s.Require().NoError(err)
}

func (s *AgreementTestSuite) copyVote(
	vote *types.Vote, proposer types.ValidatorID) *types.Vote {
	v := vote.Clone()
	v.ProposerID = proposer
	s.prepareVote(v)
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
	for vID := range s.prvKey {
		v := s.copyVote(vote, vID)
		s.Require().NoError(a.processVote(v))
	}
	a.nextState()
	// Pass1State
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteConfirm, vote.Type)
	for vID := range s.prvKey {
		v := s.copyVote(vote, vID)
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
