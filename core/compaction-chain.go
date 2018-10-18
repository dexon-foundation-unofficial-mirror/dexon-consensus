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
	"container/heap"
	"fmt"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
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
	tsigVerifier           *TSigVerifierCache
	blocks                 map[common.Hash]*types.Block
	pendingBlocks          []*types.Block
	pendingFinalizedBlocks *finalizedBlockHeap
	blocksLock             sync.RWMutex
	prevBlockLock          sync.RWMutex
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
	cc.prevBlockLock.Lock()
	defer cc.prevBlockLock.Unlock()
	cc.prevBlock = initBlock
}

func (cc *compactionChain) registerBlock(block *types.Block) {
	if cc.blockRegistered(block.Hash) {
		return
	}
	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	cc.blocks[block.Hash] = block
}

func (cc *compactionChain) blockRegistered(hash common.Hash) (exist bool) {
	cc.blocksLock.RLock()
	defer cc.blocksLock.RUnlock()
	_, exist = cc.blocks[hash]
	return
}

func (cc *compactionChain) processBlock(block *types.Block) error {
	prevBlock := cc.lastBlock()
	if prevBlock == nil {
		return ErrNotInitiazlied
	}
	block.Finalization.Height = prevBlock.Finalization.Height + 1
	cc.prevBlockLock.Lock()
	defer cc.prevBlockLock.Unlock()
	cc.prevBlock = block
	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	cc.pendingBlocks = append(cc.pendingBlocks, block)
	return nil
}

func (cc *compactionChain) processFinalizedBlock(block *types.Block) {
	if block.Finalization.Height <= cc.lastBlock().Finalization.Height {
		return
	}

	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	heap.Push(cc.pendingFinalizedBlocks, block)

	return
}

func (cc *compactionChain) extractFinalizedBlocks() []*types.Block {
	prevBlock := cc.lastBlock()

	blocks := func() []*types.Block {
		cc.blocksLock.Lock()
		defer cc.blocksLock.Unlock()
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
		// Fork resolution: choose block with smaller hash.
		if prevBlock.Finalization.Height ==
			b.Finalization.Height {
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
	func() {
		if len(confirmed) == 0 {
			return
		}
		cc.prevBlockLock.Lock()
		defer cc.prevBlockLock.Unlock()
		cc.prevBlock = prevBlock
	}()
	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	for _, b := range toPending {
		heap.Push(cc.pendingFinalizedBlocks, b)
	}
	return confirmed
}

func (cc *compactionChain) extractBlocks() []*types.Block {
	deliveringBlocks := make([]*types.Block, 0)
	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	for len(cc.pendingBlocks) != 0 &&
		(len(cc.pendingBlocks[0].Finalization.Randomness) != 0 ||
			cc.pendingBlocks[0].Position.Round == 0) {
		var block *types.Block
		block, cc.pendingBlocks = cc.pendingBlocks[0], cc.pendingBlocks[1:]
		delete(cc.blocks, block.Hash)
		deliveringBlocks = append(deliveringBlocks, block)
	}
	return deliveringBlocks
}

func (cc *compactionChain) processBlockRandomnessResult(
	rand *types.BlockRandomnessResult) error {
	if !cc.blockRegistered(rand.BlockHash) {
		return ErrBlockNotRegistered
	}
	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	cc.blocks[rand.BlockHash].Finalization.Randomness = rand.Randomness
	return nil
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.prevBlockLock.RLock()
	defer cc.prevBlockLock.RUnlock()
	return cc.prevBlock
}
