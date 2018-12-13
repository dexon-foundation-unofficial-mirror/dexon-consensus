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
	"math/rand"
	"sort"
	"time"

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

// RandomDAGBlockRevealer implements BlockRevealer interface, which would load
// all blocks from db, and randomly pick one block to reveal if it still forms
// a valid DAG in revealed blocks.
type RandomDAGBlockRevealer struct {
	// blocksByChain group all blocks by chains and sorting
	// them by height.
	blocksByChain map[uint32][]*types.Block
	// tipIndexes store the height of next block from one chain
	// to check if is candidate.
	tipIndexes map[uint32]int
	// candidate are blocks that forms valid DAG with
	// current revealed blocks.
	candidates      []*types.Block
	candidateChains map[uint32]struct{}
	// revealed stores block hashes of current revealed blocks.
	revealed map[common.Hash]struct{}
	randGen  *rand.Rand
}

// NewRandomDAGBlockRevealer constructs RandomDAGBlockRevealer.
func NewRandomDAGBlockRevealer(
	iter db.BlockIterator) (r *RandomDAGBlockRevealer, err error) {

	blocks, err := loadAllBlocks(iter)
	if err != nil {
		return
	}

	// Rearrange blocks by nodes and height.
	blocksByChain := make(map[uint32][]*types.Block)
	for _, block := range blocks {
		blocksByChain[block.Position.ChainID] =
			append(blocksByChain[block.Position.ChainID], block)
	}
	// Make sure blocks are sorted by block heights, from lower to higher.
	for chainID := range blocksByChain {
		sort.Sort(types.ByPosition(blocksByChain[chainID]))
	}
	r = &RandomDAGBlockRevealer{
		blocksByChain:   blocksByChain,
		randGen:         rand.New(rand.NewSource(time.Now().UnixNano())),
		candidateChains: make(map[uint32]struct{}),
	}
	// Make sure this revealer is ready to use.
	r.Reset()
	return
}

// pickCandidates is a helper function to pick candidates from current tips.
func (r *RandomDAGBlockRevealer) pickCandidates() {
	for chainID, tip := range r.tipIndexes {
		if _, isPicked := r.candidateChains[chainID]; isPicked {
			continue
		}
		blocks, exists := r.blocksByChain[chainID]
		if !exists {
			continue
		}
		if tip >= len(blocks) {
			continue
		}
		block := blocks[tip]
		if isAllAckingBlockRevealed(block, r.revealed) {
			r.tipIndexes[chainID]++
			r.candidates = append(r.candidates, block)
			r.candidateChains[chainID] = struct{}{}
		}
	}
}

// NextBlock implement Revealer.Next method, which would reveal blocks
// forming valid DAGs.
func (r *RandomDAGBlockRevealer) NextBlock() (types.Block, error) {
	if len(r.candidates) == 0 {
		r.pickCandidates()
		if len(r.candidates) == 0 {
			return types.Block{}, db.ErrIterationFinished
		}
	}

	// Pick next block to be revealed.
	picked := r.randGen.Intn(len(r.candidates))
	block := r.candidates[picked]
	r.candidates =
		append(r.candidates[:picked], r.candidates[picked+1:]...)
	delete(r.candidateChains, block.Position.ChainID)
	r.revealed[block.Hash] = struct{}{}
	r.pickCandidates()
	return *block, nil
}

// Reset implement Revealer.Reset method, which would reset the revealing.
func (r *RandomDAGBlockRevealer) Reset() {
	r.tipIndexes = make(map[uint32]int)
	for chainID := range r.blocksByChain {
		r.tipIndexes[chainID] = 0
	}
	r.revealed = make(map[common.Hash]struct{})
	r.candidates = []*types.Block{}
}

// RandomBlockRevealer implements BlockRevealer interface, which would load
// all blocks from db, and randomly pick one block to reveal.
type RandomBlockRevealer struct {
	blocks  map[common.Hash]*types.Block
	remains common.Hashes
	randGen *rand.Rand
}

// NewRandomBlockRevealer constructs RandomBlockRevealer.
func NewRandomBlockRevealer(
	iter db.BlockIterator) (r *RandomBlockRevealer, err error) {

	blocks, err := loadAllBlocks(iter)
	if err != nil {
		return
	}
	r = &RandomBlockRevealer{
		blocks:  blocks,
		randGen: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	r.Reset()
	return
}

// NextBlock implements Revealer.NextBlock method, which would reveal blocks
// randomly.
func (r *RandomBlockRevealer) NextBlock() (types.Block, error) {
	if len(r.remains) == 0 {
		return types.Block{}, db.ErrIterationFinished
	}

	picked := r.randGen.Intn(len(r.remains))
	block := r.blocks[r.remains[picked]]
	r.remains =
		append(r.remains[:picked], r.remains[picked+1:]...)
	return *block, nil
}

// Reset implement Revealer.Reset method, which would reset revealing.
func (r *RandomBlockRevealer) Reset() {
	hashes := common.Hashes{}
	for hash := range r.blocks {
		hashes = append(hashes, hash)
	}
	r.remains = hashes
}

// RandomTipBlockRevealer implements BlockRevealer interface, which would load
// all blocks from db, and randomly pick one chain's tip to reveal.
type RandomTipBlockRevealer struct {
	chainsBlock    []map[uint64]*types.Block
	chainTip       []uint64
	chainRevealSeq []uint32
	revealed       int
	randGen        *rand.Rand
}

// NewRandomTipBlockRevealer constructs RandomTipBlockRevealer.
func NewRandomTipBlockRevealer(
	iter db.BlockIterator) (r *RandomTipBlockRevealer, err error) {

	blocks, err := loadAllBlocks(iter)
	if err != nil {
		return
	}
	r = &RandomTipBlockRevealer{
		randGen: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
	for _, b := range blocks {
		for b.Position.ChainID >= uint32(len(r.chainsBlock)) {
			r.chainsBlock = append(r.chainsBlock, make(map[uint64]*types.Block))
			r.chainTip = append(r.chainTip, 0)
		}
		r.chainsBlock[b.Position.ChainID][b.Position.Height] = b
		r.chainRevealSeq = append(r.chainRevealSeq, b.Position.ChainID)
	}
	r.Reset()
	return
}

// NextBlock implements Revealer.Next method, which would reveal blocks randomly.
func (r *RandomTipBlockRevealer) NextBlock() (types.Block, error) {
	if len(r.chainRevealSeq) == r.revealed {
		return types.Block{}, db.ErrIterationFinished
	}

	picked := r.chainRevealSeq[r.revealed]
	r.revealed++
	block := r.chainsBlock[picked][r.chainTip[picked]]
	r.chainTip[picked]++
	return *block, nil
}

// Reset implement Revealer.Reset method, which would reset revealing.
func (r *RandomTipBlockRevealer) Reset() {
	r.revealed = 0
	r.randGen.Shuffle(len(r.chainRevealSeq), func(i, j int) {
		r.chainRevealSeq[i], r.chainRevealSeq[j] =
			r.chainRevealSeq[j], r.chainRevealSeq[i]
	})
	for i := range r.chainTip {
		r.chainTip[i] = 0
	}
}

// CompactionChainBlockRevealer implements BlockRevealer interface, which would
// load all blocks from db, reveal them in the order of compaction chain,
// from the genesis block to the latest one.
type CompactionChainBlockRevealer struct {
	blocks          types.ByFinalizationHeight
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
	blocks := types.ByFinalizationHeight{}
	for _, b := range blocksByHash {
		if b.Finalization.Height < startHeight {
			continue
		}
		blocks = append(blocks, b)
	}
	sort.Sort(types.ByFinalizationHeight(blocks))
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
