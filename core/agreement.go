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
	agrPkg "github.com/dexon-foundation/dexon-consensus/core/agreement"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// Errors for agreement module.
var (
	ErrInvalidVote            = fmt.Errorf("invalid vote")
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
	ConfirmBlock(common.Hash, []types.Vote)
	PullBlocks(common.Hashes)
	ReportForkVote(v1, v2 *types.Vote)
	ReportForkBlock(b1, b2 *types.Block)
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

	ID         types.NodeID
	isLeader   bool
	leader     *leaderSelector
	lockValue  common.Hash
	lockIter   uint64
	period     uint64
	lock       sync.RWMutex
	blocks     map[types.NodeID]*types.Block
	blocksLock sync.Mutex
}

// agreement is the agreement protocal describe in the Crypto Shuffle Algorithm.
type agreement struct {
	state          agreementState
	data           *agreementData
	aID            *atomic.Value
	doneChan       chan struct{}
	hasVoteFast    bool
	hasOutput      bool
	lock           sync.RWMutex
	pendingBlock   []pendingBlock
	pendingSignal  []*agrPkg.Signal
	candidateBlock map[common.Hash]*types.Block
	fastForward    chan uint64
	signer         *utils.Signer
	logger         common.Logger
}

// newAgreement creates a agreement instance.
func newAgreement(
	ID types.NodeID,
	recv agreementReceiver,
	leader *leaderSelector,
	signer *utils.Signer,
	logger common.Logger) *agreement {
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
		logger:         logger,
	}
	agreement.stop()
	return agreement
}

// restart the agreement
func (a *agreement) restart(
	aID types.Position, leader types.NodeID, crs common.Hash) {
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
		a.data.period = 2
		a.data.blocks = make(map[types.NodeID]*types.Block)
		a.data.leader.restart(crs)
		a.data.lockValue = types.NullBlockHash
		a.data.lockIter = 0
		a.data.isLeader = a.data.ID == leader
		if a.doneChan != nil {
			close(a.doneChan)
		}
		a.doneChan = make(chan struct{})
		a.fastForward = make(chan uint64, 1)
		a.hasVoteFast = false
		a.hasOutput = false
		a.state = newFastState(a.data)
		a.candidateBlock = make(map[common.Hash]*types.Block)
		a.aID.Store(struct {
			pos    types.Position
			leader types.NodeID
		}{aID, leader})
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
	replaySignal := make([]*agrPkg.Signal, 0)
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		newPendingSignal := make([]*agrPkg.Signal, 0)
		for _, pending := range a.pendingSignal {
			if aID.Newer(&pending.Position) {
				continue
			} else if pending.Position == aID {
				replaySignal = append(replaySignal, pending)
			} else {
				newPendingSignal = append(newPendingSignal, pending)
			}
		}
		a.pendingSignal = newPendingSignal
	}()
	for _, block := range replayBlock {
		if err := a.processBlock(block); err != nil {
			a.logger.Error("failed to process block when restarting agreement",
				"block", block)
		}
	}
	for _, signal := range replaySignal {
		if err := a.processSignal(signal); err != nil {
			a.logger.Error("failed to process signal when restarting agreement",
				"signal", signal)
		}
	}
}

func (a *agreement) stop() {
	a.restart(types.Position{ChainID: math.MaxUint32}, types.NodeID{},
		common.Hash{})
}

func isStop(aID types.Position) bool {
	return aID.ChainID == math.MaxUint32
}

// clocks returns how many time this state is required.
func (a *agreement) clocks() int {
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	scale := int(a.data.period) - 1
	if scale < 1 {
		// just in case.
		scale = 1
	}
	// 10 is a magic number derived from many years of experience.
	if scale > 10 {
		scale = 10
	}
	return a.state.clocks() * scale
}

// pullVotes returns if current agreement requires more votes to continue.
func (a *agreement) pullVotes() bool {
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	return a.state.state() == statePullVote ||
		a.state.state() == stateInitial ||
		(a.state.state() == statePreCommit && (a.data.period%3) == 0)
}

// agreementID returns the current agreementID.
func (a *agreement) agreementID() types.Position {
	return a.aID.Load().(struct {
		pos    types.Position
		leader types.NodeID
	}).pos
}

// leader returns the current leader.
func (a *agreement) leader() types.NodeID {
	return a.aID.Load().(struct {
		pos    types.Position
		leader types.NodeID
	}).leader
}

// nextState is called at the specific clock time.
func (a *agreement) nextState() (err error) {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.hasOutput {
		a.state = newSleepState(a.data)
		return
	}
	a.state, err = a.state.nextState()
	return
}

// prepareVote prepares a vote.
func (a *agreement) prepareVote(vote *types.Vote) (err error) {
	vote.Position = a.agreementID()
	err = a.signer.SignVote(vote)
	return
}

func (a *agreement) updateFilter(filter *utils.VoteFilter) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	filter.Confirm = a.hasOutput
	filter.LockIter = a.data.lockIter
	filter.Period = a.data.period
	filter.Height = a.agreementID().Height
}

// processSignal is the entry point for processing agreement.Signal.
func (a *agreement) processSignal(signal *agrPkg.Signal) error {
	addPullBlocks := func(votes []types.Vote) map[common.Hash]struct{} {
		set := make(map[common.Hash]struct{})
		for _, vote := range votes {
			if vote.BlockHash == types.NullBlockHash ||
				vote.BlockHash == types.SkipBlockHash {
				continue
			}
			if _, found := a.findCandidateBlockNoLock(vote.BlockHash); !found {
				set[vote.BlockHash] = struct{}{}
			}
		}
		return set
	}
	a.lock.Lock()
	defer a.lock.Unlock()
	aID := a.agreementID()
	if isStop(aID) {
		// Hacky way to not drop the first signal for height 0.
		if signal.Position.Height == 0 {
			a.pendingSignal = append(a.pendingSignal, signal)
		}
		a.logger.Trace("Dropping signal when stopped", "signal", signal)
		return nil
	}
	if signal.Position != aID {
		if aID.Newer(&signal.Position) {
			a.logger.Trace("Dropping older stopped", "signal", signal)
			return nil
		}
		a.pendingSignal = append(a.pendingSignal, signal)
		return nil
	}
	a.logger.Trace("ProcessSignal", "signal", signal)
	if a.hasOutput {
		return nil
	}
	refVote := &signal.Votes[0]
	switch signal.Type {
	case agrPkg.SignalFork:
		a.data.recv.ReportForkVote(refVote, &signal.Votes[1])
	case agrPkg.SignalDecide:
		a.hasOutput = true
		a.data.recv.ConfirmBlock(refVote.BlockHash, signal.Votes)
		close(a.doneChan)
		a.doneChan = nil
	case agrPkg.SignalLock:
		switch signal.VType {
		case types.VotePreCom:
			if len(a.fastForward) > 0 {
				break
			}
			// Condition 1.
			if a.data.period >= signal.Period &&
				signal.Period > a.data.lockIter &&
				refVote.BlockHash != a.data.lockValue {
				a.data.lockValue = refVote.BlockHash
				a.data.lockIter = signal.Period
				break
			}
			// Condition 2.
			if signal.Period > a.data.period {
				if signal.Period > a.data.lockIter {
					a.data.lockValue = refVote.BlockHash
					a.data.lockIter = signal.Period
				}
				a.fastForward <- signal.Period
				break
			}
		case types.VoteFast:
			if a.hasOutput {
				break
			}
			if a.hasVoteFast {
				break
			}
			a.data.recv.ProposeVote(types.NewVote(
				types.VoteFastCom, refVote.BlockHash, signal.Period))
			a.data.lockValue = refVote.BlockHash
			a.data.lockIter = 1
			a.hasVoteFast = true
		default:
			panic(fmt.Errorf("unknwon vote type for signal: %s, %s", refVote,
				signal))
		}
	case agrPkg.SignalForward:
		switch signal.VType {
		case types.VoteCom:
			// Condition 3.
			if len(a.fastForward) > 0 {
				break
			}
			if signal.Period >= a.data.period {
				hashes := common.Hashes{}
				for h := range addPullBlocks(signal.Votes) {
					hashes = append(hashes, h)
				}
				if len(hashes) > 0 {
					a.data.recv.PullBlocks(hashes)
				}
				a.fastForward <- signal.Period + 1
			}
		default:
			panic(fmt.Errorf("unknwon vote type for signal: %s, %s", refVote,
				signal))
		}
	default:
		panic(fmt.Errorf("unknown signal type: %v", signal.Type))
	}
	return nil
}

func (a *agreement) done() <-chan struct{} {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.doneChan == nil {
		return closedchan
	}
	a.data.lock.Lock()
	defer a.data.lock.Unlock()
	select {
	case period := <-a.fastForward:
		if period <= a.data.period {
			break
		}
		a.data.period = period
		a.state = newPreCommitState(a.data)
		close(a.doneChan)
		a.doneChan = make(chan struct{})
		return closedchan
	default:
	}
	return a.doneChan
}

func (a *agreement) confirmed() bool {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.hasOutput
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
			a.data.recv.ReportForkBlock(b, block)
			return &ErrFork{block.ProposerID, b.Hash, block.Hash}
		}
		return nil
	}
	if err := a.data.leader.processBlock(block); err != nil {
		return err
	}
	a.data.blocks[block.ProposerID] = block
	a.addCandidateBlockNoLock(block)
	if block.ProposerID != a.data.ID &&
		(a.state.state() == stateFast || a.state.state() == stateFastVote) &&
		block.ProposerID == a.leader() {
		go func() {
			for func() bool {
				a.lock.RLock()
				defer a.lock.RUnlock()
				if a.state.state() != stateFast && a.state.state() != stateFastVote {
					return false
				}
				block, exist := a.data.blocks[a.leader()]
				if !exist {
					return true
				}
				a.data.lock.RLock()
				defer a.data.lock.RUnlock()
				ok, err := a.data.leader.validLeader(block)
				if err != nil {
					fmt.Println("Error checking validLeader for Fast BA",
						"error", err, "block", block)
					return false
				}
				if ok {
					a.data.recv.ProposeVote(
						types.NewVote(types.VoteFast, block.Hash, a.data.period))
					return false
				}
				return true
			}() {
				// TODO(jimmy): retry interval should be related to configurations.
				time.Sleep(250 * time.Millisecond)
			}
		}()
	}
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

func (a *agreement) findCandidateBlockNoLock(
	hash common.Hash) (*types.Block, bool) {
	b, e := a.candidateBlock[hash]
	return b, e
}

// find a block in both candidate blocks and pending blocks in leader-selector.
// A block might be confirmed by others while we can't verify its validity.
func (a *agreement) findBlockNoLock(hash common.Hash) (*types.Block, bool) {
	b, e := a.findCandidateBlockNoLock(hash)
	if !e {
		b, e = a.data.leader.findPendingBlock(hash)
	}
	return b, e
}
