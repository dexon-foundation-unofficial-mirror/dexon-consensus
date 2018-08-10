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
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// reliableBroadcast is a module for reliable broadcast.
type reliableBroadcast struct {
	// lattice stores blocks by its validator ID and height.
	lattice map[types.ValidatorID]*ackingValidatorStatus

	// blocks stores the hash to block map.
	blocks map[common.Hash]*types.Block

	// receivedBlocks stores blocks which is received but its acks are not all
	// in lattice.
	receivedBlocks map[common.Hash]*types.Block
}

type ackingValidatorStatus struct {
	// blocks stores blocks proposed by specified validator in map which key is
	// the height of the block.
	blocks map[uint64]*types.Block

	// nextAck stores the height of next height that should be acked, i.e. last
	// acked height + 1. Initialized to 0, when genesis blocks are still not
	// being acked. For example, rb.lattice[vid1].NextAck[vid2] - 1 is the last
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
	ErrAlreadyInLattice   = fmt.Errorf("block already in lattice")
)

// newReliableBroadcast creates a new reliableBroadcast struct.
func newReliableBroadcast() *reliableBroadcast {
	return &reliableBroadcast{
		lattice:        make(map[types.ValidatorID]*ackingValidatorStatus),
		blocks:         make(map[common.Hash]*types.Block),
		receivedBlocks: make(map[common.Hash]*types.Block),
	}
}

func (rb *reliableBroadcast) sanityCheck(b *types.Block) error {
	// Check if its proposer is in validator set.
	if _, exist := rb.lattice[b.ProposerID]; !exist {
		return ErrInvalidProposerID
	}

	// Check if it forks.
	if bInLattice, exist := rb.lattice[b.ProposerID].blocks[b.Height]; exist {
		if b.Hash != bInLattice.Hash {
			return ErrForkBlock
		}
		return ErrAlreadyInLattice
	}

	// Check non-genesis blocks if it acks its parent.
	if b.Height > 0 {
		if _, exist := b.Acks[b.ParentHash]; !exist {
			return ErrNotAckParent
		}
		bParent, exists := rb.blocks[b.ParentHash]
		if exists && bParent.Height != b.Height-1 {
			return ErrInvalidBlockHeight
		}
	}

	// Check if it acks older blocks.
	for hash := range b.Acks {
		if bAck, exist := rb.blocks[hash]; exist {
			if bAck.Height < rb.lattice[b.ProposerID].nextAck[bAck.ProposerID] {
				return ErrDoubleAck
			}
		}
	}

	// TODO(haoping): application layer check of block's content

	return nil
}

// areAllAcksReceived checks if all ack blocks of a block are all in lattice.
func (rb *reliableBroadcast) areAllAcksInLattice(b *types.Block) bool {
	for h := range b.Acks {
		bAck, exist := rb.blocks[h]
		if !exist {
			return false
		}
		bAckInLattice, exist := rb.lattice[bAck.ProposerID].blocks[bAck.Height]
		if !exist {
			return false
		}
		if bAckInLattice.Hash != bAck.Hash {
			panic("areAllAcksInLattice: reliableBroadcast.lattice has corrupted")
		}
	}
	return true
}

// processBlock processes block, it does sanity check, inserts block into
// lattice, handles strong acking and deletes blocks which will not be used.
func (rb *reliableBroadcast) processBlock(block *types.Block) (err error) {
	// If a block does not pass sanity check, discard this block.
	if err = rb.sanityCheck(block); err != nil {
		return
	}
	rb.blocks[block.Hash] = block
	block.AckedValidators = make(map[types.ValidatorID]struct{})
	rb.receivedBlocks[block.Hash] = block

	// Check blocks in receivedBlocks if its acks are all in lattice. If a block's
	// acking blocks are all in lattice, execute sanity check and add the block
	// into lattice.
	blocksToAcked := map[common.Hash]*types.Block{}
	for {
		blocksToLattice := map[common.Hash]*types.Block{}
		for _, b := range rb.receivedBlocks {
			if rb.areAllAcksInLattice(b) {
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
			if err = rb.sanityCheck(b); err != nil {
				delete(rb.blocks, b.Hash)
				delete(rb.receivedBlocks, b.Hash)
				continue
				// TODO(mission): how to return for multiple errors?
			}
			rb.lattice[b.ProposerID].blocks[b.Height] = b
			delete(rb.receivedBlocks, b.Hash)
			for h := range b.Acks {
				bAck := rb.blocks[h]
				// Update nextAck only when bAck.Height + 1 is greater. A block might
				// ack blocks proposed by same validator with different height.
				if rb.lattice[b.ProposerID].nextAck[bAck.ProposerID] < bAck.Height+1 {
					rb.lattice[b.ProposerID].nextAck[bAck.ProposerID] = bAck.Height + 1
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
					if len(bAck.AckedValidators) > 2*((len(rb.lattice)-1)/3) {
						blocksToAcked[bAck.Hash] = bAck
					}
					if bAck.Height == 0 {
						break
					}
					bAck = rb.blocks[bAck.ParentHash]
				}
			}
		}
	}

	for _, b := range blocksToAcked {
		b.Status = types.BlockStatusAcked
	}

	// TODO(haoping): delete blocks in received array when it is received a long
	// time ago

	// Delete old blocks in "lattice" and "blocks" for release memory space.
	// First, find the height that blocks below it can be deleted. This height
	// is defined by finding minimum of validator's nextOutput and last acking
	// heights from other validators, i.e. rb.lattice[v_other].nextAck[this_vid].
	// This works because blocks of height below this minimum are not going to be
	// acked anymore, the ackings of these blocks are illegal.
	for vid := range rb.lattice {
		// Find the minimum height that heights lesser can be deleted.
		min := rb.lattice[vid].nextOutput
		for vid2 := range rb.lattice {
			if rb.lattice[vid2].nextAck[vid] < min {
				min = rb.lattice[vid2].nextAck[vid]
			}
		}
		// "min" is the height of "next" last acked, min - 1 is the last height.
		// Delete blocks from min - 2 which will never be acked.
		if min < 3 {
			continue
		}
		min -= 2
		for {
			b, exist := rb.lattice[vid].blocks[min]
			if !exist {
				break
			}
			if b.Status >= types.BlockStatusOrdering {
				delete(rb.lattice[vid].blocks, b.Height)
				delete(rb.blocks, b.Hash)
			}
			if min == 0 {
				break
			}
			min--
		}
	}
	return
}

// extractBlocks returns all blocks that can be inserted into total ordering's
// DAG. This function changes the status of blocks from types.BlockStatusAcked
// to blockStatusOrdering.
func (rb *reliableBroadcast) extractBlocks() []*types.Block {
	ret := []*types.Block{}
	for {
		updated := false
		for vid := range rb.lattice {
			b, exist := rb.lattice[vid].blocks[rb.lattice[vid].nextOutput]
			if !exist || b.Status < types.BlockStatusAcked {
				continue
			}
			allAcksInOrderingStatus := true
			// Check if all acks are in ordering or above status. If a block of an ack
			// does not exist means that it deleted but its status is definitely Acked
			// or ordering.
			for ackHash := range b.Acks {
				bAck, exist := rb.blocks[ackHash]
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
			ret = append(ret, b)
			rb.lattice[vid].nextOutput++
		}
		if !updated {
			break
		}
	}
	return ret
}

// prepareBlock helps to setup fields of block based on its ProposerID,
// including:
//  - Set 'Acks' and 'Timestamps' for the highest block of each validator not
//    acked by this proposer before.
//  - Set 'ParentHash' and 'Height' from parent block, if we can't find a
//    parent, these fields would be setup like a genesis block.
func (rb *reliableBroadcast) prepareBlock(block *types.Block) {
	// Reset fields to make sure we got these information from parent block.
	block.Height = 0
	block.ParentHash = common.Hash{}
	// The helper function to accumulate timestamps.
	accumulateTimestamps := func(
		times map[types.ValidatorID]time.Time, b *types.Block) {

		// Update timestamps with the block's proposer time.
		// TODO (mission): make epslon configurable.
		times[b.ProposerID] = b.Timestamps[b.ProposerID].Add(
			1 * time.Millisecond)

		// Update timestamps from the block if it's later than
		// current cached ones.
		for vID, t := range b.Timestamps {
			cachedTime, exists := times[vID]
			if !exists {
				// This means the block contains timestamps from
				// removed validators.
				continue
			}
			if cachedTime.After(t) {
				continue
			}
			times[vID] = t
		}
		return
	}
	// Initial timestamps with current validator set.
	times := make(map[types.ValidatorID]time.Time)
	for vID := range rb.lattice {
		times[vID] = time.Time{}
	}
	acks := make(map[common.Hash]struct{})
	for vID := range rb.lattice {
		// find height of the latest block for that validator.
		var (
			curBlock   *types.Block
			nextHeight = rb.lattice[block.ProposerID].nextAck[vID]
		)

		for {
			tmpBlock, exists := rb.lattice[vID].blocks[nextHeight]
			if !exists {
				break
			}
			curBlock = tmpBlock
			nextHeight++
		}
		if curBlock == nil {
			continue
		}
		acks[curBlock.Hash] = struct{}{}
		accumulateTimestamps(times, curBlock)
		if vID == block.ProposerID {
			block.ParentHash = curBlock.Hash
			block.Height = curBlock.Height + 1
		}
	}
	block.Timestamps = times
	block.Acks = acks
	return
}

// addValidator adds validator in the validator set.
func (rb *reliableBroadcast) addValidator(h types.ValidatorID) {
	rb.lattice[h] = &ackingValidatorStatus{
		blocks:     make(map[uint64]*types.Block),
		nextAck:    make(map[types.ValidatorID]uint64),
		nextOutput: 0,
		restricted: false,
	}
}

// deleteValidator deletes validator in validator set.
func (rb *reliableBroadcast) deleteValidator(h types.ValidatorID) {
	for h := range rb.lattice {
		delete(rb.lattice[h].nextAck, h)
	}
	delete(rb.lattice, h)
}
