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

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// blockPool is a slice of heap of blocks, indexed by chainID,
// and the heap is sorted based on heights of blocks.
type blockPool []types.ByPosition

// newBlockPool constructs a blockPool.
func newBlockPool(chainNum uint32) (pool blockPool) {
	pool = make(blockPool, chainNum)
	for _, p := range pool {
		heap.Init(&p)
	}
	return
}

// resize the pool if new chain is added.
func (p *blockPool) resize(num uint32) {
	if uint32(len(*p)) < num {
		return
	}
	newPool := make([]types.ByPosition, num)
	copy(newPool, *p)
	for i := uint32(len(*p)); i < num; i++ {
		newChain := types.ByPosition{}
		heap.Init(&newChain)
		newPool[i] = newChain
	}
	*p = newPool
}

// addBlock adds a block into pending set and make sure these
// blocks are sorted by height.
func (p blockPool) addBlock(b *types.Block) {
	heap.Push(&p[b.Position.ChainID], b)
}

// purgeBlocks purge blocks of that chain with less-or-equal height.
// NOTE: we won't check the validity of 'chainID', the caller should
//       be sure what he is expecting.
func (p blockPool) purgeBlocks(chainID uint32, height uint64) {
	for {
		if len(p[chainID]) == 0 || p[chainID][0].Position.Height > height {
			break
		}
		heap.Pop(&p[chainID])
	}
}

// tip get the blocks with lowest height of the chain if any.
func (p blockPool) tip(chainID uint32) *types.Block {
	if len(p[chainID]) == 0 {
		return nil
	}
	return p[chainID][0]
}

// removeTip removes block with lowest height of the specified chain.
func (p blockPool) removeTip(chainID uint32) {
	if len(p[chainID]) > 0 {
		heap.Pop(&p[chainID])
	}
}
