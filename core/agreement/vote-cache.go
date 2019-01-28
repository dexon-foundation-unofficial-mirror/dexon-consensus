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

package agreement

import (
	"fmt"
	"sync"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors for agreement module.
var (
	ErrInvalidVote            = fmt.Errorf("invalid vote")
	ErrNotInNotarySet         = fmt.Errorf("not in notary set")
	ErrIncorrectVoteSignature = fmt.Errorf("incorrect vote signature")
	ErrRoundNotIncreasing     = fmt.Errorf("round not increasing")
	ErrInvalidChainID         = fmt.Errorf("invalid chain ID")
	ErrNotarySetNotFound      = fmt.Errorf("notary set not found")
)

// ErrForkVote for fork vote error in agreement.
type ErrForkVote struct {
	nID      types.NodeID
	old, new *types.Vote
}

func (e *ErrForkVote) Error() string {
	return fmt.Sprintf("fork vote is found for %s, old %s, new %s",
		e.nID.String(), e.old, e.new)
}

type votesInfo struct {
	votes           map[types.NodeID]*types.Vote
	counts          map[common.Hash]int
	best            common.Hash
	votesList       []types.Vote
	triggeredSignal *Signal
	purged          bool
}

func (info *votesInfo) add(v *types.Vote) (oldV *types.Vote) {
	oldV, exist := info.votes[v.ProposerID]
	if exist {
		if oldV.BlockHash == v.BlockHash {
			oldV = nil
		}
		return
	}
	info.votes[v.ProposerID] = v
	info.votesList = append(info.votesList, *v)
	if info.counts != nil {
		info.counts[v.BlockHash]++
		if info.counts[v.BlockHash] > info.counts[info.best] {
			info.best = v.BlockHash
		}
	}
	return
}

func (info *votesInfo) getVotes(
	h common.Hash, estimatedLength int) []types.Vote {
	votes := make([]types.Vote, 0, estimatedLength)
	for _, v := range info.votesList {
		if v.BlockHash == h {
			votes = append(votes, v)
		}
	}
	return votes
}

func (info *votesInfo) appendTo(src []types.Vote) []types.Vote {
	if len(info.votesList) == 0 {
		if info.triggeredSignal == nil {
			return src
		}
		return append(src, info.triggeredSignal.Votes...)
	}
	return append(src, info.votesList...)
}

func (info *votesInfo) isAgreedOnOneHashPossible(maxCount, threshold int) bool {
	if len(info.votes) == maxCount {
		return info.counts[info.best] >= threshold
	} else if len(info.votes) > maxCount {
		panic(fmt.Errorf("collecting votes exceeding max count: %v %v %v",
			maxCount, threshold, len(info.votes)))
	}
	return maxCount-len(info.votes)+info.counts[info.best] >= threshold
}

func (info *votesInfo) setSignal(signal *Signal, purge bool) {
	if purge {
		info.votes = nil
		info.counts = nil
		info.votesList = nil
		info.purged = true
	}
	info.triggeredSignal = signal
}

func (info *votesInfo) isPurged() bool {
	return info.purged
}

type voteChainCache struct {
	lock              sync.RWMutex
	notarySets        []map[types.NodeID]struct{}
	pendingSignals    []*Signal
	refSignals        []*Signal
	minNotarySetRound uint64
	// Position -> Period -> VoteType -> ProposerID.
	votes map[types.Position]map[uint64][]*votesInfo
}

func (a *voteChainCache) appendNotarySet(
	round uint64, notarySet map[types.NodeID]struct{}) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	// Initialization case, free to assign every field.
	if len(a.notarySets) == 0 {
		a.minNotarySetRound = round
		a.notarySets = append(a.notarySets, notarySet)
		return nil
	}
	if round != a.minNotarySetRound+uint64(len(a.notarySets)) {
		return ErrRoundNotIncreasing
	}
	a.notarySets = append(a.notarySets, notarySet)
	return nil
}

func (a *voteChainCache) notarySet(r uint64) map[types.NodeID]struct{} {
	if r < a.minNotarySetRound {
		return nil
	}
	setIdx := r - a.minNotarySetRound
	if setIdx >= uint64(len(a.notarySets)) {
		return nil
	}
	return a.notarySets[setIdx]
}

func (a *voteChainCache) votesInfo(
	v *types.Vote, createIfNotExist bool) *votesInfo {
	vForPos, exist := a.votes[v.Position]
	if !exist {
		if !createIfNotExist {
			return nil
		}
		vForPos = make(map[uint64][]*votesInfo)
		a.votes[v.Position] = vForPos
	}
	vForPeriod, exist := vForPos[v.Period]
	if !exist {
		if !createIfNotExist {
			return nil
		}
		notarySet := a.notarySet(v.Position.Round)
		if len(notarySet) == 0 {
			panic(fmt.Errorf("empty notary set when creating votes info: %s", v))
		}
		vForPeriod = make([]*votesInfo, types.MaxVoteType)
		for idx := range vForPeriod {
			if types.VoteType(idx) == types.VoteInit {
				continue
			}
			info := &votesInfo{
				votes:     make(map[types.NodeID]*types.Vote),
				votesList: make([]types.Vote, 0, len(notarySet)),
				counts:    make(map[common.Hash]int),
			}
			vForPeriod[idx] = info
		}
		vForPos[v.Period] = vForPeriod
	}
	return vForPeriod[v.Type]
}

func (a *voteChainCache) purgeBy(s *Signal) {
	for pos := range a.votes {
		if pos.Older(&s.Position) {
			delete(a.votes, pos)
		}
		if s.Type == SignalDecide && pos.Equal(&s.Position) {
			delete(a.votes, pos)
		}
	}
	// Only locked signal can be used to purge older periods in the same
	// position.
	if s.Type != SignalLock {
		return
	}
	vForPos := a.votes[s.Position]
	for period, vForPeriod := range vForPos {
		if period > s.Period {
			continue
		}
		// It's safe to purge votes in older periods, except for
		// commit/fast-commit votes: a decide signal should be raised from any
		// period even if we've locked on some later period. We can only purge
		// those votes when it's impossible to trigger an decide signal from
		// that period.
		for vType, vForType := range vForPeriod {
			if vType == int(types.VotePreCom) || vType == int(types.VoteFast) {
				if period <= s.Period {
					vForType.setSignal(nil, true)
				}
			}
		}
	}
	// Purge notary set by position when decided.
	if s.Type == SignalDecide && s.Position.Round > a.minNotarySetRound {
		a.notarySets = a.notarySets[s.Position.Round-a.minNotarySetRound:]
		a.minNotarySetRound = s.Position.Round
	}
}

func (a *voteChainCache) updateReferenceSignal(s *Signal) (updated bool) {
	refSignal := a.refSignals[s.Type]
	if refSignal != nil {
		// Make sure we are raising signal forwarding. All signals from
		// older position or older period of the same position should never
		// happen, except fork votes.
		if s.Position.Older(&refSignal.Position) {
			panic(fmt.Errorf("backward signal: %s %s", refSignal, s))
		}
		if s.Position.Equal(&refSignal.Position) {
			switch refSignal.Type {
			case SignalDecide:
				// "Decide" is the strongest signal in a position, we shouldn't
				// overwrite it.
				panic(fmt.Errorf(
					"duplicated decided signal in one position: %s %s",
					refSignal, s))
			case SignalLock:
				if s.Period <= refSignal.Period {
					panic(fmt.Errorf("trigger backward locked signal: %s %s",
						refSignal, s))
				}
			case SignalForward:
				// It's possible for forward signal triggered in older period,
				// we should not panic it.
				if s.Period <= refSignal.Period {
					return
				}
			}
		}
	}
	updated = true
	a.refSignals[s.Type] = s
	switch s.Type {
	case SignalLock:
		// A lock signal might trigger period forwarding.
		sF := a.refSignals[SignalForward]
		if sF != nil {
			if sF.Position.Newer(&s.Position) {
				break
			}
			if sF.Position.Equal(&s.Position) && sF.Period <= s.Period {
				break
			}
		}
		a.refSignals[SignalForward] = s
	case SignalDecide:
		clearRef := func(sType SignalType) {
			if a.refSignals[sType] == nil {
				return
			}
			if a.refSignals[sType].Position.Newer(&s.Position) {
				return
			}
			a.refSignals[sType] = nil
		}
		clearRef(SignalForward)
		clearRef(SignalLock)
	}
	a.pendingSignals = append(a.pendingSignals, s)
	return
}

func (a *voteChainCache) trigger(v *types.Vote, info *votesInfo) (s *Signal) {
	var (
		maxVotes      = len(a.notarySet(v.Position.Round))
		requiredVotes = maxVotes/3*2 + 1
	)
	switch v.Type {
	case types.VoteCom:
		// 2t+1 commit votes on SKIP should be handled by condition#3.
		if info.best != types.SkipBlockHash &&
			info.counts[info.best] >= requiredVotes {
			s = NewSignal(SignalDecide, info.getVotes(info.best, requiredVotes))
			break
		}
		// It's the condition#3, there are more than 2t+1 commit votes for
		// different values or skip.
		if info.triggeredSignal == nil {
			if len(info.votes) >= requiredVotes {
				copiedVotes := make([]types.Vote, requiredVotes)
				copy(copiedVotes, info.votesList)
				s = NewSignal(SignalForward, copiedVotes)
			}
		}
	case types.VoteFastCom:
		if info.best == types.SkipBlockHash ||
			info.best == types.NullBlockHash ||
			info.counts[info.best] < requiredVotes {
			break
		}
		s = NewSignal(SignalDecide, info.getVotes(info.best, requiredVotes))
	case types.VotePreCom, types.VoteFast:
		if info.counts[info.best] < requiredVotes {
			break
		}
		s = NewSignal(SignalLock, info.getVotes(info.best, requiredVotes))
	}
	if s == nil {
		if v.Type != types.VoteCom {
			// TODO(mission): this threshold can be lowered to f+1, however, it
			//                might not be equal to better performance (In most
			//                cases we should be able to reach agreement in one
			//                period.) Need to benchmark before adjusting it.
			if len(info.votesList) >= requiredVotes &&
				!info.isAgreedOnOneHashPossible(maxVotes, requiredVotes) {
				info.setSignal(nil, true)
			}
		}
		return
	}
	// Only overwriting a non-decide signal with a decide signal is valid.
	if info.triggeredSignal != nil && info.triggeredSignal.Type == SignalDecide {
		panic(fmt.Errorf(
			"unexpected attempt to overwrite a decide signal: %s %s",
			info.triggeredSignal, s))
	}
	if v.Type == types.VoteCom && s.Type == SignalForward {
		// A group of commit votes might trigger a decide signal after
		// triggering a forward signal.
		info.setSignal(s, !info.isAgreedOnOneHashPossible(maxVotes,
			requiredVotes))
	} else {
		info.setSignal(s, true)
	}
	return
}

func (a *voteChainCache) extractSignals() (signals []*Signal) {
	if some := func() bool {
		a.lock.RLock()
		defer a.lock.RUnlock()
		return len(a.pendingSignals) > 0
	}(); !some {
		return
	}
	a.lock.Lock()
	defer a.lock.Unlock()
	signals, a.pendingSignals = a.pendingSignals, nil
	return
}

func (a *voteChainCache) isVoteIgnorable(v *types.Vote) bool {
	if v.Type == types.VoteInit {
		return true
	}
	sDecide := a.refSignals[SignalDecide]
	if sDecide == nil {
		return false
	}
	if !v.Position.Newer(&sDecide.Position) {
		return true
	}
	switch v.Type {
	case types.VotePreCom, types.VoteFast:
		sLock := a.refSignals[SignalLock]
		if sLock == nil || sLock.Position.Older(&v.Position) {
			return false
		}
		if sLock.Position.Newer(&v.Position) {
			return true
		}
		return sLock.Period >= v.Period
	}
	return false
}

func (a *voteChainCache) checkVote(v *types.Vote) (bool, error) {
	if v.Type >= types.MaxVoteType {
		return false, ErrInvalidVote
	}
	ok, err := utils.VerifyVoteSignature(v)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrIncorrectVoteSignature
	}
	a.lock.RLock()
	defer a.lock.RUnlock()
	if a.isVoteIgnorable(v) {
		return false, nil
	}
	notarySet := a.notarySet(v.Position.Round)
	if notarySet == nil {
		return false, ErrNotarySetNotFound
	}
	if _, exist := notarySet[v.ProposerID]; !exist {
		return false, ErrNotInNotarySet
	}
	// Check for forked vote.
	if info := a.votesInfo(v, false); info != nil {
		if oldVote, exist := info.votes[v.ProposerID]; exist {
			if v.BlockHash != oldVote.BlockHash {
				return false, &ErrForkVote{v.ProposerID, oldVote, v}
			}
		}
	}
	return true, nil
}

func (a *voteChainCache) isResultIgnorable(result *types.AgreementResult) bool {
	sDecide := a.refSignals[SignalDecide]
	if sDecide == nil {
		return false
	}
	return !result.Position.Newer(&sDecide.Position)
}

func (a *voteChainCache) processVote(v *types.Vote) error {
	ok, err := a.checkVote(v)
	if err != nil {
		if forkErr, isForked := err.(*ErrForkVote); isForked {
			func() {
				a.lock.Lock()
				defer a.lock.Unlock()
				a.pendingSignals = append(a.pendingSignals, NewSignal(
					SignalFork,
					[]types.Vote{*forkErr.old, *forkErr.new}))
			}()
			// Vote-forking is reported as signal, not error.
			err = nil
		}
		return err
	}
	if !ok {
		return nil
	}
	a.lock.Lock()
	defer a.lock.Unlock()
	// Although we've checked if this vote is ignorable in checkVote method,
	// it might become ignorable before we acquire writer lock.
	if a.isVoteIgnorable(v) {
		return nil
	}
	info := a.votesInfo(v, true)
	switch {
	case info.isPurged():
	case info.triggeredSignal != nil:
		if v.Type == types.VotePreCom || v.Type == types.VoteFast {
			break
		}
		fallthrough
	default:
		if oldV := info.add(v); oldV != nil {
			panic(fmt.Errorf("unexpected forked detection: %s %s", oldV, v))
		}
		if s := a.trigger(v, info); s != nil {
			if a.updateReferenceSignal(s) {
				a.purgeBy(s)
			}
		}
	}
	return nil
}

func (a *voteChainCache) processResult(result *types.AgreementResult) error {
	// TODO(mission): move sanity check here before locked.
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.isResultIgnorable(result) {
		return nil
	}
	// Check fork votes before adding them.
	info := a.votesInfo(&result.Votes[0], true)
	if info.isPurged() {
		panic(fmt.Errorf(
			"receive agreement result in purged period: %s", result))
	}
	for _, v := range result.Votes {
		if oldV := info.add(&v); oldV != nil {
			a.pendingSignals = append(a.pendingSignals, NewSignal(
				SignalFork, []types.Vote{*oldV, v}))
			continue
		}
	}
	if s := a.trigger(&result.Votes[0], info); s != nil {
		if a.updateReferenceSignal(s) {
			a.purgeBy(s)
		}
	} else {
		panic(fmt.Errorf(
			"unable to trigger decide signal via agreement result: %s", result))
	}
	return nil
}

func (a *voteChainCache) pull(position types.Position, lockPeriod uint64) (
	votes []types.Vote) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	addVotesInPos := func(vForPos map[uint64][]*votesInfo, minPeriod uint64) {
		for period, vForPeriod := range vForPos {
			if period <= minPeriod {
				// We can't skip commit/fast-commit votes, unless those votes
				// are impossible to trigger a decide signal.
				if vForPeriod[types.VoteCom] != nil {
					votes = vForPeriod[types.VoteCom].appendTo(votes)
				}
				if vForPeriod[types.VoteFastCom] != nil {
					votes = vForPeriod[types.VoteFastCom].appendTo(votes)
				}
				continue
			}
			for _, vForType := range vForPeriod {
				if vForType != nil {
					votes = vForType.appendTo(votes)
				}
			}
		}
	}
	sDecide := a.refSignals[SignalDecide]
	if sDecide != nil && !sDecide.Position.Older(&position) {
		votes = append(votes, sDecide.Votes...)
	}
	sLock := a.refSignals[SignalLock]
	switch {
	case sLock == nil:
	case sLock.Position.Older(&position):
	case sLock.Position.Equal(&position) && sLock.Period <= lockPeriod:
	default:
		votes = append(votes, sLock.Votes...)
	}
	for pos, vForPos := range a.votes {
		if pos.Older(&position) {
			continue
		}
		if pos.Newer(&position) {
			addVotesInPos(vForPos, 0)
		} else {
			addVotesInPos(vForPos, lockPeriod)
		}
	}
	return
}

// VoteCache is a cache for votes for one chain.
// - it's not designed for concurrent usage for adding new chains,
//   the caller should protect it.
// - it doesn't perform sanity check against those added vote, caller should
//   make sure they are valid before adding them.
type VoteCache struct {
	caches             []*voteChainCache
	pendingSignals     []*Signal
	pendingSignalsLock sync.RWMutex
	expectedNextRound  uint64
}

// NewVoteCache creates an voteCache instance.
func NewVoteCache(initRound uint64) *VoteCache {
	return &VoteCache{expectedNextRound: initRound}
}

// chain returns a chain caches if exists.
//
// Note: This method is expected to be protected by the reader-lock of
// core.agreementMgr.chainLock.
func (a *VoteCache) chain(chainID uint32) (*voteChainCache, error) {
	if chainID >= uint32(len(a.caches)) {
		return nil, ErrInvalidChainID
	}
	return a.caches[chainID], nil
}

// AppendNotarySets adds notary sets for each chain in one round. Note callers
// are responsible to provide notary set for each round after initialized, and
// should assign them in order.
//
// Note: This method is expected to be protected by the writer-lock of
// core.agreementMgr.chainLock.
func (a *VoteCache) AppendNotarySets(
	round uint64, notarySets []map[types.NodeID]struct{}) error {
	if round != a.expectedNextRound {
		return ErrRoundNotIncreasing
	}
	a.expectedNextRound++
	for len(notarySets) > len(a.caches) {
		a.caches = append(a.caches, &voteChainCache{
			votes:      make(map[types.Position]map[uint64][]*votesInfo),
			refSignals: make([]*Signal, maxSignalType),
		})
	}
	for chainID, notarySet := range notarySets {
		if err :=
			a.caches[chainID].appendNotarySet(round, notarySet); err != nil {
			panic(err)
		}
	}
	// Assign empty notary set to those inactive chains, to make notarySets kept
	// by slice in each chain, this is the easiest way.
	for chainID := len(notarySets); chainID < len(a.caches); chainID++ {
		if err :=
			a.caches[chainID].appendNotarySet(round, nil); err != nil {
			panic(err)
		}
	}
	return nil
}

// ProcessVote processes a vote, and reply triggered signal (ex. fast-forward,
// decide ...).
func (a *VoteCache) ProcessVote(v *types.Vote) ([]*Signal, error) {
	c, err := a.chain(v.Position.ChainID)
	if err != nil {
		return nil, err
	}
	if err = c.processVote(v); err != nil {
		return nil, err
	}
	return c.extractSignals(), nil
}

// ProcessResult handles all votes in an agreement result, it should
// return an decided signal when possible.
func (a *VoteCache) ProcessResult(r *types.AgreementResult) ([]*Signal, error) {
	c, err := a.chain(r.Position.ChainID)
	if err != nil {
		return nil, err
	}
	if err = c.processResult(r); err != nil {
		return nil, err
	}
	return c.extractSignals(), nil
}

// Pull votes by giving the position/period of requester's latest strong signal.
func (a *VoteCache) Pull(pos types.Position, lockPeriod uint64) []types.Vote {
	c, err := a.chain(pos.ChainID)
	if err != nil {
		return nil
	}
	return c.pull(pos, lockPeriod)
}
