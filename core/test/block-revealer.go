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

// BlockRevealerByPosition implements BlockRevealer interface, which would
// load all blocks from db, reveal them in the order of compaction chain,
// from the genesis block to the latest one.
type BlockRevealerByPosition struct {
	blocks          types.BlocksByPosition
	nextRevealIndex int
}

// NewBlockRevealerByPosition constructs a block revealer in the order of
// compaction chain.
func NewBlockRevealerByPosition(iter db.BlockIterator, startHeight uint64) (
	r *BlockRevealerByPosition, err error) {
	blocksByHash, err := loadAllBlocks(iter)
	if err != nil {
		return
	}
	blocks := types.BlocksByPosition{}
	for _, b := range blocksByHash {
		if b.Position.Height < startHeight {
			continue
		}
		blocks = append(blocks, b)
	}
	sort.Sort(types.BlocksByPosition(blocks))
	// Make sure the height of blocks are incremental with step 1.
	for idx, b := range blocks {
		if idx == 0 {
			continue
		}
		if b.Position.Height != blocks[idx-1].Position.Height+1 {
			err = ErrNotValidCompactionChain
			return
		}
	}
	r = &BlockRevealerByPosition{blocks: blocks}
	r.Reset()
	return
}

// NextBlock implements Revealer.Next method, which would reveal blocks in the
// order of compaction chain.
func (r *BlockRevealerByPosition) NextBlock() (types.Block, error) {
	if r.nextRevealIndex == len(r.blocks) {
		return types.Block{}, db.ErrIterationFinished
	}
	b := r.blocks[r.nextRevealIndex]
	r.nextRevealIndex++
	return *b, nil
}

// Reset implement Revealer.Reset method, which would reset revealing.
func (r *BlockRevealerByPosition) Reset() {
	r.nextRevealIndex = 0
}
