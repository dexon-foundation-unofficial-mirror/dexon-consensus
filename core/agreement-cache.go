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
	"errors"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors raised from agreementCache.
var (
	ErrUnableToAgree = errors.New("unable to agree")
	ErrUnknownHeight = errors.New("unknown height")
)

// voteSet is a place to keep one type of votes for one period in one position.
type voteSet struct {
	votes          map[types.NodeID]types.Vote
	counts         map[common.Hash]int
	best           common.Hash
	votesAsList    []types.Vote
	triggeredEvent *agreementEvent
	purged         bool
}

func newVoteSet(size int) *voteSet {
	return &voteSet{
		votes:       make(map[types.NodeID]types.Vote, size),
		votesAsList: make([]types.Vote, 0, size),
		counts:      make(map[common.Hash]int),
	}
}

func (s *voteSet) add(v types.Vote) (oldV types.Vote, forked bool) {
	if s.purged {
		return
	}
	oldV, exist := s.votes[v.ProposerID]
	if exist {
		forked = oldV.BlockHash != v.BlockHash
		return
	}
	s.votes[v.ProposerID] = v
	s.votesAsList = append(s.votesAsList, v)
	s.counts[v.BlockHash]++
	if s.counts[v.BlockHash] > s.counts[s.best] {
		s.best = v.BlockHash
	}
	return
}

func (s *voteSet) votesByHash(h common.Hash, estimatedLength int) []types.Vote {
	votes := make([]types.Vote, 0, estimatedLength)
	for _, v := range s.votesAsList {
		if v.BlockHash == h {
			votes = append(votes, v)
		}
	}
	return votes
}

func (s *voteSet) isAgreedOnOneHashPossible(maxCount, threshold int) bool {
	if len(s.votes) == maxCount {
		return s.counts[s.best] >= threshold
	} else if len(s.votes) > maxCount {
		panic(fmt.Errorf("collecting votes exceeding max count: %v %v %v",
			maxCount, threshold, len(s.votes)))
	}
	return maxCount-len(s.votes)+s.counts[s.best] >= threshold
}

func (s *voteSet) appendTo(src []types.Vote) []types.Vote {
	if len(s.votesAsList) == 0 {
		if s.triggeredEvent == nil {
			return src
		}
		return append(src, s.triggeredEvent.Votes...)
	}
	return append(src, s.votesAsList...)
}

func (s *voteSet) setEvent(e *agreementEvent, purge bool) {
	if purge {
		s.votes = nil
		s.counts = nil
		s.votesAsList = nil
		s.purged = true
	}
	s.triggeredEvent = e
}

func (s *voteSet) isPurged() bool {
	return s.purged
}

type agreementCacheConfig struct {
	utils.RoundBasedConfig

	notarySet map[types.NodeID]struct{}
	lambdaBA  time.Duration
}

func (c *agreementCacheConfig) from(
	round uint64,
	config *types.Config,
	notarySet map[types.NodeID]struct{}) {
	c.SetupRoundBasedFields(round, config)
	c.lambdaBA = config.LambdaBA
	c.notarySet = notarySet
}

func newAgreementCacheConfig(
	prev agreementCacheConfig,
	config *types.Config,
	notarySet map[types.NodeID]struct{}) (c agreementCacheConfig) {
	c = agreementCacheConfig{}
	c.from(prev.RoundID()+1, config, notarySet)
	c.AppendTo(prev.RoundBasedConfig)
	return
}

type agreementSnapshotVotesIndex struct {
	position types.Position
	period   uint64
	idx      int
}

func (x agreementSnapshotVotesIndex) Older(o agreementSnapshotVotesIndex) bool {
	return x.position.Older(o.position) ||
		(x.position.Equal(o.position) && x.period < o.period)
}

func (x agreementSnapshotVotesIndex) Newer(o agreementSnapshotVotesIndex) bool {
	return x.position.Newer(o.position) ||
		(x.position.Equal(o.position) && x.period > o.period)
}

type agreementSnapshotVotesIndexes []agreementSnapshotVotesIndex

func (x agreementSnapshotVotesIndexes) nearestNewerIdx(
	position types.Position,
	period uint64) (agreementSnapshotVotesIndex, bool) {
	fakeIdx := agreementSnapshotVotesIndex{position: position, period: period}
	i := sort.Search(len(x), func(i int) bool {
		return x[i].Newer(fakeIdx)
	})
	if i < len(x) {
		return x[i], true
	}
	return agreementSnapshotVotesIndex{}, false
}

func (x agreementSnapshotVotesIndexes) nearestNewerOrEqualIdx(
	position types.Position,
	period uint64) (agreementSnapshotVotesIndex, bool) {
	fakeIdx := agreementSnapshotVotesIndex{position: position, period: period}
	i := sort.Search(len(x), func(i int) bool {
		return !x[i].Older(fakeIdx)
	})
	if i < len(x) {
		return x[i], true
	}
	return agreementSnapshotVotesIndex{}, false
}

// agreementSnapshot represents a group of ongoing votes that could be pulled
// to help others nodes to reach consensus. It supports to take a subset of
// snapshoted votes by providing the latest locked position P and period r.
// (NOTE: assume P-1 is already decided).
//
// When a subset request (P, r) is made, all pre-commit/fast votes later than
// that position would be returned. And all fast-commit/commit votes later than
// (P, 0) would be returned, too. To support this kind of access, the "votes"
// slice in snapshot is splited into two parts and can be viewed as this:
//
// |<-      pre-commit/fast    ->|<-      commit/fast-commit ->|
// |P0,r0|P1,r1|P1,r2|P2,r1|P3,r1|P2,r2|P2,r1|P1,r2|P1,r0|P0,r0|
//
// When a pull request (P1,r2) is made, the slots between (P2,r1)->(P3,r1) in
// pre-commit/fast part and slots between (P1,r0)->(P2,r2) in
// commit/fast-commit part would be taken, and they are continuous in the slice,
// thus we don't need to recreate a new slice for pulling.
type agreementSnapshot struct {
	expired     time.Time
	votes       []types.Vote
	evtDecide   *agreementEvent
	preComIdxes agreementSnapshotVotesIndexes // pre-commit/fast parts.
	comIdxes    agreementSnapshotVotesIndexes // commit/fast-commit parts.
	boundary    int                           // middle line between parts.
}

func (s agreementSnapshot) get(position types.Position, lockPeriod uint64) (
	r *types.AgreementResult, votes []types.Vote) {
	if s.evtDecide != nil && s.evtDecide.Position.Newer(position) {
		result := s.evtDecide.toAgreementResult()
		r = &result
	}
	begin, end := s.boundary, s.boundary
	if i, found := s.preComIdxes.nearestNewerIdx(position, lockPeriod); found {
		begin = i.idx
	}
	if i, found := s.comIdxes.nearestNewerOrEqualIdx(position, 0); found {
		end = i.idx
	}
	votes = s.votes[begin:end]
	return
}

func (s *agreementSnapshot) addPreCommitVotes(
	position types.Position, period uint64, votes []types.Vote) {
	if len(votes) == 0 {
		return
	}
	i := agreementSnapshotVotesIndex{
		position: position,
		period:   period,
	}
	if len(s.preComIdxes) > 0 && !s.preComIdxes[len(s.preComIdxes)-1].Older(i) {
		panic(fmt.Errorf(
			"invalid snapshot index additional in pre-commit case: %+v,%+v",
			i, s.preComIdxes[len(s.preComIdxes)-1]))
	}
	// In pre-commit case, we would record the index of the first vote.
	i.idx = len(s.votes)
	s.votes = append(s.votes, votes...)
	if len(s.votes) == i.idx {
		return
	}
	s.preComIdxes = append(s.preComIdxes, i)
}

func (s *agreementSnapshot) addCommitVotes(
	position types.Position, period uint64, votes []types.Vote) {
	if len(votes) == 0 {
		return
	}
	i := agreementSnapshotVotesIndex{
		position: position,
		period:   period,
	}
	if len(s.comIdxes) > 0 && !s.comIdxes[len(s.comIdxes)-1].Newer(i) {
		panic(fmt.Errorf(
			"invalid snapshot index additional in commit case: %+v,%+v",
			i, s.comIdxes[len(s.comIdxes)-1]))
	}
	// In commit case, we would record the index of the last vote.
	beforeAppend := len(s.votes)
	s.votes = append(s.votes, votes...)
	if len(s.votes) == beforeAppend {
		return
	}
	i.idx = len(s.votes)
	s.comIdxes = append(agreementSnapshotVotesIndexes{i}, s.comIdxes...)
}

func (s *agreementSnapshot) markBoundary() {
	if len(s.comIdxes) > 0 {
		panic(fmt.Errorf("attempt to mark boundary after commit votes added"))
	}
	s.boundary = len(s.votes)
}

type agreementCacheReceiver interface {
	GetNotarySet(round uint64) (map[types.NodeID]struct{}, error)
	RecoverTSIG(blockHash common.Hash, votes []types.Vote) ([]byte, error)
	VerifyTSIG(round uint64, hash common.Hash, tSig []byte) (bool, error)
}

type agreementCache struct {
	recv           agreementCacheReceiver
	configs        []agreementCacheConfig
	configsLock    sync.RWMutex
	lock           sync.RWMutex
	vSets          map[types.Position]map[uint64][]*voteSet // Position > Period > Type
	refEvts        []*agreementEvent
	refPosition    atomic.Value
	lastSnapshot   atomic.Value
	lastSnapshotCh chan time.Time
}

func (c *agreementCache) votes(v types.Vote, config agreementCacheConfig,
	createIfNotExist bool) *voteSet {
	vForPosition, exist := c.vSets[v.Position]
	if !exist {
		if !createIfNotExist {
			return nil
		}
		vForPosition = make(map[uint64][]*voteSet)
		c.vSets[v.Position] = vForPosition
	}
	vForPeriod, exist := vForPosition[v.Period]
	if !exist {
		if !createIfNotExist {
			return nil
		}
		reserved := len(config.notarySet)
		vForPeriod = make([]*voteSet, types.MaxVoteType)
		for idx := range vForPeriod {
			if types.VoteType(idx) == types.VoteInit {
				continue
			}
			vForPeriod[idx] = &voteSet{
				votes:       make(map[types.NodeID]types.Vote, reserved),
				votesAsList: make([]types.Vote, 0, reserved),
				counts:      make(map[common.Hash]int),
			}
		}
		vForPosition[v.Period] = vForPeriod
	}
	return vForPeriod[v.Type]
}

func (c *agreementCache) config(
	round uint64) (cfg agreementCacheConfig, found bool) {
	c.configsLock.RLock()
	defer c.configsLock.RUnlock()
	if len(c.configs) == 0 {
		return
	}
	firstRound := c.configs[0].RoundID()
	if round < firstRound || round >= firstRound+uint64(len(c.configs)) {
		return
	}
	cfg, found = c.configs[round-firstRound], true
	return
}

func (c *agreementCache) firstConfig() (cfg agreementCacheConfig, found bool) {
	c.configsLock.RLock()
	defer c.configsLock.RUnlock()
	if len(c.configs) == 0 {
		return
	}
	return c.configs[0], true
}

// isIgnorable is the most trivial way to filter outdated messages.
func (c *agreementCache) isIgnorable(p types.Position) bool {
	return !p.Newer(c.refPosition.Load().(types.Position))
}

func (c *agreementCache) isIgnorableNoLock(p types.Position) bool {
	evtDecide := c.refEvts[agreementEventDecide]
	if evtDecide == nil {
		return false
	}
	return !p.Newer(evtDecide.Position)
}

func (c *agreementCache) isVoteIgnorable(v types.Vote) bool {
	if v.Type == types.VoteInit {
		return true
	}
	if c.isIgnorableNoLock(v.Position) {
		return true
	}
	switch v.Type {
	case types.VotePreCom, types.VoteFast:
		evtLock := c.refEvts[agreementEventLock]
		if evtLock == nil || evtLock.Position.Older(v.Position) {
			return false
		}
		if evtLock.Position.Newer(v.Position) {
			return true
		}
		return evtLock.period >= v.Period
	}
	return false
}

func (c *agreementCache) checkVote(
	v types.Vote, config agreementCacheConfig) (bool, *agreementEvent, error) {
	if v.Type >= types.MaxVoteType {
		return false, nil, ErrInvalidVote
	}
	if !config.Contains(v.Position.Height) {
		return false, nil, ErrUnknownHeight
	}
	ok, err := utils.VerifyVoteSignature(&v)
	if err != nil {
		return false, nil, err
	}
	if !ok {
		return false, nil, ErrIncorrectVoteSignature
	}
	c.lock.RLock()
	defer c.lock.RUnlock()
	if c.isVoteIgnorable(v) {
		return false, nil, nil
	}
	if _, exist := config.notarySet[v.ProposerID]; !exist {
		return false, nil, ErrNotInNotarySet
	}
	// Check for forked votes.
	//
	// NOTE: we won't be able to detect forked votes if they are purged.
	if vSet := c.votes(v, config, false); vSet != nil {
		if oldV, exist := vSet.votes[v.ProposerID]; exist {
			if v.BlockHash != oldV.BlockHash {
				return false, newAgreementEvent(
					agreementEventFork, []types.Vote{oldV, v}), nil
			}
		}
	}
	return true, nil, nil
}

func (c *agreementCache) checkResult(r *types.AgreementResult,
	config agreementCacheConfig) (bool, error) {
	if r.Position.Round < DKGDelayRound {
		if err := VerifyAgreementResult(r, config.notarySet); err != nil {
			return false, err
		}
		if bytes.Compare(r.Randomness, NoRand) != 0 {
			return false, ErrIncorrectAgreementResult
		}
	} else {
		ok, err := c.recv.VerifyTSIG(r.Position.Round, r.BlockHash, r.Randomness)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, ErrIncorrectBlockRandomness
		}
	}
	c.lock.RLock()
	defer c.lock.RUnlock()
	if c.isIgnorableNoLock(r.Position) {
		return false, nil
	}
	return true, nil
}

func (c *agreementCache) checkFinalizedBlock(b *types.Block) (bool, error) {
	if b.Position.Round < DKGDelayRound {
		// Finalized blocks from rounds before DKGDelayRound can't be the proof
		// of any agreement.
		return false, nil
	}
	ok, err := c.recv.VerifyTSIG(b.Position.Round, b.Hash, b.Randomness)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrIncorrectBlockRandomness
	}
	c.lock.RLock()
	defer c.lock.RUnlock()
	return !c.isIgnorableNoLock(b.Position), nil
}

func (c *agreementCache) trigger(v types.Vote, vSet *voteSet,
	config agreementCacheConfig) (e *agreementEvent, err error) {
	var (
		maxVotes      = len(config.notarySet)
		requiredVotes = maxVotes*2/3 + 1
	)
	newDecideEvt := func(vSet *voteSet) (*agreementEvent, error) {
		votes := vSet.votesByHash(vSet.best, requiredVotes)
		if v.Position.Round < DKGDelayRound {
			return newAgreementEvent(agreementEventDecide, votes), nil
		}
		tsig, err := c.recv.RecoverTSIG(vSet.best, votes)
		if err != nil {
			return nil, err
		}
		e := newAgreementEventFromTSIG(v.Position, vSet.best, tsig,
			vSet.best == types.NullBlockHash)
		return e, nil
	}
	switch v.Type {
	case types.VoteCom:
		// 2t+1 commit votes on SKIP should be handled by condition#3, other
		// cases should trigger a "decide" event.
		if vSet.best != types.SkipBlockHash &&
			vSet.counts[vSet.best] >= requiredVotes {
			e, err = newDecideEvt(vSet)
			break
		}
		// It's the condition#3, there are more than 2t+1 commit votes for
		// different values or skip.
		if vSet.triggeredEvent == nil && len(vSet.votes) >= requiredVotes {
			copiedVotes := make([]types.Vote, requiredVotes)
			copy(copiedVotes, vSet.votesAsList)
			e = newAgreementEvent(agreementEventForward, copiedVotes)
			break
		}
	case types.VoteFastCom:
		if vSet.best == types.SkipBlockHash ||
			vSet.best == types.NullBlockHash ||
			vSet.counts[vSet.best] < requiredVotes {
			break
		}
		e, err = newDecideEvt(vSet)
	case types.VotePreCom, types.VoteFast:
		if vSet.counts[vSet.best] < requiredVotes {
			break
		}
		e = newAgreementEvent(
			agreementEventLock, vSet.votesByHash(vSet.best, requiredVotes))
	}
	if err != nil {
		return
	}
	if e == nil {
		// TODO(mission): this threshold can be lowered to f+1, however, it
		//                might not be equal to better performance (In most
		//                cases we should be able to reach agreement in one
		//                period.) Need to benchmark before adjusting it.
		if v.Type != types.VoteCom &&
			len(vSet.votesAsList) >= requiredVotes &&
			!vSet.isAgreedOnOneHashPossible(maxVotes, requiredVotes) {
			vSet.setEvent(nil, true)
		}
		return
	}
	// Overwriting a triggered decide event is not valid.
	if vSet.triggeredEvent != nil &&
		vSet.triggeredEvent.evtType == agreementEventDecide {
		panic(fmt.Errorf("attempt to overwrite a decide signal: %s %s",
			vSet.triggeredEvent, e))
	}
	if v.Type == types.VoteCom && e.evtType == agreementEventForward {
		// A group of commit votes might trigger a decide signal after
		// triggering a forward signal.
		vSet.setEvent(
			e, !vSet.isAgreedOnOneHashPossible(maxVotes, requiredVotes))
	} else {
		vSet.setEvent(e, true)
	}
	return
}

func (c *agreementCache) updateReferenceEvent(
	e *agreementEvent) (updated bool) {
	refEvt := c.refEvts[e.evtType]
	if refEvt != nil {
		// Make sure we are raising signal forwarding. All signals from
		// older position or older period of the same position should never
		// happen, except fork votes.
		if e.Position.Older(refEvt.Position) {
			panic(fmt.Errorf("backward agreement event: %s %s", refEvt, e))
		}
		if e.Position.Equal(refEvt.Position) {
			switch e.evtType {
			case agreementEventDecide:
				panic(fmt.Errorf("duplicated decided event: %s %s", refEvt, e))
			case agreementEventLock:
				if e.period > refEvt.period {
					break
				}
				panic(fmt.Errorf("backward lock event: %s %s", refEvt, e))
			case agreementEventForward:
				// It's possible for forward signal triggered in older period,
				// we should not panic it.
				if e.period <= refEvt.period {
					return
				}
			}
		}
	}
	updated = true
	c.refEvts[e.evtType] = e
	switch e.evtType {
	case agreementEventLock:
		// A lock signal by commit votes should trigger period forwarding, too.
		eF := c.refEvts[agreementEventForward]
		if eF != nil {
			if eF.Position.Newer(e.Position) {
				break
			}
			if eF.Position.Equal(e.Position) && eF.period <= e.period {
				break
			}
		}
		c.refEvts[agreementEventForward] = e
	case agreementEventDecide:
		clearRef := func(eType agreementEventType) {
			if c.refEvts[eType] == nil {
				return
			}
			if c.refEvts[eType].Position.Newer(e.Position) {
				return
			}
			c.refEvts[eType] = nil
		}
		clearRef(agreementEventForward)
		clearRef(agreementEventLock)
		c.refPosition.Store(e.Position)
	}
	return
}

func (c *agreementCache) purgeBy(e *agreementEvent) {
	purgeByPeriod := func(vForPeriod []*voteSet) {
		for vType, vSet := range vForPeriod {
			switch types.VoteType(vType) {
			case types.VotePreCom, types.VoteFast:
				vSet.setEvent(nil, true)
			}
		}
	}
	switch e.evtType {
	case agreementEventDecide:
		for p := range c.vSets {
			if !p.Newer(e.Position) {
				delete(c.vSets, p)
			}
		}
		// Purge notary set by position when decided.
		firstRoundID := c.configs[0].RoundID()
		if e.Position.Round > firstRoundID {
			c.configs = c.configs[e.Position.Round-firstRoundID:]
		}
	case agreementEventLock:
		// For older positions, purge all pre-commit/fast votes.
		for p, vForPosition := range c.vSets {
			if !p.Older(e.Position) {
				continue
			}
			for _, vForPeriod := range vForPosition {
				purgeByPeriod(vForPeriod)
			}
		}
		// Only locked signal can be used to purge older periods in the same
		// position.
		vForPosition := c.vSets[e.Position]
		for period, vForPeriod := range vForPosition {
			if period >= e.period {
				continue
			}
			// It's safe to purge votes in older periods, except for
			// commit/fast-commit votes: a decide signal should be raised from
			// any period even if we've locked on some later period. We can only
			// purge those votes when it's impossible to trigger an decide
			// signal from that period.
			purgeByPeriod(vForPeriod)
		}
	}
}

func newAgreementCache(recv agreementCacheReceiver) (c *agreementCache) {
	c = &agreementCache{
		vSets:          make(map[types.Position]map[uint64][]*voteSet),
		refEvts:        make([]*agreementEvent, maxAgreementEventType),
		lastSnapshotCh: make(chan time.Time, 1),
		recv:           recv,
	}
	c.lastSnapshotCh <- time.Time{}
	c.lastSnapshot.Store(&agreementSnapshot{})
	c.refPosition.Store(types.Position{})
	return
}

func (c *agreementCache) notifyRoundEvents(
	evts []utils.RoundEventParam) error {
	apply := func(e utils.RoundEventParam) error {
		if len(c.configs) > 0 {
			lastCfg := c.configs[len(c.configs)-1]
			if e.BeginHeight != lastCfg.RoundEndHeight() {
				return ErrInvalidBlockHeight
			}
			if lastCfg.RoundID() == e.Round {
				c.configs[len(c.configs)-1].ExtendLength()
			} else if lastCfg.RoundID()+1 == e.Round {
				notarySet, err := c.recv.GetNotarySet(e.Round)
				if err != nil {
					return err
				}
				c.configs = append(c.configs, newAgreementCacheConfig(
					lastCfg, e.Config, notarySet))
			} else {
				return ErrInvalidRoundID
			}
		} else {
			notarySet, err := c.recv.GetNotarySet(e.Round)
			if err != nil {
				return err
			}
			cfg := agreementCacheConfig{}
			cfg.from(e.Round, e.Config, notarySet)
			cfg.SetRoundBeginHeight(e.BeginHeight)
			c.configs = append(c.configs, cfg)
		}
		return nil
	}
	c.configsLock.Lock()
	defer c.configsLock.Unlock()
	for _, e := range evts {
		if err := apply(e); err != nil {
			return err
		}
	}
	return nil
}

func (c *agreementCache) processVote(
	v types.Vote) (evts []*agreementEvent, err error) {
	if c.isIgnorable(v.Position) {
		return
	}
	config, found := c.config(v.Position.Round)
	if !found {
		err = ErrRoundOutOfRange
		return
	}
	ok, forkEvt, err := c.checkVote(v, config)
	if forkEvt != nil {
		evts = append(evts, forkEvt)
	}
	if err != nil || !ok {
		return
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	// Although we've checked if this vote is ignorable in checkVote method,
	// it might become ignorable before we acquire writer lock.
	if c.isVoteIgnorable(v) {
		return
	}
	vSet := c.votes(v, config, true)
	switch {
	case vSet.isPurged():
	case vSet.triggeredEvent != nil:
		if v.Type == types.VotePreCom || v.Type == types.VoteFast {
			break
		}
		fallthrough
	default:
		if oldV, forked := vSet.add(v); forked {
			panic(fmt.Errorf("unexpected forked vote: %s %s", &oldV, &v))
		}
		var evt *agreementEvent
		evt, err = c.trigger(v, vSet, config)
		if err != nil {
			break
		}
		if evt != nil {
			if c.updateReferenceEvent(evt) {
				c.purgeBy(evt)
				evts = append(evts, evt)
			}
		}
	}
	return
}

func (c *agreementCache) processAgreementResult(
	r *types.AgreementResult) (evts []*agreementEvent, err error) {
	if c.isIgnorable(r.Position) {
		return
	}
	config, found := c.config(r.Position.Round)
	if !found {
		err = ErrRoundOutOfRange
		return
	}
	ok, err := c.checkResult(r, config)
	if err != nil || !ok {
		return
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.isIgnorableNoLock(r.Position) {
		return
	}
	var evt *agreementEvent
	if r.Position.Round >= DKGDelayRound {
		evt = newAgreementEventFromTSIG(
			r.Position, r.BlockHash, r.Randomness, r.IsEmptyBlock)
	} else {
		vSet := c.votes(r.Votes[0], config, true)
		for _, v := range r.Votes {
			if oldV, forked := vSet.add(v); forked {
				evts = append(evts, newAgreementEvent(
					agreementEventFork, []types.Vote{oldV, v}))
			}
		}
		evt, err = c.trigger(r.Votes[0], vSet, config)
	}
	if err != nil {
		return
	}
	if evt == nil {
		err = ErrUnableToAgree
		return
	}
	if c.updateReferenceEvent(evt) {
		c.purgeBy(evt)
		evts = append(evts, evt)
	} else {
		// It should be treated as error when unable to proceed via
		// types.AgreementResult.
		err = fmt.Errorf("unable to assign decide event: %s from %s", evt, r)
		return
	}
	return
}

func (c *agreementCache) processFinalizedBlock(
	b *types.Block) (evts []*agreementEvent, err error) {
	if c.isIgnorable(b.Position) {
		return
	}
	ok, err := c.checkFinalizedBlock(b)
	if err != nil || !ok {
		return
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.isIgnorableNoLock(b.Position) {
		return
	}
	evt := newAgreementEventFromTSIG(
		b.Position, b.Hash, b.Randomness, b.IsEmpty())
	if c.updateReferenceEvent(evt) {
		c.purgeBy(evt)
		evts = append(evts, evt)
	} else {
		// It should be treated as error when unable to proceed via finalized
		// blocks.
		err = fmt.Errorf("unable to assign decide event: %s from %s", evt, b)
		return
	}
	return
}

func (c *agreementCache) snapshot(expect time.Time) *agreementSnapshot {
	snapshoted := <-c.lastSnapshotCh
	defer func() {
		c.lastSnapshotCh <- snapshoted
	}()
	if !snapshoted.Before(expect) {
		// Reuse current snapshot, someone else might perform snapshot right
		// before us.
		return c.lastSnapshot.Load().(*agreementSnapshot)
	}
	// NOTE: There is no clue to decide which round of config to use as snapshot
	//       refresh interval, simply pick the first one as workaround.
	var refreshInterval = 200 * time.Millisecond
	if config, found := c.firstConfig(); found {
		// The interval to refresh the cache should be shorter than lambdaBA.
		refreshInterval = config.lambdaBA * 4 / 7
	}
	c.lock.RLock()
	defer c.lock.RUnlock()
	ss := &agreementSnapshot{
		evtDecide: c.refEvts[agreementEventDecide],
	}
	var positions []types.Position
	for position := range c.vSets {
		positions = append(positions, position)
	}
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].Older(positions[j])
	})
	// All votes in cache are useful votes, so just keep them all in snapshot.
	// They would be orgainzed in acending order of (position, period) of votes.
	for _, position := range positions {
		vForPosition := c.vSets[position]
		var periods []uint64
		for period := range vForPosition {
			periods = append(periods, period)
		}
		sort.Slice(periods, func(i, j int) bool {
			return periods[i] < periods[j]
		})
		for _, period := range periods {
			vForPeriod := vForPosition[period]
			votes := []types.Vote{}
			for t, vSet := range vForPeriod {
				if t == int(types.VotePreCom) || t == int(types.VoteFast) {
					votes = vSet.appendTo(votes)
				}
			}
			ss.addPreCommitVotes(position, period, votes)
		}
	}
	ss.markBoundary()
	// It's the part for commit/fast-commit votes, they should be appended in
	// reversed order.
	for i := range positions {
		position := positions[len(positions)-i-1]
		vForPosition := c.vSets[position]
		var periods []uint64
		for period := range vForPosition {
			periods = append(periods, period)
		}
		sort.Slice(periods, func(i, j int) bool {
			return periods[i] > periods[j]
		})
		for _, period := range periods {
			vForPeriod := vForPosition[period]
			votes := []types.Vote{}
			for t, vSet := range vForPeriod {
				if t == int(types.VoteFastCom) || t == int(types.VoteCom) {
					votes = vSet.appendTo(votes)
				}
			}
			ss.addCommitVotes(position, period, votes)
		}
	}
	ss.expired = time.Now().Add(refreshInterval)
	c.lastSnapshot.Store(ss)
	snapshoted = ss.expired
	return ss
}

// pull latest agreement results, and votes for ongoing BA. This method can be
// called concurrently.
func (c *agreementCache) pull(p types.Position, lockPeriod uint64) (
	r *types.AgreementResult, votes []types.Vote) {
	snapshot := c.lastSnapshot.Load().(*agreementSnapshot)
	now := time.Now()
	if now.After(snapshot.expired) {
		snapshot = c.snapshot(now)
	}
	return snapshot.get(p, lockPeriod)
}
