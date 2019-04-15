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
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

func countVotes(votes []types.Vote, p types.Position, period uint64,
	t types.VoteType) (cnt int) {
	for _, v := range votes {
		if !v.Position.Equal(p) {
			continue
		}
		if v.Period != period {
			continue
		}
		if v.Type != t {
			continue
		}
		cnt++
	}
	return
}

type testAgreementCacheReceiver struct {
	s           *AgreementCacheTestSuite
	isTsigValid bool
}

func (r *testAgreementCacheReceiver) GetNotarySet(
	round uint64) (map[types.NodeID]struct{}, error) {
	return r.s.notarySet, nil
}

func (r *testAgreementCacheReceiver) RecoverTSIG(
	blockHash common.Hash, votes []types.Vote) ([]byte, error) {
	return []byte("OK"), nil
}

func (r *testAgreementCacheReceiver) VerifyTSIG(
	round uint64, hash common.Hash, tSig []byte) (bool, error) {
	return r.isTsigValid, nil
}

func (r *testAgreementCacheReceiver) VerifySignature(hash common.Hash,
	sig crypto.Signature) bool {
	return bytes.Compare(sig.Signature, []byte("OK")) == 0
}

type AgreementCacheTestSuite struct {
	suite.Suite
	notarySet     map[types.NodeID]struct{}
	requiredVotes int
	f             int
	roundLength   uint64
	lambdaBA      time.Duration
	signers       []*utils.Signer
	cache         *agreementCache
	recv          *testAgreementCacheReceiver
}

func (s *AgreementCacheTestSuite) SetupSuite() {
	s.setupNodes(7)
}

func (s *AgreementCacheTestSuite) SetupTest() {
	s.recv.isTsigValid = true
	s.newCache(s.newRoundEvents(1))
}

func (s *AgreementCacheTestSuite) setupNodes(count int) {
	var (
		prvKeys []crypto.PrivateKey
		pubKeys []crypto.PublicKey
	)
	for i := 0; i < count; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		if err != nil {
			panic(err)
		}
		prvKeys = append(prvKeys, prvKey)
		pubKeys = append(pubKeys, prvKey.PublicKey())
	}
	for _, k := range prvKeys {
		s.signers = append(s.signers, utils.NewSigner(k))
	}
	s.f = count / 3
	s.requiredVotes = 2*s.f + 1
	s.roundLength = 100
	s.lambdaBA = 100 * time.Millisecond
	s.notarySet = make(map[types.NodeID]struct{})
	for _, k := range pubKeys {
		s.notarySet[types.NewNodeID(k)] = struct{}{}
	}
	s.recv = &testAgreementCacheReceiver{s: s}

}

func (s *AgreementCacheTestSuite) newVote(t types.VoteType, h common.Hash,
	p types.Position, period uint64, signer *utils.Signer) *types.Vote {
	v := types.NewVote(t, h, period)
	v.Position = p
	if err := signer.SignVote(v); err != nil {
		panic(err)
	}
	return v
}

func (s *AgreementCacheTestSuite) newVotes(t types.VoteType, h common.Hash,
	p types.Position, period uint64) (votes []types.Vote) {
	for _, signer := range s.signers {
		votes = append(votes, *s.newVote(t, h, p, period, signer))
	}
	return
}

func (s *AgreementCacheTestSuite) testVotes(
	votes []types.Vote, eType agreementEventType) (e *agreementEvent) {
	for idx := range votes {
		evts, err := s.cache.processVote(votes[idx])
		s.Require().NoError(err)
		if idx+1 < s.requiredVotes {
			s.Require().Empty(evts)
		}
		if idx+1 == s.requiredVotes {
			s.Require().Len(evts, 1)
			s.Require().Equal(evts[0].evtType, eType)
			s.Require().Equal(evts[0].Position, votes[0].Position)
			if votes[0].Position.Round < DKGDelayRound ||
				eType != agreementEventDecide {
				s.Require().Equal(evts[0].period, votes[0].Period)
				s.Require().Len(evts[0].Votes, s.requiredVotes)
			}
			e = evts[0]
			break
		}
	}
	// Replay those votes again won't trigger another event.
	for idx := range votes {
		evts, err := s.cache.processVote(votes[idx])
		s.Require().NoError(err)
		s.Require().Empty(evts)
		if idx+1 == s.requiredVotes {
			break
		}
	}
	return
}

func (s *AgreementCacheTestSuite) newRoundEvents(
	round uint64) (rEvts []utils.RoundEventParam) {
	h := types.GenesisHeight
	for r := uint64(0); r <= round; r++ {
		rEvts = append(rEvts, utils.RoundEventParam{
			Round:       r,
			BeginHeight: h,
			Config: &types.Config{
				LambdaBA:    s.lambdaBA,
				RoundLength: s.roundLength,
			},
		})
		h += s.roundLength
	}
	return
}

func (s *AgreementCacheTestSuite) newCache(rEvts []utils.RoundEventParam) {
	s.cache = newAgreementCache(s.recv)
	if err := s.cache.notifyRoundEvents(rEvts); err != nil {
		panic(err)
	}
}

func (s *AgreementCacheTestSuite) generateTestData(
	positionCount, periodCount uint64,
	voteTypes []types.VoteType, voteCount int) (
	lastPosition types.Position,
	votes []types.Vote, bs []*types.Block, rs []*types.AgreementResult) {
	gen := func(p types.Position, period uint64) (
		vs []types.Vote, b *types.Block, r *types.AgreementResult) {
		h := common.NewRandomHash()
		var vCom []types.Vote
		for _, t := range voteTypes {
			v := s.newVotes(t, h, p, period)
			if t == types.VoteFastCom || t == types.VoteCom {
				vCom = v
			}
			v = v[:voteCount]
			vs = append(vs, v...)
		}
		r = &types.AgreementResult{BlockHash: h, Position: p}
		b = &types.Block{Hash: h, Position: p}
		if p.Round >= DKGDelayRound {
			r.Randomness = []byte("cold-freeze")
			b.Randomness = []byte("blackhole-rocks")
		} else {
			r.Votes = vCom[:s.requiredVotes]
			r.Randomness = NoRand
			b.Randomness = NoRand
		}
		return
	}
	for i := uint64(0); i < positionCount; i++ {
		lastPosition = types.Position{Height: types.GenesisHeight + i}
		lastPosition.Round = uint64(i) / s.roundLength
		for period := uint64(0); period < periodCount; period++ {
			vs, b, r := gen(lastPosition, period)
			votes = append(votes, vs...)
			bs = append(bs, b)
			rs = append(rs, r)
		}
	}
	return
}

func (s *AgreementCacheTestSuite) TestAgreementResult() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 0, Height: 3}
	)
	// An agreement result with fast-commit votes can trigger a decide event.
	r := &types.AgreementResult{
		BlockHash:  hash,
		Position:   position,
		Votes:      s.newVotes(types.VoteFastCom, hash, position, 1),
		Randomness: NoRand,
	}
	evts, err := s.cache.processAgreementResult(r)
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
	s.Require().Equal(evts[0].Position, position)
	s.Require().Equal(evts[0].period, uint64(1))
	s.Require().Len(evts[0].Votes, len(r.Votes))
	// An agreement result with commit votes can trigger a decide event.
	position.Height++
	r = &types.AgreementResult{
		BlockHash:  hash,
		Position:   position,
		Votes:      s.newVotes(types.VoteCom, hash, position, 1),
		Randomness: NoRand,
	}
	evts, err = s.cache.processAgreementResult(r)
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
	s.Require().Equal(evts[0].Position, position)
	s.Require().Equal(evts[0].period, uint64(1))
	s.Require().Len(evts[0].Votes, len(r.Votes))
	// An agreement result from older position would be ignored.
	position.Height--
	evts, err = s.cache.processAgreementResult(&types.AgreementResult{
		BlockHash:  hash,
		Position:   position,
		Votes:      s.newVotes(types.VoteCom, hash, position, 1),
		Randomness: NoRand,
	})
	s.Require().NoError(err)
	s.Require().Empty(evts)
	position.Height++
	// An agreement result contains fork votes should be detected, while still
	// be able to trigger a decide event.
	position.Height++
	forkedVote := s.newVote(
		types.VoteCom, common.NewRandomHash(), position, 1, s.signers[0])
	evts, err = s.cache.processVote(*forkedVote)
	s.Require().NoError(err)
	s.Require().Empty(evts)
	hash = common.NewRandomHash()
	evts, err = s.cache.processAgreementResult(&types.AgreementResult{
		BlockHash:  hash,
		Position:   position,
		Votes:      s.newVotes(types.VoteCom, hash, position, 1),
		Randomness: NoRand,
	})
	s.Require().NoError(err)
	s.Require().Len(evts, 2)
	s.Require().Equal(evts[0].evtType, agreementEventFork)
	s.Require().Equal(evts[0].Position, position)
	s.Require().Equal(evts[0].period, uint64(1))
	s.Require().Equal(evts[1].evtType, agreementEventDecide)
	s.Require().Equal(evts[1].Position, position)
	s.Require().Equal(evts[1].period, uint64(1))
	// An agreement result with valid TSIG can trigger a decide event.
	position.Round = 1
	position.Height++
	hash = common.NewRandomHash()
	evts, err = s.cache.processAgreementResult(&types.AgreementResult{
		BlockHash:  hash,
		Position:   position,
		Randomness: []byte("OK-LA"),
	})
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
	s.Require().Equal(evts[0].Position, position)
	s.Require().Equal(evts[0].Randomness, []byte("OK-LA"))
	// An agreement result with invalid TSIG would raise error.
	s.recv.isTsigValid = false
	position.Height++
	hash = common.NewRandomHash()
	evts, err = s.cache.processAgreementResult(&types.AgreementResult{
		BlockHash:  hash,
		Position:   position,
		Randomness: []byte("NOK"),
	})
	s.Require().EqualError(err, ErrIncorrectBlockRandomness.Error())
	s.Require().Empty(evts)
	s.recv.isTsigValid = true
}

func (s *AgreementCacheTestSuite) TestBasicUsage() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 0, Height: types.GenesisHeight}
	)
	// If there are 2t+1 pre-commit votes for the same value, it should raise
	// a lock event.
	s.testVotes(
		s.newVotes(types.VotePreCom, hash, position, 1), agreementEventLock)
	// If there are 2t+1 commit votes for the same value, it should raise
	// a decide event.
	s.testVotes(
		s.newVotes(types.VoteCom, hash, position, 1), agreementEventDecide)
	// If there are 2t+1 fast votes for the same value, it should raise
	// a lock event.
	position.Height++
	hash = common.NewRandomHash()
	s.testVotes(
		s.newVotes(types.VoteFast, hash, position, 1), agreementEventLock)
	// If there are 2t+1 commit votes for SKIP, it should raise a forward
	// event.
	position.Height++
	hash = types.SkipBlockHash
	s.testVotes(
		s.newVotes(types.VoteCom, hash, position, 1), agreementEventForward)
	// If there are 2t+1 commit votes for different value, it should raise
	// a forward event.
	position.Height++
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votes := append([]types.Vote(nil), votes01[0])
	votes = append(votes, votes02[1:]...)
	s.testVotes(votes, agreementEventForward)
	// If a forked vote is detected, it should raise a fork event.
	position.Height++
	hash = common.NewRandomHash()
	votes01 = s.newVotes(types.VotePreCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 = s.newVotes(types.VotePreCom, hash, position, 1)
	evts, err := s.cache.processVote(votes01[0])
	s.Require().NoError(err)
	s.Require().Empty(evts)
	evts, err = s.cache.processVote(votes02[0])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventFork)
	s.Require().Equal(evts[0].Position, position)
	s.Require().Equal(evts[0].period, uint64(1))
	s.Require().Len(evts[0].Votes, 2)
	s.Require().Equal(evts[0].Votes[0], votes01[0])
	s.Require().Equal(evts[0].Votes[1], votes02[0])
}

func (s *AgreementCacheTestSuite) TestDecideInOlderPeriod() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 0, Height: types.GenesisHeight}
	)
	// Trigger fast-forward via condition#3.
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votes := append(votes01[:1], votes02[1:s.requiredVotes]...)
	s.testVotes(votes, agreementEventForward)
	// Trigger fast-forward by pre-commit votes in later period.
	hash = common.NewRandomHash()
	s.testVotes(
		s.newVotes(types.VotePreCom, hash, position, 2), agreementEventLock)
	// Process a commit vote in period#1, should still trigger a decide event.
	evts, err := s.cache.processVote(votes02[s.requiredVotes])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
	s.Require().Equal(evts[0].Position, position)
	s.Require().Equal(evts[0].period, uint64(1))
}

func (s *AgreementCacheTestSuite) TestDecideAfterForward() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{
			Round:  1,
			Height: types.GenesisHeight + s.roundLength,
		}
	)
	// Trigger fast-forward via condition#3.
	hash = common.NewRandomHash()
	votes01 := s.newVotes(types.VoteCom, hash, position, 1)
	hash = common.NewRandomHash()
	votes02 := s.newVotes(types.VoteCom, hash, position, 1)
	votes := append(votes01[:1], votes02[1:s.requiredVotes]...)
	s.testVotes(votes, agreementEventForward)
	// Send remain votes of one hash to see if a decide event can be triggered.
	evts, err := s.cache.processVote(votes02[s.requiredVotes])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
}

func (s *AgreementCacheTestSuite) TestDecideByFinalizedBlock() {
	// TODO(mission):
	// A finalized block from round before DKGDelayRound won't trigger a decide
	// event.

	// A finalized block from round after DKGDelayRound would trigger a decide
	// event.

	// A finalized block from older position would be ignored.
}

func (s *AgreementCacheTestSuite) TestFastBA() {
	var (
		hash     = common.NewRandomHash()
		position = types.Position{Round: 0, Height: types.GenesisHeight}
	)
	test := func() {
		// Fast -> FastCom, successfuly confirmed by Fast mode.
		s.testVotes(s.newVotes(types.VoteFast, hash, position, 1),
			agreementEventLock)
		s.testVotes(s.newVotes(types.VoteFastCom, hash, position, 1),
			agreementEventDecide)
		// Fast -> PreCom -> Com, confirmed by RBA.
		position.Height++
		s.testVotes(
			s.newVotes(types.VoteFast, hash, position, 1), agreementEventLock)
		s.testVotes(
			s.newVotes(types.VotePreCom, hash, position, 2), agreementEventLock)
		s.testVotes(
			s.newVotes(types.VoteCom, hash, position, 2), agreementEventDecide)
	}
	// The case for rounds before DKGDelayRound.
	test()
	// The case for rounds after DKGDelayRound.
	position.Round = 1
	position.Height = types.GenesisHeight + s.roundLength
	test()
}

func (s *AgreementCacheTestSuite) TestSnapshot() {
	var (
		hash  = common.NewRandomHash()
		count = s.requiredVotes - 1
	)
	process := func(vs []types.Vote) []types.Vote {
		for i := range vs[:count] {
			evts, err := s.cache.processVote(vs[i])
			s.Require().NoError(err)
			s.Require().Empty(evts)
		}
		return vs
	}
	// Process some votes without triggering any event.
	p0 := types.Position{Round: 0, Height: types.GenesisHeight}
	p1 := types.Position{Round: 0, Height: types.GenesisHeight + 1}
	p2 := types.Position{Round: 0, Height: types.GenesisHeight + 2}
	p3 := types.Position{Round: 0, Height: types.GenesisHeight + 3}
	// p0.
	process(s.newVotes(types.VoteFast, hash, p0, 0))
	process(s.newVotes(types.VoteCom, hash, p0, 1))
	process(s.newVotes(types.VotePreCom, hash, p0, 2))
	process(s.newVotes(types.VoteFastCom, hash, p0, 3))
	process(s.newVotes(types.VoteFast, hash, p0, 4))
	process(s.newVotes(types.VoteCom, hash, p0, 5))
	// p1, only pre-commit family votes.
	process(s.newVotes(types.VotePreCom, hash, p1, 3))
	process(s.newVotes(types.VoteFast, hash, p1, 5))
	// p2, only commit family votes.
	votesCom := s.newVotes(types.VoteCom, hash, p2, 6)
	process(votesCom)
	process(s.newVotes(types.VoteFastCom, hash, p2, 8))
	ss := s.cache.snapshot(time.Now())
	// Both pre-commit, commit family votes.
	r, votes := ss.get(p0, 1)
	s.Require().Nil(r)
	s.Require().Equal(count, countVotes(votes, p0, 1, types.VoteCom))
	s.Require().Equal(count, countVotes(votes, p0, 2, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p0, 3, types.VoteFastCom))
	s.Require().Equal(count, countVotes(votes, p0, 4, types.VoteFast))
	s.Require().Equal(count, countVotes(votes, p0, 5, types.VoteCom))
	// Only pre-commit family votes.
	r, votes = ss.get(p1, 3)
	s.Require().Nil(r)
	s.Require().Equal(0, countVotes(votes, p1, 3, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p1, 5, types.VoteFast))
	// Only commit family votes.
	r, votes = ss.get(p2, 10000)
	s.Require().Nil(r)
	s.Require().Equal(count, countVotes(votes, p2, 6, types.VoteCom))
	s.Require().Equal(count, countVotes(votes, p2, 8, types.VoteFastCom))
	// Trigger a decide event.
	evts, err := s.cache.processVote(votesCom[count])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].Position, p2)
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	r, votes = ss.get(p0, 1)
	s.Require().NotNil(r)
	s.Require().Empty(votes)
	r, votes = ss.get(p1, 3)
	s.Require().NotNil(r)
	s.Require().Empty(votes)
	r, votes = ss.get(p2, 10000)
	s.Require().NotNil(r)
	s.Require().Empty(votes)
	r, votes = ss.get(p3, 0)
	s.Require().Nil(r)
	s.Require().Empty(votes)
}

func (s *AgreementCacheTestSuite) TestPurgeByDecideEvent() {
	var (
		hash  = common.NewRandomHash()
		count = s.requiredVotes - 1
	)
	process := func(vs []types.Vote, until int) []types.Vote {
		for i := range vs[:until] {
			evts, err := s.cache.processVote(vs[i])
			s.Require().NoError(err)
			s.Require().Empty(evts)
		}
		return vs
	}
	p0 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	p1 := p0
	p1.Height++
	s.Require().True(p1.Round >= DKGDelayRound)
	p2 := p1
	p2.Height++
	// p0.
	process(s.newVotes(types.VotePreCom, hash, p0, 1), count)
	process(s.newVotes(types.VoteCom, hash, p0, 1), count)
	// p1.
	process(s.newVotes(types.VoteFast, hash, p1, 3), count)
	votesFC := process(s.newVotes(types.VoteFastCom, hash, p1, 1), count)
	// p2.
	process(s.newVotes(types.VotePreCom, hash, p2, 1), count)
	process(s.newVotes(types.VoteCom, hash, p2, 1), count)
	// Check current snapshot: all votes are exists, no decide event triggered.
	ss := s.cache.snapshot(time.Now())
	r, votes := ss.get(p0, 0)
	s.Require().Nil(r)
	s.Require().Len(votes, 2*count)
	s.Require().Equal(count, countVotes(votes, p0, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p0, 1, types.VoteCom))
	r, votes = ss.get(p1, 0)
	s.Require().Nil(r)
	s.Require().Len(votes, 2*count)
	s.Require().Equal(count, countVotes(votes, p1, 3, types.VoteFast))
	s.Require().Equal(count, countVotes(votes, p1, 1, types.VoteFastCom))
	r, votes = ss.get(p2, 0)
	s.Require().Nil(r)
	s.Require().Len(votes, 2*count)
	s.Require().Equal(count, countVotes(votes, p2, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p2, 1, types.VoteCom))
	// trigger a decide event.
	evts, err := s.cache.processVote(votesFC[count])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
	// All votes in older/equal position should be purged.
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	r, votes = ss.get(p0, 0)
	s.Require().NotNil(r)
	s.Require().Empty(votes)
	r, votes = ss.get(p1, 2)
	s.Require().NotNil(r)
	s.Require().Empty(votes)
	// All votes in later position should be kept.
	r, votes = ss.get(p2, 0)
	s.Require().Nil(r)
	s.Require().Equal(count, countVotes(votes, p2, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p2, 1, types.VoteCom))
}

func (s *AgreementCacheTestSuite) TestPrugeByLockEvent() {
	var (
		hash  = common.NewRandomHash()
		count = s.requiredVotes - 1
	)
	process := func(vs []types.Vote, until int) []types.Vote {
		for i := range vs[:until] {
			evts, err := s.cache.processVote(vs[i])
			s.Require().NoError(err)
			s.Require().Empty(evts)
		}
		return vs
	}
	p0 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	p1 := p0
	p1.Height++
	// There are some votes unable to trigger any signal.
	process(s.newVotes(types.VotePreCom, hash, p0, 1), count)
	process(s.newVotes(types.VoteCom, hash, p0, 1), count)
	process(s.newVotes(types.VotePreCom, hash, p1, 1), count)
	votesPreCom := process(s.newVotes(types.VotePreCom, hash, p1, 2), count)
	process(s.newVotes(types.VoteCom, hash, p1, 2), count)
	process(s.newVotes(types.VoteFast, hash, p1, 3), count)
	ss := s.cache.snapshot(time.Now())
	_, votes := ss.get(p0, 0)
	s.Require().Equal(count, countVotes(votes, p0, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p0, 1, types.VoteCom))
	_, votes = ss.get(p1, 0)
	s.Require().Len(votes, 4*count)
	// Receive some pre-commit votes position that can trigger locked event.
	evts, err := s.cache.processVote(votesPreCom[count])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventLock)
	s.Require().Equal(evts[0].Position, votesPreCom[count].Position)
	s.Require().Equal(evts[0].period, votesPreCom[count].Period)
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	r, votes := ss.get(p0, 0)
	s.Require().Nil(r)
	// We shouldn't purge commit votes in older position by locked event.
	s.Require().Equal(count, countVotes(votes, p0, 1, types.VoteCom))
	r, votes = ss.get(p1, 0)
	s.Require().Nil(r)
	s.Require().Len(votes, 3*count+1)
	// Those pre-commit/fast votes in older position should be purged.
	s.Require().Equal(0, countVotes(votes, p0, 1, types.VotePreCom))
	// Those pre-commit/fast votes in older period should be purged.
	s.Require().Equal(0, countVotes(votes, p1, 1, types.VotePreCom))
	// Those votes triggering events should be included.
	s.Require().Equal(count+1, countVotes(votes, p1, 2, types.VotePreCom))
	// Those pre-commit/fast votes in newer period should be included.
	s.Require().Equal(count, countVotes(votes, p1, 3, types.VoteFast))
	// We shouldn't purge commit votes by locked event.
	s.Require().Equal(count, countVotes(votes, p1, 2, types.VoteCom))
}

func (s *AgreementCacheTestSuite) TestPurgeByImpossibleToAgreeOnOneHash() {
	var (
		hash  = common.NewRandomHash()
		count = s.requiredVotes - 1
	)
	process := func(vs []types.Vote, until int) []types.Vote {
		for i := range vs[:until] {
			evts, err := s.cache.processVote(vs[i])
			s.Require().NoError(err)
			s.Require().Empty(evts)
		}
		return vs
	}
	// 2f votes for one hash.
	p0 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	process(s.newVotes(types.VotePreCom, hash, p0, 1), count)
	ss := s.cache.snapshot(time.Now())
	_, votes := ss.get(p0, 0)
	s.Require().Len(votes, count)
	// f+1 votes for another hash.
	hash = common.NewRandomHash()
	otherVotes := s.newVotes(types.VotePreCom, hash, p0, 1)
	process(otherVotes[count:], s.f+1)
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	_, votes = ss.get(p0, 0)
	s.Require().Empty(votes)
}

func (s *AgreementCacheTestSuite) TestIgnoredVotes() {
	isIgnored := func(v *types.Vote, ssTime time.Time) {
		evts, err := s.cache.processVote(*v)
		s.Require().NoError(err)
		s.Require().Empty(evts)
		ss := s.cache.snapshot(ssTime)
		_, votes := ss.get(types.Position{}, 0)
		s.Require().Empty(votes)
	}
	// Make sure the initial state is expected.
	_, found := s.cache.config(0)
	s.Require().True(found)
	c1, found := s.cache.config(1)
	s.Require().True(found)
	_, found = s.cache.config(2)
	s.Require().False(found)
	endH := types.GenesisHeight + s.roundLength*2
	s.Require().True(c1.Contains(endH - 1))
	s.Require().False(c1.Contains(endH))
	// Votes from unknown height should be ignored.
	h := common.NewRandomHash()
	p := types.Position{Round: 1, Height: endH}
	v := s.newVote(types.VoteCom, h, p, 1, s.signers[0])
	_, err := s.cache.processVote(*v)
	s.Require().EqualError(err, ErrUnknownHeight.Error())
	p = types.Position{Round: 0, Height: 0}
	v = s.newVote(types.VoteCom, h, p, 1, s.signers[0])
	isIgnored(v, time.Now())
	// Vote with type=init should be ignored.
	p = types.Position{Round: 0, Height: types.GenesisHeight}
	v = s.newVote(types.VoteInit, h, p, 1, s.signers[0])
	isIgnored(v, time.Now().Add(time.Second))
}

func (s *AgreementCacheTestSuite) TestAgreementSnapshotVotesIndex() {
	i0 := agreementSnapshotVotesIndex{period: 0}
	i1 := agreementSnapshotVotesIndex{period: 1}
	i2 := agreementSnapshotVotesIndex{period: 2}
	i3 := agreementSnapshotVotesIndex{period: 3}
	i4 := agreementSnapshotVotesIndex{period: 4}
	is := agreementSnapshotVotesIndexes{i0, i1, i2, i3, i4}
	s.Require().True(sort.SliceIsSorted(is, func(i, j int) bool {
		return is[i].period < is[j].period
	}))
	for i := range is[:len(is)-1] {
		iFound, found := is.nearestNewerIdx(is[i].period)
		s.Require().True(found)
		s.Require().Equal(iFound, is[i+1])
	}
	_, found := is.nearestNewerIdx(i4.period)
	s.Require().False(found)
}

func (s *AgreementCacheTestSuite) TestRandomly() {
	var (
		positionCount uint64 = 300
		periodCount   uint64 = 5
		iteration            = 3
	)
	lastPosition, vs, bs, rs := s.generateTestData(positionCount, periodCount,
		[]types.VoteType{
			types.VoteFast, types.VotePreCom, types.VoteCom, types.VoteFastCom,
		}, 3*s.f+1)
	s.Require().NotEmpty(vs)
	s.Require().NotEmpty(bs)
	s.Require().NotEmpty(rs)
	// Requirements in this scenario:
	// - no panic, error should be raised.
	// - the latest decide event should be identical the last position.
	// - events should be incremental by type.
	var lastEvts = make([]*agreementEvent, maxAgreementEventType)
	chk := func(evts []*agreementEvent, err error) {
		s.Require().NoError(err)
		for _, e := range evts {
			last := lastEvts[e.evtType]
			if last != nil {
				s.Require().True(e.Position.Newer(last.Position))
			}
			lastEvts[e.evtType] = e
		}
	}
	randObj := rand.New(rand.NewSource(time.Now().UnixNano()))
	for i := 0; i < iteration; i++ {
		// Shuffle those slices.
		randObj.Shuffle(len(vs), func(i, j int) { vs[i], vs[j] = vs[j], vs[i] })
		randObj.Shuffle(len(bs), func(i, j int) { bs[i], bs[j] = bs[j], bs[i] })
		randObj.Shuffle(len(rs), func(i, j int) { rs[i], rs[j] = rs[j], rs[i] })
		for i := range lastEvts {
			lastEvts[i] = nil
		}
		s.newCache(s.newRoundEvents(positionCount/s.roundLength - 1))
		var vIdx, bIdx, rIdx int
		for {
			switch randObj.Int() % 3 {
			case 0:
				if vIdx >= len(vs) {
					break
				}
				chk(s.cache.processVote(vs[vIdx]))
				vIdx++
			case 1:
				if bIdx >= len(bs) {
					break
				}
				chk(s.cache.processFinalizedBlock(bs[bIdx]))
				bIdx++
			case 2:
				if rIdx >= len(rs) {
					break
				}
				chk(s.cache.processAgreementResult(rs[rIdx]))
				rIdx++
			}
			if vIdx >= len(vs) && bIdx >= len(bs) && rIdx >= len(rs) {
				// Make sure we reach agreement on the last position.
				lastDecide := lastEvts[agreementEventDecide]
				s.Require().NotNil(lastDecide)
				s.Require().Equal(lastDecide.Position, lastPosition)
				break
			}
		}
	}
}

func TestAgreementCache(t *testing.T) {
	suite.Run(t, new(AgreementCacheTestSuite))
}

type generated struct {
	nodeCount                  int
	positionCount, periodCount uint64
	benchType                  string
	votes                      []types.Vote
	s                          *AgreementCacheTestSuite
}

func (g *generated) refresh(
	nodeCount int, positionCount, periodCount uint64, benchType string) {
	if g.nodeCount != nodeCount ||
		g.positionCount != positionCount ||
		g.periodCount != periodCount ||
		g.benchType != benchType {
		fmt.Printf(
			"generate votes, node:%d, pos:%d, period:%d, type:%s\n",
			nodeCount, positionCount, periodCount, benchType)
		s := new(AgreementCacheTestSuite)
		s.setupNodes(nodeCount)
		var (
			voteCountPerSet int
			voteTypes       []types.VoteType
		)
		switch benchType {
		case "vote-process":
			voteCountPerSet = s.requiredVotes
			voteTypes = []types.VoteType{types.VotePreCom, types.VoteCom}
		case "vote-process-no-trigger":
			voteCountPerSet = s.requiredVotes - 1
			voteTypes = []types.VoteType{types.VotePreCom, types.VoteCom}
		case "snapshot":
			// No event trigger expected, no votes is purged, that the worst
			// case when taking snapshot.
			voteCountPerSet = s.requiredVotes - 1
			voteTypes = []types.VoteType{
				types.VoteFast,
				types.VotePreCom,
				types.VoteCom,
				types.VoteFastCom}
		default:
			panic(fmt.Errorf("unknown bench-type: %s", benchType))
		}
		_, vs, _, _ := s.generateTestData(positionCount, periodCount,
			voteTypes, voteCountPerSet)
		fmt.Printf(
			"generate votes, node:%d, pos:%d, period:%d, type:%s, done with %d votes\n",
			nodeCount, positionCount, periodCount, benchType, len(vs))
		generatedCache = generated{
			nodeCount:     nodeCount,
			positionCount: positionCount,
			periodCount:   periodCount,
			benchType:     benchType,
			votes:         vs,
			s:             s}
	}
}

var generatedCache generated

func benchmarkAgreementCacheVoteProcess(
	b *testing.B, nodeCount int, positionCount uint64, benchType string) {
	generatedCache.refresh(nodeCount, positionCount, 1, benchType)
	s := generatedCache.s
	s.newCache(s.newRoundEvents(positionCount / s.roundLength))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if i >= len(generatedCache.votes) {
			break
		}
		if _, err := s.cache.processVote(generatedCache.votes[i]); err != nil {
			panic(err)
		}
	}
}

func BenchmarkAgreementCacheVoteProcess_49_200(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 49, 200, "vote-process")
}

func BenchmarkAgreementCacheVoteProcess_199_200(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 199, 200, "vote-process")
}

func BenchmarkAgreementCacheVoteProcess_301_300(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 301, 300, "vote-process")
}

func BenchmarkAgreementCacheVoteProcess_400_300(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 401, 300, "vote-process")
}

func BenchmarkAgreementCacheVoteProcess_49_200_NoTrigger(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 49, 200, "vote-process-no-trigger")
}

func BenchmarkAgreementCacheVoteProcess_199_200_NoTrigger(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 199, 200, "vote-process-no-trigger")
}

func BenchmarkAgreementCacheVoteProcess_301_300_NoTrigger(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 301, 300, "vote-process-no-trigger")
}

func BenchmarkAgreementCacheVoteProcess_400_300_NoTrigger(b *testing.B) {
	benchmarkAgreementCacheVoteProcess(b, 401, 300, "vote-process-no-trigger")
}

func benchmarkAgreementCacheSnapshot(
	b *testing.B, nodeCount int, positionCount, periodCount uint64) {
	generatedCache.refresh(nodeCount, positionCount, periodCount, "snapshot")
	s := generatedCache.s
	s.newCache(s.newRoundEvents(positionCount / s.roundLength))
	for _, v := range generatedCache.votes {
		evts, err := s.cache.processVote(v)
		if len(evts) > 0 {
			panic(fmt.Errorf("trigger event: %s %s", evts[0], &v))
		}
		if err != nil {
			panic(err)
		}
	}
	b.ResetTimer()
	t := time.Now()
	for i := 0; i < b.N; i++ {
		s.cache.snapshot(t)
		t = t.Add(s.lambdaBA + time.Second)
	}
}

func BenchmarkAgreementCacheSnapshot_49_30_2(b *testing.B) {
	benchmarkAgreementCacheSnapshot(b, 49, 30, 2)
}

func BenchmarkAgreementCacheSnapshot_199_30_3(b *testing.B) {
	benchmarkAgreementCacheSnapshot(b, 199, 30, 2)
}

func BenchmarkAgreementCacheSnapshot_301_30_3(b *testing.B) {
	benchmarkAgreementCacheSnapshot(b, 301, 30, 3)
}

func BenchmarkAgreementCacheSnapshot_400_40_4(b *testing.B) {
	benchmarkAgreementCacheSnapshot(b, 401, 40, 4)
}
