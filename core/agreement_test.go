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
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

// agreementTestReceiver implements core.agreementReceiver.
type agreementTestReceiver struct {
	s              *AgreementTestSuite
	agreementIndex int
}

func (r *agreementTestReceiver) VerifyPartialSignature(*types.Vote) (bool, bool) {
	return true, false
}

func (r *agreementTestReceiver) ProposeVote(vote *types.Vote) {
	vote.Position = r.s.agreementID
	r.s.voteChan <- vote
}

func (r *agreementTestReceiver) ProposeBlock() common.Hash {
	block := r.s.proposeBlock(
		r.s.agreement[r.agreementIndex].data.ID,
		r.s.agreement[r.agreementIndex].data.leader.hashCRS,
		[]byte{})
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

// agreementTestForkReporter implement core.forkReporter.
type agreementTestForkReporter struct {
	s *AgreementTestSuite
}

func (r *agreementTestReceiver) ReportForkVote(v1, v2 *types.Vote) {
	r.s.forkVoteChan <- v1.BlockHash
	r.s.forkVoteChan <- v2.BlockHash
}

func (r *agreementTestReceiver) ReportForkBlock(b1, b2 *types.Block) {
	r.s.forkBlockChan <- b1.Hash
	r.s.forkBlockChan <- b2.Hash
}

func (s *AgreementTestSuite) proposeBlock(
	nID types.NodeID, crs common.Hash, payload []byte) *types.Block {
	block := &types.Block{
		ProposerID: nID,
		Position:   types.Position{Height: types.GenesisHeight},
		Payload:    payload,
	}
	signer, exist := s.signers[block.ProposerID]
	s.Require().True(exist)
	s.Require().NoError(signer.SignCRS(block, crs))
	s.Require().NoError(signer.SignBlock(block))
	s.block[block.Hash] = block

	return block
}

type AgreementTestSuite struct {
	suite.Suite
	ID                 types.NodeID
	signers            map[types.NodeID]*utils.Signer
	voteChan           chan *types.Vote
	blockChan          chan common.Hash
	confirmChan        chan common.Hash
	forkVoteChan       chan common.Hash
	forkBlockChan      chan common.Hash
	block              map[common.Hash]*types.Block
	pulledBlocks       map[common.Hash]struct{}
	agreement          []*agreement
	agreementID        types.Position
	defaultValidLeader validLeaderFn
}

func (s *AgreementTestSuite) SetupTest() {
	prvKey, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	s.ID = types.NewNodeID(prvKey.PublicKey())
	s.signers = map[types.NodeID]*utils.Signer{
		s.ID: utils.NewSigner(prvKey),
	}
	s.voteChan = make(chan *types.Vote, 100)
	s.blockChan = make(chan common.Hash, 100)
	s.confirmChan = make(chan common.Hash, 100)
	s.forkVoteChan = make(chan common.Hash, 100)
	s.forkBlockChan = make(chan common.Hash, 100)
	s.block = make(map[common.Hash]*types.Block)
	s.pulledBlocks = make(map[common.Hash]struct{})
	s.agreementID = types.Position{Height: types.GenesisHeight}
	s.defaultValidLeader = func(*types.Block, common.Hash) (bool, error) {
		return true, nil
	}
}

func (s *AgreementTestSuite) newAgreement(
	numNotarySet, leaderIdx int, validLeader validLeaderFn) (*agreement, types.NodeID) {
	s.Require().True(leaderIdx < numNotarySet)
	logger := &common.NullLogger{}
	leader := newLeaderSelector(validLeader, logger)
	agreementIdx := len(s.agreement)
	var leaderNode types.NodeID
	notarySet := make(map[types.NodeID]struct{})
	for i := 0; i < numNotarySet-1; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		nID := types.NewNodeID(prvKey.PublicKey())
		notarySet[nID] = struct{}{}
		s.signers[nID] = utils.NewSigner(prvKey)
		if i == leaderIdx-1 {
			leaderNode = nID
		}
	}
	if leaderIdx == 0 {
		leaderNode = s.ID
	}
	notarySet[s.ID] = struct{}{}
	agreement := newAgreement(
		s.ID,
		&agreementTestReceiver{
			s:              s,
			agreementIndex: agreementIdx,
		},
		leader,
		s.signers[s.ID],
		logger,
	)
	agreement.restart(notarySet, utils.GetBAThreshold(&types.Config{
		NotarySetSize: uint32(len(notarySet)),
	}), s.agreementID, leaderNode,
		common.NewRandomHash())
	s.agreement = append(s.agreement, agreement)
	return agreement, leaderNode
}

func (s *AgreementTestSuite) copyVote(
	vote *types.Vote, proposer types.NodeID) *types.Vote {
	v := vote.Clone()
	s.signers[proposer].SignVote(v)
	return v
}

func (s *AgreementTestSuite) prepareVote(
	nID types.NodeID, voteType types.VoteType, blockHash common.Hash,
	period uint64) (
	vote *types.Vote) {
	vote = types.NewVote(voteType, blockHash, period)
	vote.Position = types.Position{Height: types.GenesisHeight}
	s.Require().NoError(s.signers[nID].SignVote(vote))
	return
}

func (s *AgreementTestSuite) TestSimpleConfirm() {
	a, leaderNode := s.newAgreement(4, 0, s.defaultValidLeader)
	s.Require().Equal(s.ID, leaderNode)
	// FastState
	a.nextState()
	// FastVoteState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().Equal(s.ID, block.ProposerID)
	s.Require().NoError(a.processBlock(block))
	// Wait some time for go routine in processBlock to finish.
	time.Sleep(500 * time.Millisecond)
	s.Require().Len(s.voteChan, 1)
	fastVote := <-s.voteChan
	s.Equal(types.VoteFast, fastVote.Type)
	s.Equal(blockHash, fastVote.BlockHash)
	s.Require().Len(s.voteChan, 0)
	a.nextState()
	// InitialState
	a.nextState()
	// PreCommitState
	a.nextState()
	// CommitState
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VotePreCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	// Fast-votes should be ignored.
	for nID := range s.signers {
		v := s.copyVote(fastVote, nID)
		s.Require().NoError(a.processVote(v))
	}
	s.Require().Len(s.voteChan, 0)
	s.Equal(uint64(1), a.data.lockIter)
	for nID := range s.signers {
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
	s.Equal(uint64(2), a.data.lockIter)
	for nID := range s.signers {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Com-Votes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(blockHash, confirmBlock)
}

func (s *AgreementTestSuite) TestPartitionOnCommitVote() {
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	// FastState
	a.nextState()
	// FastVoteState
	a.nextState()
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
	for nID := range s.signers {
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
	s.Equal(uint64(2), a.data.lockIter)
	// RepeateVoteState
	a.nextState()
	s.True(a.pullVotes())
	s.Require().Len(s.voteChan, 0)
}

func (s *AgreementTestSuite) TestFastConfirmLeader() {
	a, leaderNode := s.newAgreement(4, 0, s.defaultValidLeader)
	s.Require().Equal(s.ID, leaderNode)
	// FastState
	a.nextState()
	// FastVoteState
	s.Require().Len(s.blockChan, 1)
	blockHash := <-s.blockChan
	block, exist := s.block[blockHash]
	s.Require().True(exist)
	s.Require().Equal(s.ID, block.ProposerID)
	s.Require().NoError(a.processBlock(block))
	// Wait some time for go routine in processBlock to finish.
	time.Sleep(500 * time.Millisecond)
	s.Require().Len(s.voteChan, 1)
	vote := <-s.voteChan
	s.Equal(types.VoteFast, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	s.Require().Len(s.voteChan, 0)
	for nID := range s.signers {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Fast-Votes.
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	s.Equal(types.VoteFastCom, vote.Type)
	s.Equal(blockHash, vote.BlockHash)
	for nID := range s.signers {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Fast-ConfirmVotes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(blockHash, confirmBlock)
}

func (s *AgreementTestSuite) TestFastConfirmNonLeader() {
	a, leaderNode := s.newAgreement(4, 1, s.defaultValidLeader)
	s.Require().NotEqual(s.ID, leaderNode)
	// FastState
	a.nextState()
	// FastVoteState
	s.Require().Len(s.blockChan, 0)
	block := s.proposeBlock(leaderNode, a.data.leader.hashCRS, []byte{})
	s.Require().Equal(leaderNode, block.ProposerID)
	s.Require().NoError(a.processBlock(block))
	// Wait some time for go routine in processBlock to finish.
	time.Sleep(500 * time.Millisecond)
	var vote *types.Vote
	select {
	case vote = <-s.voteChan:
	case <-time.After(500 * time.Millisecond):
		s.FailNow("Should propose vote")
	}
	s.Equal(types.VoteFast, vote.Type)
	s.Equal(block.Hash, vote.BlockHash)
	for nID := range s.signers {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Fast-Votes.
	s.Require().Len(s.voteChan, 1)
	vote = <-s.voteChan
	for nID := range s.signers {
		v := s.copyVote(vote, nID)
		s.Require().NoError(a.processVote(v))
	}
	// We have enough of Fast-ConfirmVotes.
	s.Require().Len(s.confirmChan, 1)
	confirmBlock := <-s.confirmChan
	s.Equal(block.Hash, confirmBlock)
}

func (s *AgreementTestSuite) TestFastForwardCond1() {
	votes := 0
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	a.data.lockIter = 1
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
		s.FailNow("Unexpected fast forward.")
	default:
	}
	s.Equal(hash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockIter)
	s.Equal(uint64(3), a.data.period)

	// No fast forward if vote.BlockHash == SKIP
	a.data.lockIter = 6
	a.data.period = 8
	a.data.lockValue = types.NullBlockHash
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, types.SkipBlockHash, uint64(7))
		s.Require().NoError(a.processVote(vote))
	}

	select {
	case <-a.done():
		s.FailNow("Unexpected fast forward.")
	default:
	}

	// No fast forward if lockValue == vote.BlockHash.
	a.data.lockIter = 11
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
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	a.data.period = 1
	done := a.done()
	hash := common.NewRandomHash()
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, hash, uint64(2))
		s.Require().NoError(a.processVote(vote))
		if votes++; votes == 3 {
			break
		}
	}

	select {
	case <-done:
	default:
		s.FailNow("Expecting fast forward for pending done() call.")
	}
	select {
	case <-a.done():
	default:
		s.FailNow("Expecting fast forward.")
	}
	s.Equal(hash, a.data.lockValue)
	s.Equal(uint64(2), a.data.lockIter)
	s.Equal(uint64(2), a.data.period)

	// No fast forward if vote.BlockHash == SKIP
	a.data.period = 6
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VotePreCom, types.SkipBlockHash, uint64(7))
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
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	a.data.period = 1
	done := a.done()
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteCom, common.NewRandomHash(), uint64(2))
		votes = append(votes, vote)
		s.Require().NoError(a.processVote(vote))
		if numVotes++; numVotes == 3 {
			break
		}
	}

	select {
	case <-done:
	default:
		s.FailNow("Expecting fast forward for pending done() call.")
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
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	a.data.period = 5

	// No decide if com-vote on SKIP.
	for nID := range a.notarySet {
		vote := s.prepareVote(nID, types.VoteCom, types.SkipBlockHash, uint64(2))
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

func (s *AgreementTestSuite) TestForkVote() {
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	a.data.period = 2
	for nID := range a.notarySet {
		v01 := s.prepareVote(nID, types.VotePreCom, common.NewRandomHash(), 2)
		v02 := s.prepareVote(nID, types.VotePreCom, common.NewRandomHash(), 2)
		s.Require().NoError(a.processVote(v01))
		s.Require().IsType(&ErrForkVote{}, a.processVote(v02))
		s.Require().Equal(v01.BlockHash, <-s.forkVoteChan)
		s.Require().Equal(v02.BlockHash, <-s.forkVoteChan)
		break
	}
}

func (s *AgreementTestSuite) TestForkBlock() {
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	for nID := range a.notarySet {
		b01 := s.proposeBlock(nID, a.data.leader.hashCRS, []byte{1})
		b02 := s.proposeBlock(nID, a.data.leader.hashCRS, []byte{2})
		s.Require().NoError(a.processBlock(b01))
		s.Require().IsType(&ErrFork{}, a.processBlock(b02))
		s.Require().Equal(b01.Hash, <-s.forkBlockChan)
		s.Require().Equal(b02.Hash, <-s.forkBlockChan)
	}
}

func (s *AgreementTestSuite) TestFindBlockInPendingSet() {
	a, leaderNode := s.newAgreement(4, 0, func(*types.Block, common.Hash) (bool, error) {
		return false, nil
	})
	block := s.proposeBlock(leaderNode, a.data.leader.hashCRS, []byte{})
	s.Require().NoError(a.processBlock(block))
	// Make sure the block goes to pending pool in leader selector.
	block, exist := a.data.leader.findPendingBlock(block.Hash)
	s.Require().True(exist)
	s.Require().NotNil(block)
	// This block is allowed to be found by findBlockNoLock.
	block, exist = a.findBlockNoLock(block.Hash)
	s.Require().True(exist)
	s.Require().NotNil(block)
}

func (s *AgreementTestSuite) TestConfirmWithBlock() {
	a, _ := s.newAgreement(4, -1, s.defaultValidLeader)
	block := &types.Block{
		Hash:       common.NewRandomHash(),
		Position:   a.agreementID(),
		Randomness: []byte{0x1, 0x2, 0x3, 0x4},
	}
	a.processFinalizedBlock(block)
	s.Require().Len(s.confirmChan, 1)
	confirm := <-s.confirmChan
	s.Equal(block.Hash, confirm)
	s.True(a.confirmed())
}

func TestAgreement(t *testing.T) {
	suite.Run(t, new(AgreementTestSuite))
}
