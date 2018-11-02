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

package simulation

import (
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
)

// BlockList is the list of blocks from the result of Total Ordering Algorithm.
type BlockList struct {
	ID             int             `json:"id"`
	BlockHash      common.Hashes   `json:"blockhash"`
	ConfirmLatency []time.Duration `json:"confirmlatency"`
	// The index is required by heap.Interface.
	index int
}

// PendingBlockList is a PrioirtyQueue maintaining the BlockList received
// before the previous one (based on ID).
type PendingBlockList []*BlockList

// Len, Less and Swap are implementing heap.Interface
func (p PendingBlockList) Len() int           { return len(p) }
func (p PendingBlockList) Less(i, j int) bool { return p[i].ID < p[j].ID }
func (p PendingBlockList) Swap(i, j int) {
	p[i], p[j] = p[j], p[i]
	p[i].index = i
	p[j].index = j
}

// Push item in the Heap.
func (p *PendingBlockList) Push(x interface{}) {
	n := len(*p)
	item := x.(*BlockList)
	item.index = n
	*p = append(*p, item)
}

// Pop the element from the Heap.
func (p *PendingBlockList) Pop() interface{} {
	old := *p
	n := len(old)
	item := old[n-1]
	item.index = -1 // For safety.
	*p = old[0 : n-1]
	return item
}
