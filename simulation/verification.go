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

package simulation

import (
	"container/heap"
	"log"
	"math"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// TotalOrderResult is the object maintaining peer's result of
// Total Ordering Algorithm.
type TotalOrderResult struct {
	hashList         common.Hashes
	curID            int
	pendingBlockList PendingBlockList
}

// PeerTotalOrder stores the TotalOrderResult of each validator.
type PeerTotalOrder = map[types.ValidatorID]*TotalOrderResult

// NewTotalOrderResult returns pointer to a a new TotalOrderResult instance.
func NewTotalOrderResult() *TotalOrderResult {
	totalOrder := &TotalOrderResult{}
	heap.Init(&totalOrder.pendingBlockList)
	return totalOrder
}

// PushBlocks push a BlockList into the TotalOrderResult and return true if
// there is new blocks ready for verifiy
func (totalOrder *TotalOrderResult) PushBlocks(blocks BlockList) (ready bool) {
	if blocks.ID != totalOrder.curID {
		heap.Push(&totalOrder.pendingBlockList, &blocks)
		return false
	}

	// Append all of the consecutive blockList in the pendingBlockList.
	for {
		totalOrder.hashList = append(totalOrder.hashList, blocks.BlockHash...)
		totalOrder.curID++
		if len(totalOrder.pendingBlockList) == 0 ||
			totalOrder.pendingBlockList[0].ID != totalOrder.curID {
			break
		}
		blocks = *heap.Pop(&totalOrder.pendingBlockList).(*BlockList)
	}
	return true
}

// VerifyTotalOrder verifies if the result of Total Ordering Algorithm
// returned by all validators are the same. However, the length of result
// of each validators may not be the same, so only the common part is verified.
func VerifyTotalOrder(id types.ValidatorID,
	totalOrder PeerTotalOrder) (
	unverifiedMap PeerTotalOrder, correct bool) {

	hasError := false

	// Get the common length from all validators.
	length := math.MaxInt32
	for _, peerTotalOrder := range totalOrder {
		if len(peerTotalOrder.hashList) < length {
			length = len(peerTotalOrder.hashList)
		}
	}

	// Verify if the order of the blocks are the same by comparing
	// the hash value.
	for i := 0; i < length; i++ {
		hash := totalOrder[id].hashList[i]
		for vid, peerTotalOrder := range totalOrder {
			if peerTotalOrder.hashList[i] != hash {
				log.Printf("[%d] Unexpected hash %v from %v\n", i,
					peerTotalOrder.hashList[i], vid)
				hasError = true
			}
		}
		if hasError {
			log.Printf("[%d] Hash is %v from %v\n", i, hash, id)
		} else {
			log.Printf("Block %v confirmed\n", hash)
		}
	}

	// Remove verified block from list.
	if length > 0 {
		for vid := range totalOrder {
			totalOrder[vid].hashList =
				totalOrder[vid].hashList[length:]
		}
	}
	return totalOrder, !hasError
}
