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
	"sort"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type totalOrderingSyncer struct {
	lock sync.RWMutex

	numChains          uint32
	syncHeight         map[uint32]uint64
	syncDeliverySetIdx int
	pendingBlocks      []*types.Block
	inPendingBlocks    map[common.Hash]struct{}

	bootstrapChain map[uint32]struct{}

	// Data to restore delivery set.
	pendingDeliveryBlocks []*types.Block
	deliverySet           map[int][]*types.Block
	mapToDeliverySet      map[common.Hash]int
}

func newTotalOrderingSyncer(numChains uint32) *totalOrderingSyncer {
	return &totalOrderingSyncer{
		numChains:          numChains,
		syncHeight:         make(map[uint32]uint64),
		syncDeliverySetIdx: -1,
		inPendingBlocks:    make(map[common.Hash]struct{}),
		bootstrapChain:     make(map[uint32]struct{}),
		deliverySet:        make(map[int][]*types.Block),
		mapToDeliverySet:   make(map[common.Hash]int),
	}
}

func (tos *totalOrderingSyncer) synced() bool {
	tos.lock.RLock()
	defer tos.lock.RUnlock()
	return tos.syncDeliverySetIdx != -1
}

func (tos *totalOrderingSyncer) processBlock(
	block *types.Block) (delivered []*types.Block) {
	if tos.synced() {
		if tos.syncHeight[block.Position.ChainID] >= block.Position.Height {
			return
		}
		delivered = append(delivered, block)
		return
	}
	tos.lock.Lock()
	defer tos.lock.Unlock()
	tos.inPendingBlocks[block.Hash] = struct{}{}
	tos.pendingBlocks = append(tos.pendingBlocks, block)
	if block.Position.Height == 0 {
		tos.bootstrapChain[block.Position.ChainID] = struct{}{}
	}
	if uint32(len(tos.bootstrapChain)) == tos.numChains {
		// Bootstrap mode.
		delivered = tos.pendingBlocks
		tos.syncDeliverySetIdx = 0
		for i := uint32(0); i < tos.numChains; i++ {
			tos.syncHeight[i] = uint64(0)
		}
	} else {
		maxDeliverySetIdx := -1
		// TODO(jimmy-dexon): below for loop can be optimized.
	PendingBlockLoop:
		for i, block := range tos.pendingBlocks {
			idx, exist := tos.mapToDeliverySet[block.Hash]
			if !exist {
				continue
			}
			deliverySet := tos.deliverySet[idx]
			// Check if all the blocks in deliverySet are in the pendingBlocks.
			for _, dBlock := range deliverySet {
				if _, exist := tos.inPendingBlocks[dBlock.Hash]; !exist {
					continue PendingBlockLoop
				}
			}
			if idx > maxDeliverySetIdx {
				maxDeliverySetIdx = idx
			}
			// Check if all of the chains have delivered.
			for _, dBlock := range deliverySet {
				if h, exist := tos.syncHeight[dBlock.Position.ChainID]; exist {
					if dBlock.Position.Height < h {
						continue
					}
				}
				tos.syncHeight[dBlock.Position.ChainID] = dBlock.Position.Height
			}
			if uint32(len(tos.syncHeight)) != tos.numChains {
				continue
			}
			// Core is fully synced, it can start delivering blocks from idx.
			tos.syncDeliverySetIdx = maxDeliverySetIdx
			delivered = make([]*types.Block, 0, i)
			break
		}
		if tos.syncDeliverySetIdx == -1 {
			return
		}
		// Generating delivering blocks.
		for i := maxDeliverySetIdx; i < len(tos.deliverySet); i++ {
			deliverySet := tos.deliverySet[i]
			sort.Sort(types.ByHash(deliverySet))
			for _, block := range deliverySet {
				if block.Position.Height > tos.syncHeight[block.Position.ChainID] {
					tos.syncHeight[block.Position.ChainID] = block.Position.Height
				}
				delivered = append(delivered, block)
			}
		}
		// Flush remaining blocks.
		for _, block := range tos.pendingBlocks {
			if _, exist := tos.mapToDeliverySet[block.Hash]; exist {
				continue
			}
			if block.Position.Height > tos.syncHeight[block.Position.ChainID] {
				tos.syncHeight[block.Position.ChainID] = block.Position.Height
			}
			delivered = append(delivered, block)
		}
	}
	// Clean internal data model to save memory.
	tos.pendingBlocks = nil
	tos.inPendingBlocks = nil
	tos.bootstrapChain = nil
	tos.pendingDeliveryBlocks = nil
	tos.deliverySet = nil
	tos.mapToDeliverySet = nil
	return
}

// The finalized block should be passed by the order of consensus height.
func (tos *totalOrderingSyncer) processFinalizedBlock(block *types.Block) {
	tos.lock.Lock()
	defer tos.lock.Unlock()
	if len(tos.pendingDeliveryBlocks) > 0 {
		if block.Hash.Less(
			tos.pendingDeliveryBlocks[len(tos.pendingDeliveryBlocks)-1].Hash) {
			// pendingDeliveryBlocks forms a deliverySet.
			idx := len(tos.deliverySet)
			tos.deliverySet[idx] = tos.pendingDeliveryBlocks
			for _, block := range tos.pendingDeliveryBlocks {
				tos.mapToDeliverySet[block.Hash] = idx
			}
			tos.pendingDeliveryBlocks = []*types.Block{}
		}
	}
	tos.pendingDeliveryBlocks = append(tos.pendingDeliveryBlocks, block)
}
