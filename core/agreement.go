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
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// Errors for agreement module.
var (
	ErrNotValidator           = fmt.Errorf("not a validaotr")
	ErrIncorrectVoteSignature = fmt.Errorf("incorrect vote signature")
	ErrForkVote               = fmt.Errorf("fork vote")
)

// ErrFork for fork error in agreement.
type ErrFork struct {
	vID      types.ValidatorID
	old, new common.Hash
}

func (e *ErrFork) Error() string {
	return fmt.Sprintf("fork is found for %s, old %s, new %s",
		e.vID.String(), e.old, e.new)
}

type blockProposerFn func() *types.Block

func newVoteListMap() []map[types.ValidatorID]*types.Vote {
	listMap := make([]map[types.ValidatorID]*types.Vote, types.MaxVoteType)
	for idx := range listMap {
		listMap[idx] = make(map[types.ValidatorID]*types.Vote)
	}
	return listMap
}

// agreementReceiver is the interface receiving agreement event.
type agreementReceiver interface {
	proposeVote(vote *types.Vote)
	proposeBlock(common.Hash)
	confirmBlock(common.Hash)
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

	ID            types.ValidatorID
	leader        *leaderSelector
	defaultBlock  common.Hash
	period        uint64
	requiredVote  int
	votes         map[uint64][]map[types.ValidatorID]*types.Vote
	votesLock     sync.RWMutex
	blocks        map[types.ValidatorID]*types.Block
	blocksLock    sync.Mutex
	blockProposer blockProposerFn
}

// agreement is the agreement protocal describe in the Crypto Shuffle Algorithm.
type agreement struct {
	state          agreementState
	data           *agreementData
	aID            *atomic.Value
	validators     map[types.ValidatorID]struct{}
	sigToPub       SigToPubFn
	hasOutput      bool
	lock           sync.RWMutex
	pendingBlock   []pendingBlock
	pendingVote    []pendingVote
	candidateBlock map[common.Hash]*types.Block
}

// newAgreement creates a agreement instance.
func newAgreement(
	ID types.ValidatorID,
	recv agreementReceiver,
	validators types.ValidatorIDs,
	leader *leaderSelector,
	sigToPub SigToPubFn,
	blockProposer blockProposerFn) *agreement {
	agreement := &agreement{
		data: &agreementData{
			recv:          recv,
			ID:            ID,
			leader:        leader,
			blockProposer: blockProposer,
		},
		aID:            &atomic.Value{},
		sigToPub:       sigToPub,
		candidateBlock: make(map[common.Hash]*types.Block),
	}
	agreement.restart(validators, types.Position{})
	return agreement
}

// terminate the current running state.
func (a *agreement) terminate() {
	if a.state != nil {
		a.state.terminate()
	}
}

// restart the agreement
func (a *agreement) restart(validators types.ValidatorIDs, aID types.Position) {
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		a.data.votesLock.Lock()
		defer a.data.votesLock.Unlock()
		a.data.blocksLock.Lock()
		defer a.data.blocksLock.Unlock()
		a.data.votes = make(map[uint64][]map[types.ValidatorID]*types.Vote)
		a.data.votes[1] = newVoteListMap()
		a.data.period = 1
		a.data.blocks = make(map[types.ValidatorID]*types.Block)
		a.data.requiredVote = len(validators)/3*2 + 1
		a.data.leader.restart()
		a.hasOutput = false
		a.state = newPrepareState(a.data)
		a.validators = make(map[types.ValidatorID]struct{})
		for _, v := range validators {
			a.validators[v] = struct{}{}
		}
		a.candidateBlock = make(map[common.Hash]*types.Block)
		a.aID.Store(aID)
	}()

	expireTime := time.Now().Add(-10 * time.Second)
	replayBlock := make([]*types.Block, 0)
	func() {
		a.lock.Lock()
		defer a.lock.Unlock()
		newPendingBlock := make([]pendingBlock, 0)
		for _, pending := range a.pendingBlock {
			if pending.block.Position == aID {
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
			if pending.vote.Position == aID {
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

// clocks returns how many time this state is required.
func (a *agreement) clocks() int {
	return a.state.clocks()
}

// agreementID returns the current agreementID.
func (a *agreement) agreementID() types.Position {
	return a.aID.Load().(types.Position)
}

// nextState is called at the spcifi clock time.
func (a *agreement) nextState() (err error) {
	a.state, err = a.state.nextState()
	return
}

func (a *agreement) sanityCheck(vote *types.Vote) error {
	if exist := func() bool {
		a.lock.RLock()
		defer a.lock.RUnlock()
		_, exist := a.validators[vote.ProposerID]
		return exist
	}(); !exist {
		return ErrNotValidator
	}
	ok, err := verifyVoteSignature(vote, a.sigToPub)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectVoteSignature
	}
	if exist := func() bool {
		a.data.votesLock.RLock()
		defer a.data.votesLock.RUnlock()
		_, exist := a.data.votes[vote.Period]
		return exist
	}(); exist {
		if oldVote, exist :=
			a.data.votes[vote.Period][vote.Type][vote.ProposerID]; exist {
			if vote.BlockHash != oldVote.BlockHash {
				return ErrForkVote
			}
		}
	}
	return nil
}

// prepareVote prepares a vote.
func (a *agreement) prepareVote(vote *types.Vote, prv crypto.PrivateKey) (
	err error) {
	vote.ProposerID = a.data.ID
	vote.Position = a.agreementID()
	hash := hashVote(vote)
	vote.Signature, err = prv.Sign(hash)
	return
}

// processVote is the entry point for processing Vote.
func (a *agreement) processVote(vote *types.Vote) error {
	if err := a.sanityCheck(vote); err != nil {
		return err
	}
	if vote.Position != a.agreementID() {
		a.pendingVote = append(a.pendingVote, pendingVote{
			vote:         vote,
			receivedTime: time.Now().UTC(),
		})
		return nil
	}
	if func() bool {
		a.data.votesLock.Lock()
		defer a.data.votesLock.Unlock()
		if _, exist := a.data.votes[vote.Period]; !exist {
			a.data.votes[vote.Period] = newVoteListMap()
		}
		a.data.votes[vote.Period][vote.Type][vote.ProposerID] = vote
		if !a.hasOutput && vote.Type == types.VoteConfirm {
			if len(a.data.votes[vote.Period][types.VoteConfirm]) >=
				a.data.requiredVote {
				a.hasOutput = true
				a.data.recv.confirmBlock(vote.BlockHash)
			}
		}
		return true
	}() {
		return a.state.receiveVote()
	}
	return nil
}

// prepareBlok prepares a block.
func (a *agreement) prepareBlock(
	block *types.Block, prv crypto.PrivateKey) error {
	return a.data.leader.prepareBlock(block, prv)
}

// processBlock is the entry point for processing Block.
func (a *agreement) processBlock(block *types.Block) error {
	a.data.blocksLock.Lock()
	defer a.data.blocksLock.Unlock()
	if b, exist := a.data.blocks[block.ProposerID]; exist {
		if b.Hash != block.Hash {
			return &ErrFork{block.ProposerID, b.Hash, block.Hash}
		}
		return nil
	}
	if block.Position != a.agreementID() {
		a.pendingBlock = append(a.pendingBlock, pendingBlock{
			block:        block,
			receivedTime: time.Now().UTC(),
		})
		return nil
	}
	if err := a.data.leader.processBlock(block); err != nil {
		return err
	}
	a.data.blocks[block.ProposerID] = block
	a.addCandidateBlock(block)
	return nil
}

func (a *agreement) addCandidateBlock(block *types.Block) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.candidateBlock[block.Hash] = block
}

func (a *agreement) findCandidateBlock(hash common.Hash) (*types.Block, bool) {
	a.lock.RLock()
	defer a.lock.RUnlock()
	b, e := a.candidateBlock[hash]
	return b, e
}

func (a *agreementData) countVote(period uint64, voteType types.VoteType) (
	blockHash common.Hash, ok bool) {
	a.votesLock.RLock()
	defer a.votesLock.RUnlock()
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
