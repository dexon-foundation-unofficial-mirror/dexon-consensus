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

// Status represents the block process state.
type blockStatus int

// Block Status.
const (
	blockStatusInit blockStatus = iota
	blockStatusAcked
	blockStatusOrdering
	blockStatusFinal
)

// reliableBroadcast is a module for reliable broadcast.
type reliableBroadcast struct {
	// lattice stores node's blocks and other info.
	lattice []*rbcNodeStatus

	// blockInfos stores block infos.
	blockInfos map[common.Hash]*rbcBlockInfo

	// receivedBlocks stores blocks which is received but its acks are not all
	// in lattice.
	receivedBlocks map[common.Hash]*types.Block

	// nodes stores node set.
	nodes map[types.NodeID]struct{}
}

type rbcNodeStatus struct {
	// blocks stores blocks proposed by specified node in map which key is
	// the height of the block.
	blocks map[uint64]*types.Block

	// nextAck stores the height of next height that should be acked, i.e. last
	// acked height + 1. Initialized to 0, when genesis blocks are still not
	// being acked. For example, rb.lattice[vid1].NextAck[vid2] - 1 is the last
	// acked height by vid1 acking vid2.
	nextAck []uint64

	// nextOutput is the next output height of block, default to 0.
	nextOutput uint64

	// nextHeight is the next height of block to be prepared.
	nextHeight uint64
}

type rbcBlockInfo struct {
	block        *types.Block
	receivedTime time.Time
	status       blockStatus
	ackedChain   map[uint32]struct{}
}

// Errors for sanity check error.
var (
	ErrInvalidChainID     = fmt.Errorf("invalid chain id")
	ErrInvalidProposerID  = fmt.Errorf("invalid proposer id")
	ErrInvalidTimestamp   = fmt.Errorf("invalid timestamp")
	ErrForkBlock          = fmt.Errorf("fork block")
	ErrNotAckParent       = fmt.Errorf("not ack parent")
	ErrDoubleAck          = fmt.Errorf("double ack")
	ErrInvalidBlockHeight = fmt.Errorf("invalid block height")
	ErrAlreadyInLattice   = fmt.Errorf("block already in lattice")
)

// newReliableBroadcast creates a new reliableBroadcast struct.
func newReliableBroadcast() *reliableBroadcast {
	return &reliableBroadcast{
		blockInfos:     make(map[common.Hash]*rbcBlockInfo),
		receivedBlocks: make(map[common.Hash]*types.Block),
		nodes:          make(map[types.NodeID]struct{}),
	}
}

func (rb *reliableBroadcast) sanityCheck(b *types.Block) error {
	// Check if the chain id is valid.
	if b.Position.ChainID >= uint32(len(rb.lattice)) {
		return ErrInvalidChainID
	}

	// Check if its proposer is in node set.
	if _, exist := rb.nodes[b.ProposerID]; !exist {
		return ErrInvalidProposerID
	}

	// Check if it forks.
	if bInLattice, exist :=
		rb.lattice[b.Position.ChainID].blocks[b.Position.Height]; exist {
		if b.Hash != bInLattice.Hash {
			return ErrForkBlock
		}
		return ErrAlreadyInLattice
	}

	// Check non-genesis blocks if it acks its parent.
	if b.Position.Height > 0 {
		if !b.IsAcking(b.ParentHash) {
			return ErrNotAckParent
		}
		bParentStat, exists := rb.blockInfos[b.ParentHash]
		if exists && bParentStat.block.Position.Height != b.Position.Height-1 {
			return ErrInvalidBlockHeight
		}
	}

	// Check if it acks older blocks.
	for _, hash := range b.Acks {
		if bAckStat, exist := rb.blockInfos[hash]; exist {
			bAck := bAckStat.block
			if bAck.Position.Height <
				rb.lattice[b.Position.ChainID].nextAck[bAck.Position.ChainID] {
				return ErrDoubleAck
			}
		}
	}

	// Check if its timestamp is valid.
	if bParent, exist :=
		rb.lattice[b.Position.ChainID].blocks[b.Position.Height-1]; exist {
		if !b.Timestamp.After(bParent.Timestamp) {
			return ErrInvalidTimestamp
		}
	}

	// TODO(haoping): application layer check of block's content

	return nil
}

// areAllAcksReceived checks if all ack blocks of a block are all in lattice.
func (rb *reliableBroadcast) areAllAcksInLattice(b *types.Block) bool {
	for _, h := range b.Acks {
		bAckStat, exist := rb.blockInfos[h]
		if !exist {
			return false
		}
		bAck := bAckStat.block

		bAckInLattice, exist :=
			rb.lattice[bAck.Position.ChainID].blocks[bAck.Position.Height]
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
	rb.blockInfos[block.Hash] = &rbcBlockInfo{
		block:        block,
		receivedTime: time.Now().UTC(),
		ackedChain:   make(map[uint32]struct{}),
	}
	rb.receivedBlocks[block.Hash] = block
	if rb.lattice[block.Position.ChainID].nextHeight <= block.Position.Height {
		rb.lattice[block.Position.ChainID].nextHeight = block.Position.Height + 1
	}

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
				delete(rb.blockInfos, b.Hash)
				delete(rb.receivedBlocks, b.Hash)
				continue
				// TODO(mission): how to return for multiple errors?
			}
			chainID := b.Position.ChainID
			rb.lattice[chainID].blocks[b.Position.Height] = b
			delete(rb.receivedBlocks, b.Hash)
			for _, h := range b.Acks {
				bAckStat := rb.blockInfos[h]
				// Update nextAck only when bAckStat.block.Position.Height + 1
				// is greater. A block might ack blocks proposed by same node with
				// different height.
				if rb.lattice[chainID].nextAck[bAckStat.block.Position.ChainID] <
					bAckStat.block.Position.Height+1 {
					rb.lattice[chainID].nextAck[bAckStat.block.Position.ChainID] =
						bAckStat.block.Position.Height + 1
				}
				// Update ackedChain for each ack blocks and its parents.
				for {
					if _, exist := bAckStat.ackedChain[chainID]; exist {
						break
					}
					if bAckStat.status > blockStatusInit {
						break
					}
					bAckStat.ackedChain[chainID] = struct{}{}
					// A block is strongly acked if it is acked by more than
					// 2 * (maximum number of byzatine nodes) unique nodes.
					if len(bAckStat.ackedChain) > 2*((len(rb.lattice)-1)/3) {
						blocksToAcked[bAckStat.block.Hash] = bAckStat.block
					}
					if bAckStat.block.Position.Height == 0 {
						break
					}
					bAckStat = rb.blockInfos[bAckStat.block.ParentHash]
				}
			}
		}
	}

	for _, b := range blocksToAcked {
		rb.blockInfos[b.Hash].status = blockStatusAcked
	}

	// Delete blocks in received array when it is received a long time ago.
	oldBlocks := []common.Hash{}
	for h, b := range rb.receivedBlocks {
		if time.Now().Sub(rb.blockInfos[b.Hash].receivedTime) >= 30*time.Second {
			oldBlocks = append(oldBlocks, h)
		}
	}
	for _, h := range oldBlocks {
		delete(rb.receivedBlocks, h)
		delete(rb.blockInfos, h)
	}

	// Delete old blocks in "lattice" and "blocks" for release memory space.
	// First, find the height that blocks below it can be deleted. This height
	// is defined by finding minimum of node's nextOutput and last acking
	// heights from other nodes, i.e. rb.lattice[v_other].nextAck[this_vid].
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
			if rb.blockInfos[b.Hash].status >= blockStatusOrdering {
				delete(rb.lattice[vid].blocks, b.Position.Height)
				delete(rb.blockInfos, b.Hash)
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
// DAG. This function changes the status of blocks from blockStatusAcked to
// blockStatusOrdering.
func (rb *reliableBroadcast) extractBlocks() []*types.Block {
	ret := []*types.Block{}
	for {
		updated := false
		for vid := range rb.lattice {
			b, exist := rb.lattice[vid].blocks[rb.lattice[vid].nextOutput]
			if !exist || rb.blockInfos[b.Hash].status < blockStatusAcked {
				continue
			}
			allAcksInOrderingStatus := true
			// Check if all acks are in ordering or above status. If a block of an ack
			// does not exist means that it deleted but its status is definitely Acked
			// or ordering.
			for _, ackHash := range b.Acks {
				bAckStat, exist := rb.blockInfos[ackHash]
				if !exist {
					continue
				}
				if bAckStat.status < blockStatusOrdering {
					allAcksInOrderingStatus = false
					break
				}
			}
			if !allAcksInOrderingStatus {
				continue
			}
			updated = true
			rb.blockInfos[b.Hash].status = blockStatusOrdering
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
//  - Set 'Acks' and 'Timestamps' for the highest block of each node not
//    acked by this proposer before.
//  - Set 'ParentHash' and 'Height' from parent block, if we can't find a
//    parent, these fields would be setup like a genesis block.
func (rb *reliableBroadcast) prepareBlock(block *types.Block) {
	// Reset fields to make sure we got these information from parent block.
	block.Position.Height = 0
	block.ParentHash = common.Hash{}
	acks := common.Hashes{}
	for chainID := range rb.lattice {
		// find height of the latest block for that node.
		var (
			curBlock   *types.Block
			nextHeight = rb.lattice[block.Position.ChainID].nextAck[chainID]
		)

		for {
			tmpBlock, exists := rb.lattice[chainID].blocks[nextHeight]
			if !exists {
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
			if block.Timestamp.Before(curBlock.Timestamp) {
				// TODO (mission): make epslon configurable.
				block.Timestamp = curBlock.Timestamp.Add(1 * time.Millisecond)
			}
			if block.Position.Height == 0 {
				block.Position.Height = curBlock.Position.Height + 1
			}
		}
	}
	block.Acks = common.NewSortedHashes(acks)
	return
}

// addNode adds node in the node set.
func (rb *reliableBroadcast) addNode(h types.NodeID) {
	rb.nodes[h] = struct{}{}
}

// deleteNode deletes node in node set.
func (rb *reliableBroadcast) deleteNode(h types.NodeID) {
	delete(rb.nodes, h)
}

// setChainNum set the number of chains.
func (rb *reliableBroadcast) setChainNum(num uint32) {
	rb.lattice = make([]*rbcNodeStatus, num)
	for i := range rb.lattice {
		rb.lattice[i] = &rbcNodeStatus{
			blocks:     make(map[uint64]*types.Block),
			nextAck:    make([]uint64, num),
			nextOutput: 0,
			nextHeight: 0,
		}
	}
}

func (rb *reliableBroadcast) chainNum() uint32 {
	return uint32(len(rb.lattice))
}

// nextHeight returns the next height for the chain.
func (rb *reliableBroadcast) nextHeight(chainID uint32) uint64 {
	return rb.lattice[chainID].nextHeight
}
