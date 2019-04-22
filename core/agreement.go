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

// closedchan is a reusable closed channel.
var closedchan = make(chan struct{})

func init() {
	close(closedchan)
}

// Errors for agreement module.
var (
	ErrInvalidVote                   = fmt.Errorf("invalid vote")
	ErrNotInNotarySet                = fmt.Errorf("not in notary set")
	ErrIncorrectVoteSignature        = fmt.Errorf("incorrect vote signature")
	ErrIncorrectVotePartialSignature = fmt.Errorf("incorrect vote psig")
	ErrMismatchBlockPosition         = fmt.Errorf("mismatch block position")
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
	ReportForkVote(v1, v2 *types.Vote)
	ReportForkBlock(b1, b2 *types.Block)
	VerifyPartialSignature(vote *types.Vote) (bool, bool)
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
	isLeader     bool
	leader       *leaderSelector
	lockValue    common.Hash
	lockIter     uint64
	period       uint64
	requiredVote int
	votes        map[uint64][]map[types.NodeID]*types.Vote
	lock         sync.RWMutex
	blocks       map[types.NodeID]*types.Block
	blocksLock   sync.Mutex
}

// agreement is the agreement protocal describe in the Crypto Shuffle Algorithm.
type agreement struct {
	state                  agreementState
	data                   *agreementData
	aID                    *atomic.Value
	doneChan               chan struct{}
	notarySet              map[types.NodeID]struct{}
	hasVoteFast            bool
	hasOutput              bool
	lock                   sync.RWMutex
	pendingBlock           []pendingBlock
	pendingVote            []pendingVote
	pendingAgreementResult map[types.Position]*types.AgreementResult
	candidateBlock         map[common.Hash]*types.Block
	fastForward            chan uint64
	signer                 *utils.Signer
	logger                 common.Logger
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
		aID:                    &atomic.Value{},
		pendingAgreementResult: make(map[types.Position]*types.AgreementResult),
		candidateBlock:         make(map[common.Hash]*types.Block),
		fastForward:            make(chan uint64, 1),
		signer:                 signer,
		logger:                 logger,
	}
	agreement.stop()
	return agreement
}

// restart the agreement
func (a *agreement) restart(
	notarySet map[types.NodeID]struct{},
	threshold int, aID types.Position, leader types.NodeID,
	crs common.Hash) {
	if !func() bool {
		a.lock.Lock()
		defer a.lock.Unlock()
		if !isStop(aID) {
			oldAID := a.agreementID()
			if !isStop(oldAID) && !aID.Newer(oldAID) {
				return false
			}
		}
		a.logger.Debug("Restarting BA",
			"notarySet", notarySet, "position", aID, "leader", leader)
		a.data.lock.Lock()
		defer a.data.lock.Unlock()
		a.data.blocksLock.Lock()
		defer a.data.blocksLock.Unlock()
		a.data.votes = make(map[uint64][]map[types.NodeID]*types.Vote)
		a.data.votes[1] = newVoteListMap()
		a.data.period = 2
		a.data.blocks = make(map[types.NodeID]*types.Block)
		a.data.requiredVote = threshold
		a.data.leader.restart(crs)
		a.data.lockValue = types.SkipBlockHash
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
		a.notarySet = notarySet
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

	var result *types.AgreementResult
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		newPendingAgreementResult := make(
			map[types.Position]*types.AgreementResult)
		for pos, agr := range a.pendingAgreementResult {
			if pos.Newer(aID) {
				newPendingAgreementResult[pos] = agr
			} else if pos == aID {
				result = agr
			}
		}
		a.pendingAgreementResult = newPendingAgreementResult
	}()

	expireTime := time.Now().Add(-10 * time.Second)
	replayBlock := make([]*types.Block, 0)
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		newPendingBlock := make([]pendingBlock, 0)
		for _, pending := range a.pendingBlock {
			if aID.Newer(pending.block.Position) {
				continue
			} else if pending.block.Position == aID {
				if result == nil ||
					result.Position.Round < DKGDelayRound ||
					result.BlockHash == pending.block.Hash {
					replayBlock = append(replayBlock, pending.block)
				}
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
			if aID.Newer(pending.vote.Position) {
				continue
			} else if pending.vote.Position == aID {
				if result == nil || result.Position.Round < DKGDelayRound {
					replayVote = append(replayVote, pending.vote)
				}
			} else if pending.receivedTime.After(expireTime) {
				newPendingVote = append(newPendingVote, pending)
			}
		}
		a.pendingVote = newPendingVote
	}()

	for _, block := range replayBlock {
		if err := a.processBlock(block); err != nil {
			a.logger.Error("Failed to process block when restarting agreement",
				"block", block)
		}
	}

	if result != nil {
		if err := a.processAgreementResult(result); err != nil {
			a.logger.Error("Failed to process agreement result when retarting",
				"result", result)
		}
	}

	for _, vote := range replayVote {
		if err := a.processVote(vote); err != nil {
			a.logger.Error("Failed to process vote when restarting agreement",
				"vote", vote)
		}
	}
}

func (a *agreement) stop() {
	a.restart(make(map[types.NodeID]struct{}), int(math.MaxInt32),
		types.Position{
			Height: math.MaxUint64,
		},
		types.NodeID{}, common.Hash{})
}

func isStop(aID types.Position) bool {
	return aID.Height == math.MaxUint64
}

// clocks returns how many time this state is required.
func (a *agreement) clocks() int {
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	scale := int(a.data.period) - 1
	if a.state.state() == stateForward {
		scale = 1
	}
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

func (a *agreement) sanityCheck(vote *types.Vote) error {
	if vote.Type >= types.MaxVoteType {
		return ErrInvalidVote
	}
	ok, err := utils.VerifyVoteSignature(vote)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectVoteSignature
	}
	if vote.Position.Round != a.agreementID().Round {
		// TODO(jimmy): maybe we can verify partial signature at agreement-mgr.
		return nil
	}
	if ok, report := a.data.recv.VerifyPartialSignature(vote); !ok {
		if report {
			return ErrIncorrectVotePartialSignature
		}
		return ErrSkipButNoError
	}
	return nil
}

func (a *agreement) checkForkVote(vote *types.Vote) (
	alreadyExist bool, err error) {
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	if votes, exist := a.data.votes[vote.Period]; exist {
		if oldVote, exist := votes[vote.Type][vote.ProposerID]; exist {
			alreadyExist = true
			if vote.BlockHash != oldVote.BlockHash {
				a.data.recv.ReportForkVote(oldVote, vote)
				err = &ErrForkVote{vote.ProposerID, oldVote, vote}
				return
			}
		}
	}
	return
}

// prepareVote prepares a vote.
func (a *agreement) prepareVote(vote *types.Vote) (err error) {
	vote.Position = a.agreementID()
	err = a.signer.SignVote(vote)
	return
}

func (a *agreement) updateFilter(filter *utils.VoteFilter) {
	if isStop(a.agreementID()) {
		return
	}
	a.lock.RLock()
	defer a.lock.RUnlock()
	a.data.lock.RLock()
	defer a.data.lock.RUnlock()
	filter.Confirm = a.hasOutput
	filter.LockIter = a.data.lockIter
	filter.Period = a.data.period
	filter.Position.Height = a.agreementID().Height
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
		// Hacky way to not drop first votes when round just begins.
		if vote.Position.Round == aID.Round {
			a.pendingVote = append(a.pendingVote, pendingVote{
				vote:         vote,
				receivedTime: time.Now().UTC(),
			})
			return nil
		}
		return ErrSkipButNoError
	}
	if vote.Position != aID {
		if aID.Newer(vote.Position) {
			return nil
		}
		a.pendingVote = append(a.pendingVote, pendingVote{
			vote:         vote,
			receivedTime: time.Now().UTC(),
		})
		return nil
	}
	exist, err := a.checkForkVote(vote)
	if err != nil {
		return err
	}
	if exist {
		return nil
	}

	a.data.lock.Lock()
	defer a.data.lock.Unlock()
	if _, exist := a.data.votes[vote.Period]; !exist {
		a.data.votes[vote.Period] = newVoteListMap()
	}
	if _, exist := a.data.votes[vote.Period][vote.Type][vote.ProposerID]; exist {
		return nil
	}
	a.data.votes[vote.Period][vote.Type][vote.ProposerID] = vote
	if !a.hasOutput &&
		(vote.Type == types.VoteCom ||
			vote.Type == types.VoteFast ||
			vote.Type == types.VoteFastCom) {
		if hash, ok := a.data.countVoteNoLock(vote.Period, vote.Type); ok &&
			hash != types.SkipBlockHash {
			if vote.Type == types.VoteFast {
				if !a.hasVoteFast {
					if a.state.state() == stateFast ||
						a.state.state() == stateFastVote {
						a.data.recv.ProposeVote(
							types.NewVote(types.VoteFastCom, hash, vote.Period))
						a.hasVoteFast = true

					}
					if a.data.lockIter == 0 {
						a.data.lockValue = hash
						a.data.lockIter = 1
					}
				}
			} else {
				a.hasOutput = true
				a.data.recv.ConfirmBlock(hash,
					a.data.votes[vote.Period][vote.Type])
				if a.doneChan != nil {
					close(a.doneChan)
					a.doneChan = nil
				}
			}
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
		if vote.Period < a.data.lockIter {
			// This PreCom is useless for us.
			return nil
		}
		if hash, ok := a.data.countVoteNoLock(vote.Period, vote.Type); ok &&
			hash != types.SkipBlockHash {
			// Condition 1.
			if vote.Period > a.data.lockIter {
				a.data.lockValue = hash
				a.data.lockIter = vote.Period
			}
			// Condition 2.
			if vote.Period > a.data.period {
				a.fastForward <- vote.Period
				if a.doneChan != nil {
					close(a.doneChan)
					a.doneChan = nil
				}
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
				if vote.BlockHash == types.NullBlockHash ||
					vote.BlockHash == types.SkipBlockHash {
					continue
				}
				if _, found := a.findCandidateBlockNoLock(vote.BlockHash); !found {
					hashes = append(hashes, vote.BlockHash)
				}
			}
		}
		addPullBlocks(types.VotePreCom)
		addPullBlocks(types.VoteCom)
		if len(hashes) > 0 {
			a.data.recv.PullBlocks(hashes)
		}
		a.fastForward <- vote.Period + 1
		if a.doneChan != nil {
			close(a.doneChan)
			a.doneChan = nil
		}
		return nil
	}
	return nil
}

func (a *agreement) processFinalizedBlock(block *types.Block) {
	a.lock.Lock()
	defer a.lock.Unlock()
	if a.hasOutput {
		return
	}
	aID := a.agreementID()
	if aID.Older(block.Position) {
		return
	}
	a.addCandidateBlockNoLock(block)
	a.hasOutput = true
	a.data.lock.Lock()
	defer a.data.lock.Unlock()
	a.data.recv.ConfirmBlock(block.Hash, nil)
	if a.doneChan != nil {
		close(a.doneChan)
		a.doneChan = nil
	}
}

func (a *agreement) processAgreementResult(result *types.AgreementResult) error {
	a.lock.Lock()
	defer a.lock.Unlock()
	aID := a.agreementID()
	if result.Position.Older(aID) {
		return nil
	} else if result.Position.Newer(aID) {
		a.pendingAgreementResult[result.Position] = result
		return nil
	}
	if a.hasOutput {
		return nil
	}
	a.data.lock.Lock()
	defer a.data.lock.Unlock()
	if _, exist := a.findCandidateBlockNoLock(result.BlockHash); !exist {
		a.data.recv.PullBlocks(common.Hashes{result.BlockHash})
	}
	a.hasOutput = true
	a.data.recv.ConfirmBlock(result.BlockHash, nil)
	if a.doneChan != nil {
		close(a.doneChan)
		a.doneChan = nil
	}
	return nil
}

func (a *agreement) done() <-chan struct{} {
	a.lock.Lock()
	defer a.lock.Unlock()
	select {
	case period := <-a.fastForward:
		a.data.lock.Lock()
		defer a.data.lock.Unlock()
		if period <= a.data.period {
			break
		}
		a.data.setPeriod(period)
		a.state = newPreCommitState(a.data)
		a.doneChan = make(chan struct{})
		return closedchan
	default:
	}
	if a.doneChan == nil {
		return closedchan
	}
	return a.doneChan
}

func (a *agreement) confirmed() bool {
	a.lock.RLock()
	defer a.lock.RUnlock()
	return a.confirmedNoLock()
}

func (a *agreement) confirmedNoLock() bool {
	return a.hasOutput
}

// processBlock is the entry point for processing Block.
func (a *agreement) processBlock(block *types.Block) error {
	checkSkip := func() bool {
		aID := a.agreementID()
		if block.Position != aID {
			// Agreement module has stopped.
			if !isStop(aID) {
				if aID.Newer(block.Position) {
					return true
				}
			}
		}
		return false
	}
	if checkSkip() {
		return nil
	}
	if err := utils.VerifyBlockSignature(block); err != nil {
		return err
	}

	a.lock.Lock()
	defer a.lock.Unlock()
	a.data.blocksLock.Lock()
	defer a.data.blocksLock.Unlock()
	aID := a.agreementID()
	// a.agreementID might change during lock, so we need to checkSkip again.
	if checkSkip() {
		return nil
	} else if aID != block.Position {
		a.pendingBlock = append(a.pendingBlock, pendingBlock{
			block:        block,
			receivedTime: time.Now().UTC(),
		})
		return nil
	} else if a.confirmedNoLock() {
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
				if aID != a.agreementID() {
					return false
				}
				a.lock.RLock()
				defer a.lock.RUnlock()
				if a.state.state() != stateFast && a.state.state() != stateFastVote {
					return false
				}
				a.data.lock.RLock()
				defer a.data.lock.RUnlock()
				a.data.blocksLock.Lock()
				defer a.data.blocksLock.Unlock()
				block, exist := a.data.blocks[a.leader()]
				if !exist {
					return true
				}
				ok, err := a.data.leader.validLeader(block, a.data.leader.hashCRS)
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
