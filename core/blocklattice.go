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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for sanity check error.
var (
	ErrAckingBlockNotExists    = fmt.Errorf("acking block not exists")
	ErrInvalidParentChain      = fmt.Errorf("invalid parent chain")
	ErrDuplicatedAckOnOneChain = fmt.Errorf("duplicated ack on one chain")
	ErrChainStatusCorrupt      = fmt.Errorf("chain status corrupt")
)

// blockLattice is a module for storing blocklattice.
type blockLattice struct {
	// lattice stores chains' blocks and other info.
	chains []*chainStatus

	// blockByHash stores blocks, indexed by block hash.
	blockByHash map[common.Hash]*types.Block

	// shardID caches which shard I belongs to.
	shardID uint32
}

type chainStatus struct {
	// ID keeps the chainID of this chain status.
	ID uint32

	// blocks stores blocks proposed for this chain, sorted by height.
	blocks []*types.Block

	// minHeight keeps minimum height in blocks.
	minHeight uint64

	// nextAck stores the height of next height that should be acked, i.e. last
	// acked height + 1. Initialized to 0.
	// being acked. For example, rb.chains[vid1].nextAck[vid2] - 1 is the last
	// acked height by vid2 acking vid1.
	nextAck []uint64

	// nextOutput is the next output height of block, default to 0.
	nextOutput uint64
}

func (s *chainStatus) getBlockByHeight(height uint64) (b *types.Block) {
	if height < s.minHeight {
		return
	}
	idx := int(height - s.minHeight)
	if idx >= len(s.blocks) {
		return
	}
	b = s.blocks[idx]
	return
}

func (s *chainStatus) addBlock(b *types.Block) error {
	if len(s.blocks) > 0 {
		// Make sure the height of incoming block should be
		// plus one to current latest blocks if exists.
		if s.blocks[len(s.blocks)-1].Position.Height != b.Position.Height-1 {
			return ErrChainStatusCorrupt
		}
	} else {
		if b.Position.Height != 0 {
			return ErrChainStatusCorrupt
		}
	}
	s.blocks = append(s.blocks, b)
	return nil
}

func (s *chainStatus) calcPurgeHeight() (safe uint64, ok bool) {
	// blocks with height less than min(nextOutput, nextAck...)
	// are safe to be purged.
	safe = s.nextOutput
	for _, ackedHeight := range s.nextAck {
		if safe > ackedHeight {
			safe = ackedHeight
		}
	}
	// Both 'nextOutput' and 'nextAck' represents some block to be
	// outputed/acked. To find a block already outputed/acked, the height
	// needs to be minus 1.
	if safe == 0 {
		// Avoid underflow.
		return
	}
	safe--
	if safe < s.minHeight {
		return
	}
	ok = true
	return
}

// purge blocks if they are safe to be deleted from working set.
func (s *chainStatus) purge() (purged common.Hashes) {
	safe, ok := s.calcPurgeHeight()
	if !ok {
		return
	}
	newMinIndex := safe - s.minHeight + 1
	for _, b := range s.blocks[:newMinIndex] {
		purged = append(purged, b.Hash)
	}
	s.blocks = s.blocks[newMinIndex:]
	s.minHeight = safe + 1
	return
}

// nextPosition returns a valid position for new block in this chain.
func (s *chainStatus) nextPosition(shardID uint32) types.Position {
	return types.Position{
		ChainID: s.ID,
		Height:  s.minHeight + uint64(len(s.blocks)),
	}
}

// newBlockLattice creates a new blockLattice struct.
func newBlockLattice(shardID, chainNum uint32) (bl *blockLattice) {
	bl = &blockLattice{
		shardID:     shardID,
		chains:      make([]*chainStatus, chainNum),
		blockByHash: make(map[common.Hash]*types.Block),
	}
	for i := range bl.chains {
		bl.chains[i] = &chainStatus{
			ID:      uint32(i),
			blocks:  []*types.Block{},
			nextAck: make([]uint64, chainNum),
		}
	}
	return
}

func (bl *blockLattice) sanityCheck(b *types.Block) error {
	// Check if the chain id is valid.
	if b.Position.ChainID >= uint32(len(bl.chains)) {
		return ErrInvalidChainID
	}

	// TODO(mission): Check if its proposer is in validator set somewhere,
	//                blocklattice doesn't have to know about node set.

	// Check if it forks
	if bInLattice := bl.chains[b.Position.ChainID].getBlockByHeight(
		b.Position.Height); bInLattice != nil {

		if b.Hash != bInLattice.Hash {
			return ErrForkBlock
		}
		return ErrAlreadyInLattice
	}
	// TODO(mission): check if fork by loading blocks from DB if the block
	//                doesn't exists because forking is serious.

	// Check if it acks older blocks.
	acksByChainID := make(map[uint32]struct{}, len(bl.chains))
	for _, hash := range b.Acks {
		if bAck, exist := bl.blockByHash[hash]; exist {
			if bAck.Position.Height <
				bl.chains[bAck.Position.ChainID].nextAck[b.Position.ChainID] {
				return ErrDoubleAck
			}
			// Check if ack two blocks on the same chain. This would need
			// to check after we replace map with slice for acks.
			if _, acked := acksByChainID[bAck.Position.ChainID]; acked {
				return ErrDuplicatedAckOnOneChain
			}
			acksByChainID[bAck.Position.ChainID] = struct{}{}
		} else {
			// This error has the same checking effect as areAllAcksInLattice.
			return ErrAckingBlockNotExists
		}
	}

	// Check non-genesis blocks if it acks its parent.
	if b.Position.Height > 0 {
		if !b.IsAcking(b.ParentHash) {
			return ErrNotAckParent
		}
		bParent := bl.blockByHash[b.ParentHash]
		if bParent.Position.ChainID != b.Position.ChainID {
			return ErrInvalidParentChain
		}
		if bParent.Position.Height != b.Position.Height-1 {
			return ErrInvalidBlockHeight
		}
		// Check if its timestamp is valid.
		if !b.Timestamp.After(bParent.Timestamp) {
			return ErrInvalidTimestamp
		}
	}
	return nil
}

// areAllAcksReceived checks if all ack blocks of a block are all in lattice,
// blockLattice would make sure all blocks not acked by some chain would be kept
// in working set.
func (bl *blockLattice) areAllAcksInLattice(b *types.Block) bool {
	for _, h := range b.Acks {
		bAck, exist := bl.blockByHash[h]
		if !exist {
			return false
		}
		if bAckInLattice := bl.chains[bAck.Position.ChainID].getBlockByHeight(
			bAck.Position.Height); bAckInLattice != nil {

			if bAckInLattice.Hash != bAck.Hash {
				panic("areAllAcksInLattice: blockLattice.chains has corrupted")
			}
		} else {
			return false
		}
	}
	return true
}

// addBlock processes block, it does sanity check, inserts block into
// lattice and deletes blocks which will not be used.
func (bl *blockLattice) addBlock(
	block *types.Block) (deliverable []*types.Block, err error) {

	var (
		bAck    *types.Block
		updated bool
	)
	// If a block does not pass sanity check, report error.
	if err = bl.sanityCheck(block); err != nil {
		return
	}
	if err = bl.chains[block.Position.ChainID].addBlock(block); err != nil {
		return
	}
	bl.blockByHash[block.Hash] = block
	// Update nextAcks.
	for _, ack := range block.Acks {
		bAck = bl.blockByHash[ack]
		bl.chains[bAck.Position.ChainID].nextAck[block.Position.ChainID] =
			bAck.Position.Height + 1
	}
	// Extract blocks that deliverable to total ordering.
	// A block is deliverable to total ordering iff:
	//  - All its acking blocks are delivered to total ordering.
	for {
		updated = false
		for _, status := range bl.chains {
			tip := status.getBlockByHeight(status.nextOutput)
			if tip == nil {
				continue
			}
			allAckingBlockDelivered := true
			for _, ack := range tip.Acks {
				bAck, exists := bl.blockByHash[ack]
				if !exists {
					continue
				}
				if bl.chains[bAck.Position.ChainID].nextOutput >
					bAck.Position.Height {

					continue
				}
				// This acked block exists and not delivered yet.
				allAckingBlockDelivered = false
			}
			if allAckingBlockDelivered {
				deliverable = append(deliverable, tip)
				status.nextOutput++
				updated = true
			}
		}
		if !updated {
			break
		}
	}

	// Delete old blocks in "chains" and "blocks" to release memory space.
	//
	// A block is safe to be deleted iff:
	//  - It's delivered to total ordering
	//  - All chains (including its proposing chain) acks some block with
	//    higher height in its proposing chain.
	//
	// This works because blocks of height below this minimum are not going to be
	// acked anymore, the ackings of these blocks are illegal.
	for _, status := range bl.chains {
		for _, h := range status.purge() {
			delete(bl.blockByHash, h)
		}
	}
	return
}

// prepareBlock helps to setup fields of block based on its ProposerID,
// including:
//  - Set 'Acks' and 'Timestamps' for the highest block of each validator not
//    acked by this proposer before.
//  - Set 'ParentHash' and 'Height' from parent block, if we can't find a
//    parent, these fields would be setup like a genesis block.
func (bl *blockLattice) prepareBlock(block *types.Block) {
	// Reset fields to make sure we got these information from parent block.
	block.Position.Height = 0
	block.ParentHash = common.Hash{}
	acks := common.Hashes{}
	for chainID := range bl.chains {
		// find height of the latest block for that validator.
		var (
			curBlock   *types.Block
			nextHeight = bl.chains[chainID].nextAck[block.Position.ChainID]
		)
		for {
			tmpBlock := bl.chains[chainID].getBlockByHeight(nextHeight)
			if tmpBlock == nil {
				break
			}
			curBlock = tmpBlock
			nextHeight++
		}
		if curBlock == nil {
			continue
		}
		acks = append(acks, curBlock.Hash)
		if uint32(chainID) == block.Position.ChainID {
			block.ParentHash = curBlock.Hash
			block.Position.Height = curBlock.Position.Height + 1
		}
	}
	block.Acks = common.NewSortedHashes(acks)
	return
}

// TODO(mission): make more abstraction for this method.
// nextHeight returns the next height for the chain.
func (bl *blockLattice) nextPosition(chainID uint32) types.Position {
	return bl.chains[chainID].nextPosition(bl.shardID)
}
