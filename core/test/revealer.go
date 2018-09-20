// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package test

import (
	"math/rand"
	"sort"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
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

// loadAllBlocks is a helper to load all blocks from blockdb.BlockIterator.
func loadAllBlocks(iter blockdb.BlockIterator) (
	blocks map[common.Hash]*types.Block, err error) {

	blocks = make(map[common.Hash]*types.Block)
	for {
		block, err := iter.Next()
		if err != nil {
			if err == blockdb.ErrIterationFinished {
				// It's safe to ignore iteraion-finished error.
				err = nil
			}
			break
		}
		blocks[block.Hash] = &block
	}
	return
}

// RandomDAGRevealer implements Revealer interface, which would load
// all blocks from blockdb, and randomly pick one block to reveal if
// it still forms a valid DAG in revealed blocks.
type RandomDAGRevealer struct {
	// blocksByNode group all blocks by nodes and sorting
	// them by height.
	blocksByNode map[types.NodeID][]*types.Block
	// tipIndexes store the height of next block from one node
	// to check if is candidate.
	tipIndexes map[types.NodeID]int
	// candidate are blocks that forms valid DAG with
	// current revealed blocks.
	candidates []*types.Block
	// revealed stores block hashes of current revealed blocks.
	revealed map[common.Hash]struct{}
	randGen  *rand.Rand
}

// NewRandomDAGRevealer constructs RandomDAGRevealer.
func NewRandomDAGRevealer(
	iter blockdb.BlockIterator) (r *RandomDAGRevealer, err error) {

	blocks, err := loadAllBlocks(iter)
	if err != nil {
		return
	}

	// Rearrange blocks by nodes and height.
	blocksByNode := make(map[types.NodeID][]*types.Block)
	for _, block := range blocks {
		blocksByNode[block.ProposerID] =
			append(blocksByNode[block.ProposerID], block)
	}
	// Make sure blocks are sorted by block heights, from lower to higher.
	for nID := range blocksByNode {
		sort.Sort(types.ByHeight(blocksByNode[nID]))
	}
	r = &RandomDAGRevealer{
		blocksByNode: blocksByNode,
		randGen:      rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	// Make sure this revealer is ready to use.
	r.Reset()
	return
}

// pickCandidates is a helper function to pick candidates from current tips.
func (r *RandomDAGRevealer) pickCandidates() {
	for nID, tip := range r.tipIndexes {
		blocks, exists := r.blocksByNode[nID]
		if !exists {
			continue
		}
		if tip >= len(blocks) {
			continue
		}
		block := blocks[tip]
		if isAllAckingBlockRevealed(block, r.revealed) {
			r.tipIndexes[nID]++
			r.candidates = append(r.candidates, block)
		}
	}
}

// Next implement Revealer.Next method, which would reveal blocks
// forming valid DAGs.
func (r *RandomDAGRevealer) Next() (types.Block, error) {
	if len(r.candidates) == 0 {
		r.pickCandidates()
		if len(r.candidates) == 0 {
			return types.Block{}, blockdb.ErrIterationFinished
		}
	}

	// Pick next block to be revealed.
	picked := r.randGen.Intn(len(r.candidates))
	block := r.candidates[picked]
	r.candidates =
		append(r.candidates[:picked], r.candidates[picked+1:]...)
	r.revealed[block.Hash] = struct{}{}
	r.pickCandidates()
	return *block, nil
}

// Reset implement Revealer.Reset method, which would reset the revealing.
func (r *RandomDAGRevealer) Reset() {
	r.tipIndexes = make(map[types.NodeID]int)
	for nID := range r.blocksByNode {
		r.tipIndexes[nID] = 0
	}
	r.revealed = make(map[common.Hash]struct{})
	r.candidates = []*types.Block{}
}

// RandomRevealer implements Revealer interface, which would load
// all blocks from blockdb, and randomly pick one block to reveal.
type RandomRevealer struct {
	blocks  map[common.Hash]*types.Block
	remains common.Hashes
	randGen *rand.Rand
}

// NewRandomRevealer constructs RandomRevealer.
func NewRandomRevealer(
	iter blockdb.BlockIterator) (r *RandomRevealer, err error) {

	blocks, err := loadAllBlocks(iter)
	if err != nil {
		return
	}
	r = &RandomRevealer{
		blocks:  blocks,
		randGen: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	r.Reset()
	return
}

// Next implements Revealer.Next method, which would reveal blocks randomly.
func (r *RandomRevealer) Next() (types.Block, error) {
	if len(r.remains) == 0 {
		return types.Block{}, blockdb.ErrIterationFinished
	}

	picked := r.randGen.Intn(len(r.remains))
	block := r.blocks[r.remains[picked]]
	r.remains =
		append(r.remains[:picked], r.remains[picked+1:]...)
	return *block, nil
}

// Reset implement Revealer.Reset method, which would reset revealing.
func (r *RandomRevealer) Reset() {
	hashes := common.Hashes{}
	for hash := range r.blocks {
		hashes = append(hashes, hash)
	}
	r.remains = hashes
}
