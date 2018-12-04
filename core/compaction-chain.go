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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// Errors for compaction chain module.
var (
	ErrBlockNotRegistered = fmt.Errorf(
		"block not registered")
	ErrNotInitiazlied = fmt.Errorf(
		"not initialized")
	ErrTSigNotReady = fmt.Errorf(
		"tsig not ready")
	ErrIncorrectBlockRandomnessResult = fmt.Errorf(
		"incorrect block randomness result")
)

const maxPendingPeriod = 3 * time.Second

type pendingRandomnessResult struct {
	receivedTime time.Time
	randResult   *types.BlockRandomnessResult
}

type finalizedBlockHeap = types.ByFinalizationHeight

type compactionChain struct {
	gov               Governance
	chainUnsynced     uint32
	tsigVerifier      *TSigVerifierCache
	blocks            map[common.Hash]*types.Block
	blockRandomness   map[common.Hash][]byte
	pendingRandomness map[common.Hash]pendingRandomnessResult
	pendingBlocks     []*types.Block
	lock              sync.RWMutex
	prevBlock         *types.Block
}

func newCompactionChain(gov Governance) *compactionChain {
	return &compactionChain{
		gov:               gov,
		tsigVerifier:      NewTSigVerifierCache(gov, 7),
		blocks:            make(map[common.Hash]*types.Block),
		blockRandomness:   make(map[common.Hash][]byte),
		pendingRandomness: make(map[common.Hash]pendingRandomnessResult),
	}
}

// init the compaction chain module with a finalized block, or just an empty
// block for bootstrap case.
func (cc *compactionChain) init(initBlock *types.Block) {
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.prevBlock = initBlock
	cc.pendingBlocks = []*types.Block{}
	// It's the bootstrap case, compactionChain would only deliver blocks until
	// tips of all chains are received.
	if initBlock.Finalization.Height == 0 {
		cc.chainUnsynced = cc.gov.Configuration(uint64(0)).NumChains
	}
}

func (cc *compactionChain) registerBlock(block *types.Block) {
	if cc.blockRegistered(block.Hash) {
		return
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.blocks[block.Hash] = block
	if rand, exist := cc.pendingRandomness[block.Hash]; exist {
		cc.blockRandomness[rand.randResult.BlockHash] = rand.randResult.Randomness
		delete(cc.pendingRandomness, block.Hash)
	}
}

func (cc *compactionChain) blockRegistered(hash common.Hash) bool {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	return cc.blockRegisteredNoLock(hash)
}

func (cc *compactionChain) blockRegisteredNoLock(
	hash common.Hash) (exist bool) {
	_, exist = cc.blocks[hash]
	return
}

func (cc *compactionChain) processBlock(block *types.Block) error {
	prevBlock := cc.lastDeliveredBlock()
	if prevBlock == nil {
		return ErrNotInitiazlied
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	if prevBlock.Finalization.Height == 0 && block.Position.Height == 0 {
		cc.chainUnsynced--
	}
	cc.pendingBlocks = append(cc.pendingBlocks, block)
	return nil
}

func (cc *compactionChain) extractBlocks() []*types.Block {
	prevBlock := cc.lastDeliveredBlock()

	// Check if we're synced.
	if !func() bool {
		cc.lock.RLock()
		defer cc.lock.RUnlock()
		if len(cc.pendingBlocks) == 0 {
			return false
		}
		// Finalization.Height == 0 is syncing from bootstrap.
		if prevBlock.Finalization.Height == 0 {
			return cc.chainUnsynced == 0
		}
		return true
	}() {
		return []*types.Block{}
	}
	deliveringBlocks := make([]*types.Block, 0)
	cc.lock.Lock()
	defer cc.lock.Unlock()
	var block *types.Block
	for len(cc.pendingBlocks) > 0 &&
		(len(cc.blockRandomness[cc.pendingBlocks[0].Hash]) != 0 ||
			cc.pendingBlocks[0].Position.Round == 0) {
		delete(cc.blocks, cc.pendingBlocks[0].Hash)
		block, cc.pendingBlocks = cc.pendingBlocks[0], cc.pendingBlocks[1:]
		block.Finalization.ParentHash = prevBlock.Hash
		block.Finalization.Height = prevBlock.Finalization.Height + 1
		if block.Position.Round != 0 {
			block.Finalization.Randomness = cc.blockRandomness[block.Hash]
			delete(cc.blockRandomness, block.Hash)
		}
		deliveringBlocks = append(deliveringBlocks, block)
		prevBlock = block
	}
	cc.prevBlock = prevBlock
	return deliveringBlocks
}

func (cc *compactionChain) verifyRandomness(
	blockHash common.Hash, round uint64, randomness []byte) (bool, error) {
	if round == 0 {
		return len(randomness) == 0, nil
	}
	// Randomness is not available at round 0.
	v, ok, err := cc.tsigVerifier.UpdateAndGet(round)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrTSigNotReady
	}
	return v.VerifySignature(blockHash, crypto.Signature{
		Type:      "bls",
		Signature: randomness}), nil
}

func (cc *compactionChain) processFinalizedBlock(block *types.Block) error {
	if block.Finalization.Height <= cc.lastDeliveredBlock().Finalization.Height {
		return nil
	}
	// Block of round 0 should not have randomness.
	if block.Position.Round == 0 && len(block.Finalization.Randomness) != 0 {
		return nil
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	// The randomness result is missed previously.
	if cc.blockRegisteredNoLock(block.Hash) {
		ok, err := cc.verifyRandomness(
			block.Hash, block.Position.Round, block.Finalization.Randomness)
		if err != nil {
			return err
		}
		if ok {
			cc.blockRandomness[block.Hash] = block.Finalization.Randomness
		}
	}
	return nil
}

func (cc *compactionChain) processBlockRandomnessResult(
	rand *types.BlockRandomnessResult) error {
	ok, err := cc.verifyRandomness(
		rand.BlockHash, rand.Position.Round, rand.Randomness)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectBlockRandomnessResult
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	if !cc.blockRegisteredNoLock(rand.BlockHash) {
		cc.purgePending()
		cc.pendingRandomness[rand.BlockHash] = pendingRandomnessResult{
			receivedTime: time.Now(),
			randResult:   rand,
		}
		return ErrBlockNotRegistered
	}
	cc.blockRandomness[rand.BlockHash] = rand.Randomness
	return nil
}

func (cc *compactionChain) purgePending() {
	now := time.Now()
	for key, rand := range cc.pendingRandomness {
		if now.After(rand.receivedTime.Add(maxPendingPeriod)) {
			delete(cc.pendingRandomness, key)
		}
	}
}

// lastDeliveredBlock returns the last delivered block, or the one used to
// initialize this module.
func (cc *compactionChain) lastDeliveredBlock() *types.Block {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	return cc.prevBlock
}

// lastPendingBlock returns the last pending block.
func (cc *compactionChain) lastPendingBlock() *types.Block {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	if len(cc.pendingBlocks) > 0 {
		return cc.pendingBlocks[0]
	}
	return nil
}
