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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for compaction chain module.
var (
	ErrBlockNotRegistered = fmt.Errorf(
		"block not registered")
)

type compactionChain struct {
	blocks        map[common.Hash]*types.Block
	pendingBlocks []*types.Block
	blocksLock    sync.RWMutex
	prevBlockLock sync.RWMutex
	prevBlock     *types.Block
}

func newCompactionChain() *compactionChain {
	return &compactionChain{
		blocks: make(map[common.Hash]*types.Block),
	}
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
	if prevBlock != nil {
		block.Finalization.Height = prevBlock.Finalization.Height + 1
	} else {
		block.Finalization.Height = 1
	}
	cc.prevBlockLock.Lock()
	defer cc.prevBlockLock.Unlock()
	cc.prevBlock = block
	cc.blocksLock.Lock()
	defer cc.blocksLock.Unlock()
	cc.pendingBlocks = append(cc.pendingBlocks, block)
	return nil
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
