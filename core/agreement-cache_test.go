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
	signers       []*utils.Signer
	cache         *agreementCache
	recv          *testAgreementCacheReceiver
}

func (s *AgreementCacheTestSuite) SetupSuite() {
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
	s.roundLength = 100
	s.notarySet = make(map[types.NodeID]struct{})
	for _, k := range pubKeys {
		s.notarySet[types.NewNodeID(k)] = struct{}{}
	}
	s.recv = &testAgreementCacheReceiver{s: s}
}

func (s *AgreementCacheTestSuite) SetupTest() {
	s.recv.isTsigValid = true
	s.newCache(s.newRoundEvents(1))
}

func (s *AgreementCacheTestSuite) newVote(t types.VoteType, h common.Hash,
	p types.Position, period uint64, signer *utils.Signer) *types.Vote {
	v := types.NewVote(t, h, period)
	v.Position = p
	s.Require().NoError(signer.SignVote(v))
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

func (s *AgreementCacheTestSuite) takeSubset(ss *agreementSnapshot,
	p types.Position, period uint64, length int) (
	*types.AgreementResult, []types.Vote) {
	s.Require().NotNil(ss)
	r, votes := ss.get(p, period)
	s.Require().Len(votes, length)
	return r, votes
}

func (s *AgreementCacheTestSuite) newRoundEvents(
	round uint64) (rEvts []utils.RoundEventParam) {
	h := types.GenesisHeight
	for r := uint64(0); r <= round; r++ {
		rEvts = append(rEvts, utils.RoundEventParam{
			Round:       r,
			BeginHeight: h,
			Config: &types.Config{
				LambdaBA:    100 * time.Millisecond,
				RoundLength: s.roundLength,
			},
		})
		h += s.roundLength
	}
	return
}

func (s *AgreementCacheTestSuite) newCache(rEvts []utils.RoundEventParam) {
	s.cache = newAgreementCache(s.recv)
	s.Require().NotNil(s.cache)
	s.Require().NoError(s.cache.notifyRoundEvents(rEvts))
}

func (s *AgreementCacheTestSuite) generateTestData(
	positionCount, periodCount uint64) (
	lastPosition types.Position,
	votes []types.Vote, bs []*types.Block, rs []*types.AgreementResult) {
	gen := func(p types.Position, period uint64) (
		vs []types.Vote, b *types.Block, r *types.AgreementResult) {
		h := common.NewRandomHash()
		v := s.newVotes(types.VotePreCom, h, p, period)
		vs = append(vs, v...)
		vCom := s.newVotes(types.VoteCom, h, p, period)
		vs = append(vs, vCom...)
		if period == 0 {
			v = s.newVotes(types.VoteFast, h, p, period)
			vs = append(vs, v...)
			v = s.newVotes(types.VoteFastCom, h, p, period)
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
	process := func(vs []types.Vote, until int) []types.Vote {
		for i := range vs[:until] {
			evts, err := s.cache.processVote(vs[i])
			s.Require().NoError(err)
			s.Require().Empty(evts)
		}
		return vs
	}
	// Process some votes without triggering any event.
	//
	// Below is the expected slice of votes when taking snapshot.
	// |<-      pre-commit/fast    ->|<-commit/fast-commit ->|
	// |P0,r1|P1,r0|P1,r1|P1,r2|P2,r1|P2,r1|P1,r2|P1,r0|P0,r1|
	p0 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	process(s.newVotes(types.VoteFast, hash, p0, 1), count)
	process(s.newVotes(types.VotePreCom, hash, p0, 1), count)
	process(s.newVotes(types.VoteCom, hash, p0, 1), count)
	process(s.newVotes(types.VoteFastCom, hash, p0, 1), count)
	p1 := p0
	p1.Height++
	process(s.newVotes(types.VoteCom, hash, p1, 0), count)
	process(s.newVotes(types.VoteFastCom, hash, p1, 0), count)
	process(s.newVotes(types.VotePreCom, hash, p1, 0), count)
	process(s.newVotes(types.VoteFast, hash, p1, 0), count)
	process(s.newVotes(types.VoteFast, hash, p1, 1), count)
	process(s.newVotes(types.VotePreCom, hash, p1, 1), count)
	process(s.newVotes(types.VoteFast, hash, p1, 2), count)
	process(s.newVotes(types.VotePreCom, hash, p1, 2), count)
	process(s.newVotes(types.VoteCom, hash, p1, 2), count)
	process(s.newVotes(types.VoteFastCom, hash, p1, 2), count)
	p2 := p1
	p2.Height++
	process(s.newVotes(types.VoteFast, hash, p2, 1), count)
	process(s.newVotes(types.VotePreCom, hash, p2, 1), count)
	process(s.newVotes(types.VoteCom, hash, p2, 1), count)
	process(s.newVotes(types.VoteFastCom, hash, p2, 1), count)
	// The snapshot should contains all processed votes.
	ss := s.cache.snapshot(time.Now())
	s.takeSubset(ss, types.Position{}, 0, 18*count)
	// all votes in older position would be igonred.
	_, vs := s.takeSubset(ss, p1, 1, 10*count)
	s.Require().Equal(0, countVotes(vs, p0, 1, types.VotePreCom))
	s.Require().Equal(0, countVotes(vs, p0, 1, types.VoteFast))
	s.Require().Equal(0, countVotes(vs, p0, 1, types.VoteCom))
	s.Require().Equal(0, countVotes(vs, p0, 1, types.VoteFastCom))
	// pre-commit/fast votes in older or equal period of the same position would
	// be ignored.
	s.Require().Equal(0, countVotes(vs, p1, 0, types.VotePreCom))
	s.Require().Equal(0, countVotes(vs, p1, 0, types.VoteFast))
	s.Require().Equal(0, countVotes(vs, p1, 1, types.VoteCom))
	s.Require().Equal(0, countVotes(vs, p1, 1, types.VoteFastCom))
	// pre-commit/fast votes in newer period of the same position would not be
	// ignored.
	s.Require().Equal(count, countVotes(vs, p1, 2, types.VotePreCom))
	s.Require().Equal(count, countVotes(vs, p1, 2, types.VoteFast))
	// commit/fast-commit votes in the same position can't be ignored.
	s.Require().Equal(count, countVotes(vs, p1, 0, types.VoteCom))
	s.Require().Equal(count, countVotes(vs, p1, 0, types.VoteFastCom))
	s.Require().Equal(count, countVotes(vs, p1, 2, types.VoteCom))
	s.Require().Equal(count, countVotes(vs, p1, 2, types.VoteFastCom))
	// all votes in newer position would be kept.
	s.Require().Equal(count, countVotes(vs, p2, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(vs, p2, 1, types.VoteFast))
	s.Require().Equal(count, countVotes(vs, p2, 1, types.VoteCom))
	s.Require().Equal(count, countVotes(vs, p2, 1, types.VoteFastCom))
	// Take an empty subset.
	s.takeSubset(ss, types.Position{Round: 1, Height: 1000}, 0, 0)
	// Take a subset contains only commit/fast-commit votes.
	_, vs = s.takeSubset(ss, p2, 1, 2*count)
	s.Require().Equal(0, countVotes(vs, p2, 1, types.VotePreCom))
	s.Require().Equal(0, countVotes(vs, p2, 1, types.VoteFast))
	s.Require().Equal(count, countVotes(vs, p2, 1, types.VoteCom))
	s.Require().Equal(count, countVotes(vs, p2, 1, types.VoteFastCom))
	// Taks a subset contains only pre-commit/fast votes.
	p3 := p2
	p3.Height++
	process(s.newVotes(types.VotePreCom, hash, p3, 1), count)
	process(s.newVotes(types.VoteFast, hash, p3, 1), count)
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	_, vs = s.takeSubset(ss, p3, 0, 2*count)
	s.Require().Equal(count, countVotes(vs, p3, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(vs, p3, 1, types.VoteFast))
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
	// There are some pre-commit/fast votes unable to trigger any signal.
	p00 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	process(s.newVotes(types.VotePreCom, hash, p00, 1), count)
	process(s.newVotes(types.VoteFast, hash, p00, 1), count)
	// There are some commit/fast-commit votes unable to trigger any signal.
	process(s.newVotes(types.VoteCom, hash, p00, 1), count)
	process(s.newVotes(types.VoteFastCom, hash, p00, 1), count)
	// There are some pre-commit/fast votes at later position unable to trigger
	// any signal.
	p01 := p00
	p01.Height++
	s.Require().True(p01.Round >= DKGDelayRound)
	process(s.newVotes(types.VotePreCom, hash, p01, 3), count)
	process(s.newVotes(types.VoteFast, hash, p01, 3), count)
	// There are some commit/fast-commit votes at later position unable to
	// trigger any signal.
	process(s.newVotes(types.VoteCom, hash, p01, 1), count)
	votesFC := process(s.newVotes(types.VoteFastCom, hash, p01, 1), count)
	// There are some pre-commit/fast votes at the newest position unable to
	// trigger any signal.
	p02 := p01
	p02.Height++
	process(s.newVotes(types.VotePreCom, hash, p02, 1), count)
	process(s.newVotes(types.VoteFast, hash, p02, 1), count)
	// There are some commit/fast-commit votes at the newest position unable to
	// trigger any signal.
	process(s.newVotes(types.VoteCom, hash, p02, 1), count)
	process(s.newVotes(types.VoteFastCom, hash, p02, 1), count)
	// Check current snapshot: all votes are exists, no decide event triggered.
	ss := s.cache.snapshot(time.Now())
	r, _ := s.takeSubset(ss, types.Position{}, 0, 12*count)
	s.Require().Nil(r)
	// We receive some commit votes position that can trigger some decide event,
	// then those votes should be purged.
	evts, err := s.cache.processVote(votesFC[count])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventDecide)
	// All votes in older/equal position should be purged.
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	r, votes := s.takeSubset(ss, types.Position{}, 0, 4*count)
	s.Require().NotNil(r)
	s.Require().Equal(r.Position, p01)
	s.Require().NotEmpty(r.Randomness)
	// All votes in later position should be kept.
	s.Require().Equal(count, countVotes(votes, p02, 1, types.VotePreCom))
	s.Require().Equal(count, countVotes(votes, p02, 1, types.VoteFast))
	s.Require().Equal(count, countVotes(votes, p02, 1, types.VoteCom))
	s.Require().Equal(count, countVotes(votes, p02, 1, types.VoteFastCom))
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
	// There are some votes unable to trigger any signal.
	p00 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	process(s.newVotes(types.VoteFast, hash, p00, 1), count)
	process(s.newVotes(types.VotePreCom, hash, p00, 1), count)
	process(s.newVotes(types.VoteFastCom, hash, p00, 1), count)
	process(s.newVotes(types.VoteCom, hash, p00, 1), count)
	p01 := p00
	p01.Height++
	process(s.newVotes(types.VoteFast, hash, p01, 0), count)
	votes := process(s.newVotes(types.VotePreCom, hash, p01, 1), count)
	process(s.newVotes(types.VoteCom, hash, p01, 1), count)
	process(s.newVotes(types.VotePreCom, hash, p01, 2), count)
	ss := s.cache.snapshot(time.Now())
	s.takeSubset(ss, types.Position{}, 0, 8*count)
	// Receive some pre-commit votes position that can trigger locked event.
	evts, err := s.cache.processVote(votes[count])
	s.Require().NoError(err)
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0].evtType, agreementEventLock)
	s.Require().Equal(evts[0].Position, votes[count].Position)
	s.Require().Equal(evts[0].period, votes[count].Period)
	ss = s.cache.snapshot(time.Now().Add(time.Second))
	r, votes := s.takeSubset(ss, types.Position{}, 0, 5*count+1)
	s.Require().Nil(r)
	// Those pre-commit/fast votes in older position should be purged.
	s.Require().Equal(0, countVotes(votes, p00, 1, types.VoteFast))
	s.Require().Equal(0, countVotes(votes, p00, 1, types.VotePreCom))
	// Those pre-commit/fast votes in older period should be purged.
	s.Require().Equal(0, countVotes(votes, p01, 0, types.VoteFast))
	// Those pre-commit/fast votes in newer period should be included.
	s.Require().Equal(count, countVotes(votes, p01, 2, types.VotePreCom))
	// Those votes triggering events should be included.
	s.Require().Equal(count+1, countVotes(votes, p01, 1, types.VotePreCom))
	// We shouldn't purge commit votes by locked event.
	s.Require().Equal(count, countVotes(votes, p00, 1, types.VoteCom))
	s.Require().Equal(count, countVotes(votes, p00, 1, types.VoteFastCom))
	s.Require().Equal(count, countVotes(votes, p01, 1, types.VoteCom))
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
	p00 := types.Position{Round: 1, Height: types.GenesisHeight + s.roundLength}
	process(s.newVotes(types.VotePreCom, hash, p00, 1), count)
	s.takeSubset(s.cache.snapshot(time.Now()), types.Position{}, 0, count)
	// f+1 votes for another hash.
	hash = common.NewRandomHash()
	otherVotes := s.newVotes(types.VotePreCom, hash, p00, 1)
	process(otherVotes[count:], s.f+1)
	s.takeSubset(
		s.cache.snapshot(time.Now().Add(time.Second)), types.Position{}, 0, 0)
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
	i0 := agreementSnapshotVotesIndex{
		position: types.Position{Round: 0, Height: types.GenesisHeight},
		period:   0}
	i1 := agreementSnapshotVotesIndex{
		position: types.Position{Round: 0, Height: types.GenesisHeight},
		period:   1}
	i2 := agreementSnapshotVotesIndex{
		position: types.Position{Round: 0, Height: types.GenesisHeight + 1},
		period:   0}
	i3 := agreementSnapshotVotesIndex{
		position: types.Position{Round: 1, Height: types.GenesisHeight},
		period:   0}
	i4 := agreementSnapshotVotesIndex{
		position: types.Position{Round: 1, Height: types.GenesisHeight},
		period:   2}
	is := agreementSnapshotVotesIndexes{i0, i1, i2, i3, i4}
	s.Require().True(sort.SliceIsSorted(is, func(i, j int) bool {
		return !is[i].Newer(is[j])
	}))
	for i := range is[:len(is)-1] {
		s.Require().True(is[i].Older(is[i+1]))
	}
	for i := range is[:len(is)-1] {
		s.Require().True(is[i+1].Newer(is[i]))
	}
	for _, i := range is {
		s.Require().False(i.Older(i))
		s.Require().False(i.Newer(i))
	}
	for i := range is[:len(is)-1] {
		iFound, found := is.nearestNewerIdx(is[i].position, is[i].period)
		s.Require().True(found)
		s.Require().Equal(iFound, is[i+1])
	}
	_, found := is.nearestNewerIdx(i4.position, i4.period)
	s.Require().False(found)
	_, found = is.nearestNewerOrEqualIdx(i4.position, i4.period)
	s.Require().True(found)
}

func (s *AgreementCacheTestSuite) TestAgreementSnapshot() {
	var (
		h     = common.NewRandomHash()
		count = s.requiredVotes - 1
	)
	newVotes := func(t types.VoteType, h common.Hash, p types.Position,
		period uint64) []types.Vote {
		votes := s.newVotes(t, h, p, period)
		return votes[:count]
	}
	ss := agreementSnapshot{}
	// |<-pre-commit      ->|<- commit         ->|
	// | P0,1 | P2,2 | P3,1 | P2,2 | P2,1 | P1,0 |
	p0 := types.Position{Round: 0, Height: types.GenesisHeight}
	p1 := types.Position{Round: 0, Height: types.GenesisHeight + 1}
	p2 := types.Position{Round: 0, Height: types.GenesisHeight + 2}
	p3 := types.Position{Round: 0, Height: types.GenesisHeight + 3}
	p4 := types.Position{Round: 0, Height: types.GenesisHeight + 4}
	ss.addPreCommitVotes(p0, 1, newVotes(types.VotePreCom, h, p0, 1))
	ss.addPreCommitVotes(p2, 2, newVotes(types.VoteFast, h, p1, 2))
	ss.addPreCommitVotes(p3, 1, newVotes(types.VoteFast, h, p3, 1))
	// Add commit/fast-common votes in reversed order.
	ss.markBoundary()
	ss.addCommitVotes(p2, 2, newVotes(types.VoteCom, h, p2, 2))
	ss.addCommitVotes(p2, 1, newVotes(types.VoteFastCom, h, p2, 1))
	ss.addCommitVotes(p1, 0, newVotes(types.VoteCom, h, p1, 0))
	// Get with a very new position, should return empty votes.
	r, votes := ss.get(types.Position{Round: 0, Height: 10}, 0)
	s.Require().Nil(r)
	s.Require().Empty(votes)
	// Get with a very old position, should return all votes.
	_, votes = ss.get(types.Position{}, 0)
	s.Require().Nil(r)
	s.Require().Len(votes, 6*count)
	// Only the newest pre-commit votes returns.
	_, votes = ss.get(p3, 0)
	s.Require().Len(votes, count)
	s.Require().Equal(count, countVotes(votes, p3, 1, types.VoteFast))
	// pre-commit votes in the same position and period is ignored, commit votes
	// in the same position and period is included.
	_, votes = ss.get(p2, 2)
	s.Require().Len(votes, 3*count)
	s.Require().Equal(count, countVotes(votes, p3, 1, types.VoteFast))
	s.Require().Equal(count, countVotes(votes, p2, 1, types.VoteFastCom))
	s.Require().Equal(count, countVotes(votes, p2, 2, types.VoteCom))
	// Only the newest commit votes is included.
	ss = agreementSnapshot{}
	ss.addPreCommitVotes(p0, 1, newVotes(types.VotePreCom, h, p0, 1))
	ss.markBoundary()
	ss.addCommitVotes(p4, 1, newVotes(types.VoteFastCom, h, p4, 1))
	_, votes = ss.get(p4, 10)
	s.Require().Len(votes, count)
	s.Require().Equal(count, countVotes(votes, p4, 1, types.VoteFastCom))
}

func (s *AgreementCacheTestSuite) TestRandomly() {
	var (
		positionCount uint64 = 300
		periodCount   uint64 = 5
		iteration            = 3
	)
	lastPosition, vs, bs, rs := s.generateTestData(positionCount, periodCount)
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
		randObj.Shuffle(len(vs), func(i, j int) { vs[i], vs[i] = vs[j], vs[i] })
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
