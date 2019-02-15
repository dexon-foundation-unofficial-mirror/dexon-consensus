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

package test

import (
	"errors"
	"sort"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// Errors returns from block-revealer.
var (
	ErrNotValidCompactionChain = errors.New("not valid compaction chain")
)

// isAllAckingBlockRevealed is a helper to check if all acking blocks of
// one block are revealed.
func isAllAckingBlockRevealed(
	b *types.Block, revealed map[common.Hash]struct{}) bool {

	for _, ack := range b.Acks {
		if _, exists := revealed[ack]; !exists {
			return false
		}
	}
	return true
}

// loadAllBlocks is a helper to load all blocks from db.BlockIterator.
func loadAllBlocks(iter db.BlockIterator) (
	blocks map[common.Hash]*types.Block, err error) {

	blocks = make(map[common.Hash]*types.Block)
	for {
		block, err := iter.NextBlock()
		if err != nil {
			if err == db.ErrIterationFinished {
				// It's safe to ignore iteraion-finished error.
				err = nil
			}
			break
		}
		blocks[block.Hash] = &block
	}
	return
}

// CompactionChainBlockRevealer implements BlockRevealer interface, which would
// load all blocks from db, reveal them in the order of compaction chain,
// from the genesis block to the latest one.
type CompactionChainBlockRevealer struct {
	blocks          types.BlocksByFinalizationHeight
	nextRevealIndex int
}

// NewCompactionChainBlockRevealer constructs a block revealer in the order of
// compaction chain.
func NewCompactionChainBlockRevealer(iter db.BlockIterator,
	startHeight uint64) (r *CompactionChainBlockRevealer, err error) {
	blocksByHash, err := loadAllBlocks(iter)
	if err != nil {
		return
	}
	if startHeight == 0 {
		startHeight = 1
	}
	blocks := types.BlocksByFinalizationHeight{}
	for _, b := range blocksByHash {
		if b.Finalization.Height < startHeight {
			continue
		}
		blocks = append(blocks, b)
	}
	sort.Sort(types.BlocksByFinalizationHeight(blocks))
	// Make sure the finalization height of blocks are incremental with step 1.
	for idx, b := range blocks {
		if idx == 0 {
			continue
		}
		if b.Finalization.Height != blocks[idx-1].Finalization.Height+1 {
			err = ErrNotValidCompactionChain
			return
		}
	}
	r = &CompactionChainBlockRevealer{
		blocks: blocks,
	}
	r.Reset()
	return
}

// NextBlock implements Revealer.Next method, which would reveal blocks in the
// order of compaction chain.
func (r *CompactionChainBlockRevealer) NextBlock() (types.Block, error) {
	if r.nextRevealIndex == len(r.blocks) {
		return types.Block{}, db.ErrIterationFinished
	}
	b := r.blocks[r.nextRevealIndex]
	r.nextRevealIndex++
	return *b, nil
}

// Reset implement Revealer.Reset method, which would reset revealing.
func (r *CompactionChainBlockRevealer) Reset() {
	r.nextRevealIndex = 0
}
