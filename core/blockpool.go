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

	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// blockPool is a heaped slice of blocks, indexed by chainID, and each in it is
// sorted by block's height.
type blockPool []types.ByPosition

func newBlockPool(chainNum uint32) (pool blockPool) {
	pool = make(blockPool, chainNum)
	for _, p := range pool {
		heap.Init(&p)
	}
	return
}

func (p *blockPool) resize(num uint32) {
	if uint32(len(*p)) >= num {
		// Do nothing If the origin size is larger.
		return
	}
	newPool := make(blockPool, num)
	copy(newPool, *p)
	for i := uint32(len(*p)); i < num; i++ {
		newChain := types.ByPosition{}
		heap.Init(&newChain)
		newPool[i] = newChain
	}
	*p = newPool
}

// addBlock adds a block into pool and sorts them by height.
func (p blockPool) addBlock(b *types.Block) {
	heap.Push(&p[b.Position.ChainID], b)
}

// purgeBlocks purges blocks of a specified chain with less-or-equal heights.
// NOTE: "chainID" is not checked here, this should be ensured by the called.
func (p blockPool) purgeBlocks(chainID uint32, height uint64) {
	for len(p[chainID]) > 0 && p[chainID][0].Position.Height <= height {
		heap.Pop(&p[chainID])
	}
}

// tip returns block with the smallest height, nil if no existing block.
func (p blockPool) tip(chainID uint32) *types.Block {
	if len(p[chainID]) == 0 {
		return nil
	}
	return p[chainID][0]
}

// removeTip removes block with lowest height of a specified chain.
func (p blockPool) removeTip(chainID uint32) {
	if len(p[chainID]) > 0 {
		heap.Pop(&p[chainID])
	}
}
