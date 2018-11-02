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
	"container/heap"
	"fmt"
	"sync"

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
)

type finalizedBlockHeap = types.ByFinalizationHeight

type compactionChain struct {
	gov                    Governance
	chainUnsynced          uint32
	tsigVerifier           *TSigVerifierCache
	blocks                 map[common.Hash]*types.Block
	pendingBlocks          []*types.Block
	pendingFinalizedBlocks *finalizedBlockHeap
	lock                   sync.RWMutex
	prevBlock              *types.Block
}

func newCompactionChain(gov Governance) *compactionChain {
	pendingFinalizedBlocks := &finalizedBlockHeap{}
	heap.Init(pendingFinalizedBlocks)
	return &compactionChain{
		gov:                    gov,
		tsigVerifier:           NewTSigVerifierCache(gov, 7),
		blocks:                 make(map[common.Hash]*types.Block),
		pendingFinalizedBlocks: pendingFinalizedBlocks,
	}
}

func (cc *compactionChain) init(initBlock *types.Block) {
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.prevBlock = initBlock
	cc.pendingBlocks = []*types.Block{}
	if initBlock.Finalization.Height == 0 {
		cc.chainUnsynced = cc.gov.Configuration(uint64(0)).NumChains
		cc.pendingBlocks = append(cc.pendingBlocks, initBlock)
	}
}

func (cc *compactionChain) registerBlock(block *types.Block) {
	if cc.blockRegistered(block.Hash) {
		return
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.blocks[block.Hash] = block
}

func (cc *compactionChain) blockRegistered(hash common.Hash) (exist bool) {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	_, exist = cc.blocks[hash]
	return
}

func (cc *compactionChain) processBlock(block *types.Block) error {
	prevBlock := cc.lastBlock()
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
	prevBlock := cc.lastBlock()

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
		if prevBlock.Hash != cc.pendingBlocks[0].Hash {
			return false
		}
		return true
	}() {
		return []*types.Block{}
	}
	deliveringBlocks := make([]*types.Block, 0)
	cc.lock.Lock()
	defer cc.lock.Unlock()
	// cc.pendingBlocks[0] will not be popped and will equal to cc.prevBlock.
	for len(cc.pendingBlocks) > 1 &&
		(len(cc.pendingBlocks[1].Finalization.Randomness) != 0 ||
			cc.pendingBlocks[1].Position.Round == 0) {
		delete(cc.blocks, cc.pendingBlocks[0].Hash)
		cc.pendingBlocks = cc.pendingBlocks[1:]

		block := cc.pendingBlocks[0]
		block.Finalization.ParentHash = prevBlock.Hash
		block.Finalization.Height = prevBlock.Finalization.Height + 1
		deliveringBlocks = append(deliveringBlocks, block)
		prevBlock = block
	}

	cc.prevBlock = prevBlock

	return deliveringBlocks
}

func (cc *compactionChain) processFinalizedBlock(block *types.Block) {
	if block.Finalization.Height <= cc.lastBlock().Finalization.Height {
		return
	}

	// Block of round 0 should not have randomness.
	if block.Position.Round == 0 && len(block.Finalization.Randomness) != 0 {
		return
	}

	cc.lock.Lock()
	defer cc.lock.Unlock()
	heap.Push(cc.pendingFinalizedBlocks, block)

	return
}

func (cc *compactionChain) extractFinalizedBlocks() []*types.Block {
	prevBlock := cc.lastBlock()

	blocks := func() []*types.Block {
		cc.lock.Lock()
		defer cc.lock.Unlock()
		blocks := []*types.Block{}
		prevHeight := prevBlock.Finalization.Height
		for cc.pendingFinalizedBlocks.Len() != 0 {
			tip := (*cc.pendingFinalizedBlocks)[0]
			// Pop blocks that are already confirmed.
			if tip.Finalization.Height <= prevBlock.Finalization.Height {
				heap.Pop(cc.pendingFinalizedBlocks)
				continue
			}
			// Since we haven't verified the finalized block,
			// it is possible to be forked.
			if tip.Finalization.Height == prevHeight ||
				tip.Finalization.Height == prevHeight+1 {
				prevHeight = tip.Finalization.Height
				blocks = append(blocks, tip)
				heap.Pop(cc.pendingFinalizedBlocks)
			} else {
				break
			}
		}
		return blocks
	}()
	toPending := []*types.Block{}
	confirmed := []*types.Block{}
	for _, b := range blocks {
		if b.Hash == prevBlock.Hash &&
			b.Finalization.Height == prevBlock.Finalization.Height {
			continue
		}
		round := b.Position.Round
		if round != 0 {
			// Randomness is not available at round 0.
			v, ok, err := cc.tsigVerifier.UpdateAndGet(round)
			if err != nil {
				continue
			}
			if !ok {
				toPending = append(toPending, b)
				continue
			}
			if ok := v.VerifySignature(b.Hash, crypto.Signature{
				Type:      "bls",
				Signature: b.Finalization.Randomness}); !ok {
				continue
			}
		}
		// Fork resolution: choose block with smaller hash.
		if prevBlock.Finalization.Height == b.Finalization.Height {
			//TODO(jimmy-dexon): remove this panic after test.
			if true {
				// workaround to `go vet` error
				panic(fmt.Errorf(
					"forked finalized block %s,%s", prevBlock.Hash, b.Hash))
			}
			if b.Hash.Less(prevBlock.Hash) {
				confirmed = confirmed[:len(confirmed)-1]
			} else {
				continue
			}
		}
		if b.Finalization.Height-prevBlock.Finalization.Height > 1 {
			toPending = append(toPending, b)
			continue
		}
		confirmed = append(confirmed, b)
		prevBlock = b
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	if len(confirmed) != 0 && cc.prevBlock.Finalization.Height == 0 {
		// Pop the initBlock inserted when bootstrapping.
		cc.pendingBlocks = cc.pendingBlocks[1:]
	}
	cc.prevBlock = prevBlock
	for _, b := range toPending {
		heap.Push(cc.pendingFinalizedBlocks, b)
	}
	return confirmed
}

func (cc *compactionChain) processBlockRandomnessResult(
	rand *types.BlockRandomnessResult) error {
	if !cc.blockRegistered(rand.BlockHash) {
		return ErrBlockNotRegistered
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.blocks[rand.BlockHash].Finalization.Randomness = rand.Randomness
	return nil
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	return cc.prevBlock
}
