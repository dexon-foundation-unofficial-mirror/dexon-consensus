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
	"fmt"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors for agreement module.
var (
	ErrNotInNotarySet         = fmt.Errorf("not in notary set")
	ErrIncorrectVoteSignature = fmt.Errorf("incorrect vote signature")
)

// ErrFork for fork error in agreement.
type ErrFork struct {
	nID      types.NodeID
	old, new common.Hash
}

func (e *ErrFork) Error() string {
	return fmt.Sprintf("fork is found for %s, old %s, new %s",
		e.nID.String(), e.old, e.new)
}

// ErrForkVote for fork vote error in agreement.
type ErrForkVote struct {
	nID      types.NodeID
	old, new *types.Vote
}

func (e *ErrForkVote) Error() string {
	return fmt.Sprintf("fork vote is found for %s, old %s, new %s",
		e.nID.String(), e.old, e.new)
}

func newVoteListMap() []map[types.NodeID]*types.Vote {
	listMap := make([]map[types.NodeID]*types.Vote, types.MaxVoteType)
	for idx := range listMap {
		listMap[idx] = make(map[types.NodeID]*types.Vote)
	}
	return listMap
}

// agreementReceiver is the interface receiving agreement event.
type agreementReceiver interface {
	ProposeVote(vote *types.Vote)
	ProposeBlock() common.Hash
	// ConfirmBlock is called with lock hold. User can safely use all data within
	// agreement module.
	ConfirmBlock(common.Hash, map[types.NodeID]*types.Vote)
	PullBlocks(common.Hashes)
}

type pendingBlock struct {
	block        *types.Block
	receivedTime time.Time
}

type pendingVote struct {
	vote         *types.Vote
	receivedTime time.Time
}

// agreementData is the data for agreementState.
type agreementData struct {
	recv agreementReceiver

	ID           types.NodeID
	leader       *leaderSelector
	lockValue    common.Hash
	lockRound    uint64
	period       uint64
	requiredVote int
	votes        map[uint64][]map[types.NodeID]*types.Vote
	lock         sync.RWMutex
	blocks       map[types.NodeID]*types.Block
	blocksLock   sync.Mutex
}

// agreement is the agreement protocal describe in the Crypto Shuffle Algorithm.
type agreement struct {
	state          agreementState
	data           *agreementData
	aID            *atomic.Value
	notarySet      map[types.NodeID]struct{}
	hasOutput      bool
	lock           sync.RWMutex
	pendingBlock   []pendingBlock
	pendingVote    []pendingVote
	candidateBlock map[common.Hash]*types.Block
	fastForward    chan uint64
	signer         *utils.Signer
}

// newAgreement creates a agreement instance.
func newAgreement(
	ID types.NodeID,
	recv agreementReceiver,
	leader *leaderSelector,
	signer *utils.Signer) *agreement {
	agreement := &agreement{
		data: &agreementData{
			recv:   recv,
			ID:     ID,
			leader: leader,
		},
		aID:            &atomic.Value{},
		candidateBlock: make(map[common.Hash]*types.Block),
		fastForward:    make(chan uint64, 1),
		signer:         signer,
	}
	agreement.stop()
	return agreement
}

// restart the agreement
func (a *agreement) restart(
	notarySet map[types.NodeID]struct{}, aID types.Position, crs common.Hash) {

	if !func() bool {
		a.lock.Lock()
		defer a.lock.Unlock()
		if !isStop(aID) {
			oldAID := a.agreementID()
			if !isStop(oldAID) && !aID.Newer(&oldAID) {
				return false
			}
		}
		a.data.lock.Lock()
		defer a.data.lock.Unlock()
		a.data.blocksLock.Lock()
		defer a.data.blocksLock.Unlock()
		a.data.votes = make(map[uint64][]map[types.NodeID]*types.Vote)
		a.data.votes[1] = newVoteListMap()
		a.data.period = 1
		a.data.blocks = make(map[types.NodeID]*types.Block)
		a.data.requiredVote = len(notarySet)/3*2 + 1
		a.data.leader.restart(crs)
		a.data.lockValue = nullBlockHash
		a.data.lockRound = 0
		a.fastForward = make(chan uint64, 1)
		a.hasOutput = false
		a.state = newInitialState(a.data)
		a.notarySet = notarySet
		a.candidateBlock = make(map[common.Hash]*types.Block)
		a.aID.Store(aID)
		return true
	}() {
		return
	}

	if isStop(aID) {
		return
	}

	expireTime := time.Now().Add(-10 * time.Second)
	replayBlock := make([]*types.Block, 0)
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		newPendingBlock := make([]pendingBlock, 0)
		for _, pending := range a.pendingBlock {
			if aID.Newer(&pending.block.Position) {
				continue
			} else if pending.block.Position == aID {
				replayBlock = append(replayBlock, pending.block)
			} else if pending.receivedTime.After(expireTime) {
				newPendingBlock = append(newPendingBlock, pending)
			}
		}
		a.pendingBlock = newPendingBlock
	}()

	replayVote := make([]*types.Vote, 0)
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		newPendingVote := make([]pendingVote, 0)
		for _, pending := range a.pendingVote {
			if aID.Newer(&pending.vote.Position) {
				continue
			} else if pending.vote.Position == aID {
				replayVote = append(replayVote, pending.vote)
			} else if pending.receivedTime.After(expireTime) {
				newPendingVote = append(newPendingVote, pending)
			}
		}
		a.pendingVote = newPendingVote
	}()

	for _, block := range replayBlock {
		a.processBlock(block)
	}

	for _, vote := range replayVote {
		a.processVote(vote)
	}
}

func (a *agreement) stop() {
	a.restart(make(map[types.NodeID]struct{}), types.Position{
		ChainID: math.MaxUint32,
	}, common.Hash{})
}

func isStop(aID types.Position) bool {
	return aID.ChainID == math.MaxUint32
}

// clocks returns how many time this state is required.
func (a *agreement) clocks() int {
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	return a.state.clocks()
}

// pullVotes returns if current agreement requires more votes to continue.
func (a *agreement) pullVotes() bool {
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	return a.state.state() == statePullVote ||
		(a.state.state() == statePreCommit && (a.data.period%3) == 0)
}

// agreementID returns the current agreementID.
func (a *agreement) agreementID() types.Position {
	return a.aID.Load().(types.Position)
}

// nextState is called at the specific clock time.
func (a *agreement) nextState() (err error) {
	if func() bool {
		a.lock.RLock()
		defer a.lock.RUnlock()
		return a.hasOutput
	}() {
		a.state = newSleepState(a.data)
		return
	}
	a.state, err = a.state.nextState()
	return
}

func (a *agreement) sanityCheck(vote *types.Vote) error {
	if _, exist := a.notarySet[vote.ProposerID]; !exist {
		return ErrNotInNotarySet
	}
	ok, err := utils.VerifyVoteSignature(vote)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectVoteSignature
	}
	return nil
}

func (a *agreement) checkForkVote(vote *types.Vote) error {
	if err := func() error {
		a.data.lock.RLock()
		defer a.data.lock.RUnlock()
		if votes, exist := a.data.votes[vote.Period]; exist {
			if oldVote, exist := votes[vote.Type][vote.ProposerID]; exist {
				if vote.BlockHash != oldVote.BlockHash {
					return &ErrForkVote{vote.ProposerID, oldVote, vote}
				}
			}
		}
		return nil
	}(); err != nil {
		return err
	}
	return nil
}

// prepareVote prepares a vote.
func (a *agreement) prepareVote(vote *types.Vote) (err error) {
	vote.Position = a.agreementID()
	err = a.signer.SignVote(vote)
	return
}

// processVote is the entry point for processing Vote.
func (a *agreement) processVote(vote *types.Vote) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	if err := a.sanityCheck(vote); err != nil {
		return err
	}
	aID := a.agreementID()
	// Agreement module has stopped.
	if isStop(aID) {
		return nil
	}
	if vote.Position != aID {
		if aID.Newer(&vote.Position) {
			return nil
		}
		a.pendingVote = append(a.pendingVote, pendingVote{
			vote:         vote,
			receivedTime: time.Now().UTC(),
		})
		return nil
	}
	if err := a.checkForkVote(vote); err != nil {
		return err
	}

	a.data.lock.Lock()
	defer a.data.lock.Unlock()
	if _, exist := a.data.votes[vote.Period]; !exist {
		a.data.votes[vote.Period] = newVoteListMap()
	}
	a.data.votes[vote.Period][vote.Type][vote.ProposerID] = vote
	if !a.hasOutput && vote.Type == types.VoteCom {
		if hash, ok := a.data.countVoteNoLock(vote.Period, vote.Type); ok &&
			hash != skipBlockHash {
			a.hasOutput = true
			a.data.recv.ConfirmBlock(hash,
				a.data.votes[vote.Period][types.VoteCom])
			return nil
		}
	} else if a.hasOutput {
		return nil
	}

	// Check if the agreement requires fast-forwarding.
	if len(a.fastForward) > 0 {
		return nil
	}
	if vote.Type == types.VotePreCom {
		if hash, ok := a.data.countVoteNoLock(vote.Period, vote.Type); ok &&
			hash != skipBlockHash {
			// Condition 1.
			if a.data.period >= vote.Period && vote.Period > a.data.lockRound &&
				vote.BlockHash != a.data.lockValue {
				a.data.lockValue = hash
				a.data.lockRound = vote.Period
				return nil
			}
			// Condition 2.
			if vote.Period > a.data.period {
				a.data.lockValue = hash
				a.data.lockRound = vote.Period
				a.fastForward <- vote.Period
				return nil
			}
		}
	}
	// Condition 3.
	if vote.Type == types.VoteCom && vote.Period >= a.data.period &&
		len(a.data.votes[vote.Period][types.VoteCom]) >= a.data.requiredVote {
		hashes := common.Hashes{}
		addPullBlocks := func(voteType types.VoteType) {
			for _, vote := range a.data.votes[vote.Period][voteType] {
				if vote.BlockHash == nullBlockHash || vote.BlockHash == skipBlockHash {
					continue
				}
				if _, found := a.findCandidateBlockNoLock(vote.BlockHash); !found {
					hashes = append(hashes, vote.BlockHash)
				}
			}
		}
		addPullBlocks(types.VoteInit)
		addPullBlocks(types.VotePreCom)
		addPullBlocks(types.VoteCom)
		if len(hashes) > 0 {
			a.data.recv.PullBlocks(hashes)
		}
		a.fastForward <- vote.Period + 1
		return nil
	}
	return nil
}

func (a *agreement) done() <-chan struct{} {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.data.lock.Lock()
	defer a.data.lock.Unlock()
	ch := make(chan struct{}, 1)
	if a.hasOutput {
		ch <- struct{}{}
	} else {
		select {
		case period := <-a.fastForward:
			if period <= a.data.period {
				break
			}
			a.data.setPeriod(period)
			a.state = newPreCommitState(a.data)
			ch <- struct{}{}
		default:
		}
	}
	return ch
}

// processBlock is the entry point for processing Block.
func (a *agreement) processBlock(block *types.Block) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.data.blocksLock.Lock()
	defer a.data.blocksLock.Unlock()

	aID := a.agreementID()
	if block.Position != aID {
		// Agreement module has stopped.
		if !isStop(aID) {
			if aID.Newer(&block.Position) {
				return nil
			}
		}
		a.pendingBlock = append(a.pendingBlock, pendingBlock{
			block:        block,
			receivedTime: time.Now().UTC(),
		})
		return nil
	}
	if b, exist := a.data.blocks[block.ProposerID]; exist {
		if b.Hash != block.Hash {
			return &ErrFork{block.ProposerID, b.Hash, block.Hash}
		}
		return nil
	}
	if err := a.data.leader.processBlock(block); err != nil {
		return err
	}
	a.data.blocks[block.ProposerID] = block
	a.addCandidateBlockNoLock(block)
	return nil
}

func (a *agreement) addCandidateBlock(block *types.Block) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.addCandidateBlockNoLock(block)
}

func (a *agreement) addCandidateBlockNoLock(block *types.Block) {
	a.candidateBlock[block.Hash] = block
}

func (a *agreement) findCandidateBlock(hash common.Hash) (*types.Block, bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.findCandidateBlockNoLock(hash)
}

func (a *agreement) findCandidateBlockNoLock(
	hash common.Hash) (*types.Block, bool) {
	b, e := a.candidateBlock[hash]
	return b, e
}
func (a *agreementData) countVote(period uint64, voteType types.VoteType) (
	blockHash common.Hash, ok bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.countVoteNoLock(period, voteType)
}

func (a *agreementData) countVoteNoLock(
	period uint64, voteType types.VoteType) (blockHash common.Hash, ok bool) {
	votes, exist := a.votes[period]
	if !exist {
		return
	}
	candidate := make(map[common.Hash]int)
	for _, vote := range votes[voteType] {
		if _, exist := candidate[vote.BlockHash]; !exist {
			candidate[vote.BlockHash] = 0
		}
		candidate[vote.BlockHash]++
	}
	for candidateHash, votes := range candidate {
		if votes >= a.requiredVote {
			blockHash = candidateHash
			ok = true
			return
		}
	}
	return
}

func (a *agreementData) setPeriod(period uint64) {
	for i := a.period + 1; i <= period; i++ {
		if _, exist := a.votes[i]; !exist {
			a.votes[i] = newVoteListMap()
		}
	}
	a.period = period
}
