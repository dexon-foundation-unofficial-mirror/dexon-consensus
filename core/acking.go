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

package core

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// acking is for acking module.
type acking struct {
	// lattice stores blocks by its validator ID and height.
	lattice map[types.ValidatorID]*ackingValidatorStatus

	// blocks stores the hash to block map.
	blocks map[common.Hash]*types.Block

	// receivedBlocks stores blocks which is received but its acks are not all
	// in lattice.
	receivedBlocks map[common.Hash]*types.Block

	// ackedBlocks stores blocks in status types.BlockStatusAcked, which are
	// strongly acked but not yet being output to total ordering module.
	ackedBlocks map[common.Hash]*types.Block
}

type ackingValidatorStatus struct {
	// blocks stores blocks proposed by specified validator in map which key is
	// the height of the block.
	blocks map[uint64]*types.Block

	// nextAck stores the height of next height that should be acked, i.e. last
	// acked height + 1. Initialized to 0, when genesis blocks are still not
	// being acked. For example, a.lattice[vid1].NextAck[vid2] - 1 is the last
	// acked height by vid1 acking vid2.
	nextAck map[types.ValidatorID]uint64

	// nextOutput is the next output height of block, default to 0.
	nextOutput uint64

	// restricted is the flag of a validator is in restricted mode or not.
	restricted bool
}

// Errors for sanity check error.
var (
	ErrInvalidProposerID  = fmt.Errorf("invalid proposer id")
	ErrForkBlock          = fmt.Errorf("fork block")
	ErrNotAckParent       = fmt.Errorf("not ack parent")
	ErrDoubleAck          = fmt.Errorf("double ack")
	ErrInvalidBlockHeight = fmt.Errorf("invalid block height")
)

// newAcking creates a new acking struct.
func newAcking() *acking {
	return &acking{
		lattice:        make(map[types.ValidatorID]*ackingValidatorStatus),
		blocks:         make(map[common.Hash]*types.Block),
		receivedBlocks: make(map[common.Hash]*types.Block),
		ackedBlocks:    make(map[common.Hash]*types.Block),
	}
}

func (a *acking) sanityCheck(b *types.Block) error {
	// Check if its proposer is in validator set.
	if _, exist := a.lattice[b.ProposerID]; !exist {
		return ErrInvalidProposerID
	}

	// Check if it forks.
	if bInLattice, exist := a.lattice[b.ProposerID].blocks[b.Height]; exist {
		if b.Hash != bInLattice.Hash {
			return ErrForkBlock
		}
	}

	// Check non-genesis blocks if it acks its parent.
	if b.Height > 0 {
		if _, exist := b.Acks[b.ParentHash]; !exist {
			return ErrNotAckParent
		}
		bParent := a.blocks[b.ParentHash]
		if bParent.Height != b.Height-1 {
			return ErrInvalidBlockHeight
		}
	}

	// Check if it acks older blocks.
	for hash := range b.Acks {
		if bAck, exist := a.blocks[hash]; exist {
			if bAck.Height < a.lattice[b.ProposerID].nextAck[bAck.ProposerID] {
				return ErrDoubleAck
			}
		}
	}

	// TODO(haoping): application layer check of block's content

	return nil
}

// areAllAcksReceived checks if all ack blocks of a block are all in lattice.
func (a *acking) areAllAcksInLattice(b *types.Block) bool {
	for h := range b.Acks {
		bAck, exist := a.blocks[h]
		if !exist {
			return false
		}
		if bAckInLattice, exist := a.lattice[bAck.ProposerID].blocks[bAck.Height]; !exist {
			if bAckInLattice.Hash != bAck.Hash {
				panic("areAllAcksInLattice: acking.lattice has corrupted")
			}
			return false
		}
	}
	return true
}

// processBlock processes block, it does sanity check, inserts block into
// lattice, handles strong acking and deletes blocks which will not be used.
func (a *acking) processBlock(block *types.Block) {
	// If a block does not pass sanity check, discard this block.
	if err := a.sanityCheck(block); err != nil {
		return
	}
	a.blocks[block.Hash] = block
	block.AckedValidators = make(map[types.ValidatorID]struct{})
	a.receivedBlocks[block.Hash] = block

	// Check blocks in receivedBlocks if its acks are all in lattice. If a block's
	// acking blocks are all in lattice, execute sanity check and add the block
	// into lattice.
	blocksToAcked := map[common.Hash]*types.Block{}
	for {
		blocksToLattice := map[common.Hash]*types.Block{}
		for _, b := range a.receivedBlocks {
			if a.areAllAcksInLattice(b) {
				blocksToLattice[b.Hash] = b
			}
		}
		if len(blocksToLattice) == 0 {
			break
		}
		for _, b := range blocksToLattice {
			// Sanity check must been executed again here for the case that several
			// valid blocks with different content being added into blocksToLattice
			// in the same time. For example
			// B   C  Block B and C both ack A and are valid. B, C received first
			//  \ /   (added in receivedBlocks), and A comes, if sanity check is
			//   A    not being executed here, B and C will both be added in lattice
			if err := a.sanityCheck(b); err != nil {
				delete(a.blocks, b.Hash)
				delete(a.receivedBlocks, b.Hash)
				continue
			}
			a.lattice[b.ProposerID].blocks[b.Height] = b
			delete(a.receivedBlocks, b.Hash)
			for h := range b.Acks {
				bAck := a.blocks[h]
				// Update nextAck only when bAck.Height + 1 is greater. A block might
				// ack blocks proposed by same validator with different height.
				if a.lattice[b.ProposerID].nextAck[bAck.ProposerID] < bAck.Height+1 {
					a.lattice[b.ProposerID].nextAck[bAck.ProposerID] = bAck.Height + 1
				}
				// Update AckedValidators for each ack blocks and its parents.
				for {
					if _, exist := bAck.AckedValidators[b.ProposerID]; exist {
						break
					}
					if bAck.Status > types.BlockStatusInit {
						break
					}
					bAck.AckedValidators[b.ProposerID] = struct{}{}
					// A block is strongly acked if it is acked by more than
					// 2 * (maximum number of byzatine validators) unique validators.
					if len(bAck.AckedValidators) > 2*((len(a.lattice)-1)/3) {
						blocksToAcked[bAck.Hash] = bAck
					}
					if bAck.Height == 0 {
						break
					}
					bAck = a.blocks[bAck.ParentHash]
				}
			}
		}
	}

	for _, b := range blocksToAcked {
		a.ackedBlocks[b.Hash] = b
		b.Status = types.BlockStatusAcked
	}

	// TODO(haoping): delete blocks in received array when it is received a long
	// time ago

	// Delete old blocks in "lattice" and "blocks" for release memory space.
	// First, find the height that blocks below it can be deleted. This height
	// is defined by finding minimum of validator's nextOutput and last acking
	// heights from other validators, i.e. a.lattice[v_other].nextAck[this_vid].
	// This works because blocks of height below this minimum are not going to be
	// acked anymore, the ackings of these blocks are illegal.
	for vid := range a.lattice {
		// Find the minimum height that heights lesser can be deleted.
		min := a.lattice[vid].nextOutput
		for vid2 := range a.lattice {
			if a.lattice[vid2].nextAck[vid] < min {
				min = a.lattice[vid2].nextAck[vid]
			}
		}
		// "min" is the height of "next" last acked, min - 1 is the last height.
		// Delete blocks from min - 2 which will never be acked.
		if min < 3 {
			continue
		}
		min -= 2
		for {
			b, exist := a.lattice[vid].blocks[min]
			if !exist {
				break
			}
			if b.Status >= types.BlockStatusOrdering {
				delete(a.lattice[vid].blocks, b.Height)
				delete(a.blocks, b.Hash)
			}
			if min == 0 {
				break
			}
			min--
		}
	}
}

// extractBlocks returns all blocks that can be inserted into total ordering's
// DAG. This function changes the status of blocks from types.BlockStatusAcked
// to blockStatusOrdering.
func (a *acking) extractBlocks() []*types.Block {
	ret := []*types.Block{}
	for {
		updated := false
		for vid := range a.lattice {
			b, exist := a.lattice[vid].blocks[a.lattice[vid].nextOutput]
			if !exist || b.Status < types.BlockStatusAcked {
				continue
			}
			allAcksInOrderingStatus := true
			// Check if all acks are in ordering or above status. If a block of an ack
			// does not exist means that it deleted but its status is definitely Acked
			// or ordering.
			for ackHash := range b.Acks {
				bAck, exist := a.blocks[ackHash]
				if !exist {
					continue
				}
				if bAck.Status < types.BlockStatusOrdering {
					allAcksInOrderingStatus = false
					break
				}
			}
			if !allAcksInOrderingStatus {
				continue
			}
			updated = true
			b.Status = types.BlockStatusOrdering
			delete(a.ackedBlocks, b.Hash)
			ret = append(ret, b)
			a.lattice[vid].nextOutput++
		}
		if !updated {
			break
		}
	}
	return ret
}

// addValidator adds validator in the validator set.
func (a *acking) addValidator(h types.ValidatorID) {
	a.lattice[h] = &ackingValidatorStatus{
		blocks:     make(map[uint64]*types.Block),
		nextAck:    make(map[types.ValidatorID]uint64),
		nextOutput: 0,
		restricted: false,
	}
}

// deleteValidator deletes validator in validator set.
func (a *acking) deleteValidator(h types.ValidatorID) {
	for h := range a.lattice {
		delete(a.lattice[h].nextAck, h)
	}
	delete(a.lattice, h)
}
