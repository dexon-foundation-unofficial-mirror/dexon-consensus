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
	"testing"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

type VoteCacheTestSuite struct {
	suite.Suite
	notarySetsRound00 []map[types.NodeID]struct{}
	notarySetsRound01 []map[types.NodeID]struct{}
	requiredVotes     int
	f                 int
	signers           []*utils.Signer
	cache             *VoteCache
}

func (s *VoteCacheTestSuite) SetupSuite() {
	var (
		prvKeys []crypto.PrivateKey
		pubKeys []crypto.PublicKey
		count   = 7
	)
	for i := 0; i < count; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		s.Require().NoError(err)
		prvKeys = append(prvKeys, prvKey)
		pubKeys = append(pubKeys, prvKey.PublicKey())
	}
	for _, k := range prvKeys {
		s.signers = append(s.signers, utils.NewSigner(k))
	}
	s.f = count / 3
	s.requiredVotes = 2*s.f + 1
	notarySet := make(map[types.NodeID]struct{})
	for _, k := range pubKeys {
		notarySet[types.NewNodeID(k)] = struct{}{}
	}
	for i := 0; i < 4; i++ {
		s.notarySetsRound00 = append(s.notarySetsRound00, notarySet)
	}
	for i := 0; i < 7; i++ {
		s.notarySetsRound01 = append(s.notarySetsRound01, notarySet)
	}
}

func (s *VoteCacheTestSuite) SetupTest() {
	s.cache = NewVoteCache(0)
	s.Require().NotNil(s.cache)
	s.Require().NoError(s.cache.AppendNotarySets(0, s.notarySetsRound00))
	s.Require().NoError(s.cache.AppendNotarySets(1, s.notarySetsRound01))
}

func (s *VoteCacheTestSuite) newVote(t types.VoteType, h common.Hash,
	pos types.Position, period uint64, signer *utils.Signer) *types.Vote {
	v := types.NewVote(t, h, period)
	v.Position = pos
	s.Require().NoError(signer.SignVote(v))
	return v
}

func (s *VoteCacheTestSuite) newVotes(t types.VoteType,
	h common.Hash, pos types.Position, period uint64) (votes []types.Vote) {
	for _, signer := range s.signers {
		votes = append(votes, *s.newVote(t, h, pos, period, signer))
	}
	s.Require().Len(votes, len(s.signers))
	return
}

func (s *VoteCacheTestSuite) testVotes(votes []types.Vote, sType SignalType) (
	signal *Signal) {
	refVote := votes[0]
	for idx := range votes {
		signals, err := s.cache.ProcessVote(&votes[idx])
		s.Require().NoError(err)
		switch {
		case idx+1 < s.requiredVotes:
			s.Require().Empty(signals)
		case idx+1 == s.requiredVotes:
			s.Require().Len(signals, 1)
			s.Require().Equal(signals[0].Type, sType)
			s.Require().Equal(signals[0].Position, refVote.Position)
			s.Require().Equal(signals[0].Period, refVote.Period)
			s.Require().Len(signals[0].Votes, s.requiredVotes)
			signal = signals[0]
		}
	}
	// Replay those votes again won't trigger another signal.
	for idx := range votes {
		signals, err := s.cache.ProcessVote(&votes[idx])
		s.Require().NoError(err)
		s.Require().Empty(signals)
	}
	return
}

func (s *VoteCacheTestSuite) votesToSortedHashes(
	votes []types.Vote) common.SortedHashes {
	var hashes common.Hashes
	for _, v := range votes {
		hashes = append(hashes, utils.HashVote(&v))
	}
	return common.NewSortedHashes(hashes)
}

func (s *VoteCacheTestSuite) checkIfNoDuplicatedHashes(hs common.SortedHashes) {
	hashSet := make(map[common.Hash]struct{})
	for _, h := range hs {
		_, duplicated := hashSet[h]
		s.Require().False(duplicated)
		hashSet[h] = struct{}{}
	}
}

func (s *VoteCacheTestSuite) TestPull() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 3}
	)
	// All votes for the same period should be pulled if no decide signal is
	// trigger.
	s.testVotes(s.newVotes(types.VoteFast, hash, position, 1), SignalLock)
	votesPreCom := s.newVotes(types.VotePreCom, hash, position, 2)
	s.testVotes(votesPreCom, SignalLock)
	hashesOfPulledVotes := s.votesToSortedHashes(s.cache.Pull(position, 0))
	s.checkIfNoDuplicatedHashes(hashesOfPulledVotes)
	s.Require().Equal(hashesOfPulledVotes, s.votesToSortedHashes(
		votesPreCom[:s.requiredVotes]))
	// Once a decide signal is triggered, only votes in that signal would be
	// pulled.
	votes := s.newVotes(types.VoteCom, hash, position, 1)
	signal := s.testVotes(votes, SignalDecide)
	hashesOfPulledVotes = s.votesToSortedHashes(s.cache.Pull(position, 0))
	s.checkIfNoDuplicatedHashes(hashesOfPulledVotes)
	s.Require().Equal(hashesOfPulledVotes, s.votesToSortedHashes(signal.Votes))
	// All later votes than the last triggered signal would be pulled along with
	// that signal.
	position.Height++
	votes = s.newVotes(types.VotePreCom, hash, position, 1)
	for _, v := range votes[:s.requiredVotes-1] {
		signals, err := s.cache.ProcessVote(&v)
		s.Require().NoError(err)
		s.Require().Empty(signals)
	}
	oldPos := position
	oldPos.Height--
	hashesOfPulledVotes = s.votesToSortedHashes(s.cache.Pull(oldPos, 0))
	s.checkIfNoDuplicatedHashes(hashesOfPulledVotes)
	s.Require().Equal(hashesOfPulledVotes, s.votesToSortedHashes(append(
		votes[:s.requiredVotes-1], signal.Votes...)))
}

func (s *VoteCacheTestSuite) TestPullCommitVotesFromOlderPeriods() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
		signals  []*Signal
	)
	partialProcess := func(vs []types.Vote) {
		signals = nil
		for idx := range vs[:s.requiredVotes-1] {
			sigs, err := s.cache.ProcessVote(&vs[idx])
			s.Require().NoError(err)
			signals = append(signals, sigs...)
		}
		s.Require().Empty(signals)
	}
	// Process some commit votes in period#1.
	hash = common.NewRandomHash()
	votesCom := s.newVotes(types.VoteCom, hash, position, 1)
	partialProcess(votesCom)
	// Process some fast-commit votes in period#1.
	hash = common.NewRandomHash()
	votesFastCom := s.newVotes(types.VoteFastCom, hash, position, 1)
	partialProcess(votesFastCom)
	// Process some pre-commit votes in period#1.
	hash = common.NewRandomHash()
	s.testVotes(s.newVotes(types.VotePreCom, hash, position, 1), SignalLock)
	// Process some votes able to trigger lock signals in period#3.
	hash = common.NewRandomHash()
	signal := s.testVotes(s.newVotes(types.VotePreCom, hash, position, 3),
		SignalLock)
	// Pull with lockIter == 2.
	hashesOfPulledVotes := s.votesToSortedHashes(s.cache.Pull(position, 2))
	s.checkIfNoDuplicatedHashes(hashesOfPulledVotes)
	s.Require().Equal(hashesOfPulledVotes, s.votesToSortedHashes(append(
		append(signal.Votes, votesCom[:s.requiredVotes-1]...),
		votesFastCom[:s.requiredVotes-1]...)))
}

func (s *VoteCacheTestSuite) TestResult() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 3}
	)
	// An agreement result with fast votes can trigger a decide signal.
	result := &types.AgreementResult{
		BlockHash: hash,
		Position:  position,
		Votes:     s.newVotes(types.VoteFastCom, hash, position, 1),
	}
	signals, err := s.cache.ProcessResult(result)
	s.Require().NoError(err)
	s.Require().Len(signals, 1)
	s.Require().Equal(signals[0].Type, SignalDecide)
	s.Require().Equal(signals[0].Position, position)
	s.Require().Equal(signals[0].Period, uint64(1))
	s.Require().Len(signals[0].Votes, len(result.Votes))
	// An agreement result with commit votes can trigger a decide signal.
	position.Height++
	result = &types.AgreementResult{
		BlockHash: hash,
		Position:  position,
		Votes:     s.newVotes(types.VoteCom, hash, position, 1),
	}
	signals, err = s.cache.ProcessResult(result)
	s.Require().NoError(err)
	s.Require().Len(signals, 1)
	s.Require().Equal(signals[0].Type, SignalDecide)
	s.Require().Equal(signals[0].Position, position)
	s.Require().Equal(signals[0].Period, uint64(1))
	s.Require().Len(signals[0].Votes, len(result.Votes))
	// An agreement result from older position would be ignored.
	signals, err = s.cache.ProcessResult(&types.AgreementResult{
		BlockHash: hash,
		Votes:     s.newVotes(types.VoteCom, hash, position, 1),
		Position: types.Position{
			Round:   position.Round,
			ChainID: position.ChainID,
			Height:  position.Height - 1,
		},
	})
	s.Require().NoError(err)
	s.Require().Len(signals, 0)
	// An agreement result contains fork votes should be detected.
	position.Height++
	vote := s.newVote(
		types.VoteCom, common.NewRandomHash(), position, 1, s.signers[0])
	signals, err = s.cache.ProcessVote(vote)
	s.Require().NoError(err)
	s.Require().Empty(signals)
	hash = common.NewRandomHash()
	signals, err = s.cache.ProcessResult(&types.AgreementResult{
		BlockHash: hash,
		Position:  position,
		Votes:     s.newVotes(types.VoteCom, hash, position, 1),
	})
	s.Require().NoError(err)
	s.Require().Len(signals, 2)
	s.Require().Equal(signals[0].Type, SignalFork)
	s.Require().Equal(signals[0].Position, position)
	s.Require().Equal(signals[0].Period, uint64(1))
	// The other votes are still counted, therefore, there should be one decide
	// signal.
	s.Require().Equal(signals[1].Type, SignalDecide)
	s.Require().Equal(signals[1].Position, position)
	s.Require().Equal(signals[1].Period, uint64(1))
}

func (s *VoteCacheTestSuite) TestBasicUsage() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
	)
	// If there are 2t+1 pre-commit votes for the same value, it should raise
	// a lock signal.
	s.testVotes(s.newVotes(types.VotePreCom, hash, position, 1), SignalLock)
	// If there are 2t+1 commit votes for the same value, it should raise
	// a decide signal.
	s.testVotes(s.newVotes(types.VoteCom, hash, position, 1), SignalDecide)
	// If there are 2t+1 fast votes for the same value, it should raise
	// a lock signal.
	position.Height++
	hash = common.NewRandomHash()
	s.testVotes(s.newVotes(types.VoteFast, hash, position, 1), SignalLock)
	// If there are 2t+1 commit votes for SKIP, it should raise a forward
	// signal.
	position.Height++
	hash = common.NewRandomHash()
	votes := s.newVotes(types.VoteCom, types.SkipBlockHash, position, 1)
	s.testVotes(votes, SignalForward)
	// If there are 2t+1 commit votes for different value, it should raise
	// a forward signal.
	position.Height++
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votes = nil
	votes = append(votes, votes01[0])
	votes = append(votes, votes02[1:]...)
	s.testVotes(votes, SignalForward)
	// If a forked vote is detected, it should raise a fork signal.
	position.Height++
	hash = common.NewRandomHash()
	votes01 = s.newVotes(types.VotePreCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 = s.newVotes(types.VotePreCom, hash, position, 1)
	signals, err := s.cache.ProcessVote(&votes01[0])
	s.Require().NoError(err)
	s.Require().Empty(signals)
	signals, err = s.cache.ProcessVote(&votes02[0])
	s.Require().NoError(err)
	s.Require().Len(signals, 1)
	s.Require().Equal(signals[0].Type, SignalFork)
	s.Require().Equal(signals[0].Position, position)
	s.Require().Equal(signals[0].Period, uint64(1))
	s.Require().Len(signals[0].Votes, 2)
	s.Require().Equal(signals[0].Votes[0], votes01[0])
	s.Require().Equal(signals[0].Votes[1], votes02[0])
}

func (s *VoteCacheTestSuite) TestPurgeByDecideSignal() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
	)
	// There are some pre-commit votes unable to trigger any signal.
	votes := s.newVotes(types.VotePreCom, hash, position, 1)
	for idx := range votes[:s.requiredVotes-1] {
		signals, err := s.cache.ProcessVote(&votes[idx])
		s.Require().NoError(err)
		s.Require().Empty(signals)
	}
	// Let's check internal caches, corresponding caches are existed.
	chainCache, err := s.cache.chain(position.ChainID)
	s.Require().NoError(err)
	s.Require().NotNil(chainCache.votesInfo(&votes[0], false))
	// We receive some commit votes position that can trigger some non-forked
	// signal, then those pre-commit votes should be purged.
	s.testVotes(s.newVotes(types.VoteCom, hash, position, 1), SignalDecide)
	// Let's check internal caches, those older caches should be purged.
	s.Require().Nil(chainCache.votesInfo(&votes[0], false))
}

func (s *VoteCacheTestSuite) TestPurgeByNewerPeriod() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
	)
	// There are some pre-commit votes unable to trigger any signal.
	votes := s.newVotes(types.VotePreCom, hash, position, 1)
	for idx := range votes[:s.requiredVotes-1] {
		signals, err := s.cache.ProcessVote(&votes[idx])
		s.Require().NoError(err)
		s.Require().Empty(signals)
	}
	// Let's check internal caches, corresponding caches are existed.
	chainCache, err := s.cache.chain(position.ChainID)
	s.Require().NoError(err)
	s.Require().NotNil(chainCache.votesInfo(&votes[0], false))
	// We receive some pre-commit votes position that can trigger some
	// non-forked signal, then those pre-commit votes should be purged.
	s.testVotes(s.newVotes(types.VotePreCom, hash, position, 2), SignalLock)
	// Let's check internal caches, those older caches should be purged.
	s.Require().True(chainCache.votesInfo(&votes[0], false).isPurged())
}

func (s *VoteCacheTestSuite) TestPurgeByNewerPosition() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
	)
	// There are some commit votes unable to trigger any signal.
	votes := s.newVotes(types.VoteCom, hash, position, 1)
	for idx := range votes[:s.requiredVotes-1] {
		signals, err := s.cache.ProcessVote(&votes[idx])
		s.Require().NoError(err)
		s.Require().Empty(signals)
	}
	// Let's check internal caches, corresponding caches are existed.
	chainCache, err := s.cache.chain(position.ChainID)
	s.Require().NoError(err)
	s.Require().NotNil(chainCache.votesInfo(&votes[0], false))
	// We receive some pre-commit votes position that can trigger some
	// non-forked signal, then those commit votes should be purged.
	position.Height++
	s.testVotes(s.newVotes(types.VotePreCom, hash, position, 1), SignalLock)
	// Let's check internal caches, those older caches should be purged.
	s.Require().Nil(chainCache.votesInfo(&votes[0], false))
}

func (s *VoteCacheTestSuite) TestPurgeByImpossibleToAgreeOnOneHash() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
		signals  []*Signal
	)
	// Trigger fast-forward via condition#3, and make an "impossible to agree
	// on one hash" scenario.
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votesSkip := s.newVotes(types.VoteCom, types.SkipBlockHash, position, 1)
	votes := append(append(votes01[:s.f], votes02[s.f:2*s.f]...),
		votesSkip[2*s.f])
	for idx := range votes {
		sigs, err := s.cache.ProcessVote(&votes[idx])
		s.Require().NoError(err)
		signals = append(signals, sigs...)
	}
	s.Require().Len(signals, 1)
	s.Require().Equal(signals[0].Type, SignalForward)
	s.Require().Equal(signals[0].VType, types.VoteCom)
	chainCache, err := s.cache.chain(position.ChainID)
	s.Require().NoError(err)
	s.Require().True(chainCache.votesInfo(&votes01[0], false).isPurged())
	s.Require().Equal(chainCache.refSignals[SignalForward], signals[0])
}

func (s *VoteCacheTestSuite) TestDecideInOlderPeriod() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
	)
	// Trigger fast-forward via condition#3.
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votes := append(votes01[:1], votes02[1:s.requiredVotes]...)
	s.testVotes(votes, SignalForward)
	// Trigger fast-forward by pre-commit votes in later period.
	hash = common.NewRandomHash()
	s.testVotes(s.newVotes(types.VotePreCom, hash, position, 2), SignalLock)
	// Process a commit vote in period#1, should still trigger a decide signal.
	signals, err := s.cache.ProcessVote(&votes02[s.requiredVotes])
	s.Require().NoError(err)
	s.Require().Len(signals, 1)
	s.Require().Equal(signals[0].Type, SignalDecide)
	s.Require().Equal(signals[0].Position, position)
	s.Require().Equal(signals[0].Period, uint64(1))
}

func (s *VoteCacheTestSuite) TestDecideAfterForward() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 1, ChainID: 1, Height: 1}
	)
	// Trigger fast-forward via condition#3.
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votes := append(votes01[:1], votes02[1:s.requiredVotes]...)
	s.testVotes(votes, SignalForward)
	signals, err := s.cache.ProcessVote(&votes02[s.requiredVotes])
	s.Require().NoError(err)
	s.Require().Len(signals, 1)
	s.Require().Equal(signals[0].Type, SignalDecide)
}

func TestVoteCache(t *testing.T) {
	suite.Run(t, new(VoteCacheTestSuite))
}
