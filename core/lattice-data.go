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
	"errors"
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for sanity check error.
var (
	ErrAckingBlockNotExists    = fmt.Errorf("acking block not exists")
	ErrInvalidParentChain      = fmt.Errorf("invalid parent chain")
	ErrDuplicatedAckOnOneChain = fmt.Errorf("duplicated ack on one chain")
	ErrChainStatusCorrupt      = fmt.Errorf("chain status corrupt")
	ErrInvalidChainID          = fmt.Errorf("invalid chain id")
	ErrInvalidProposerID       = fmt.Errorf("invalid proposer id")
	ErrInvalidTimestamp        = fmt.Errorf("invalid timestamp")
	ErrInvalidWitness          = fmt.Errorf("invalid witness data")
	ErrInvalidBlock            = fmt.Errorf("invalid block")
	ErrForkBlock               = fmt.Errorf("fork block")
	ErrNotAckParent            = fmt.Errorf("not ack parent")
	ErrDoubleAck               = fmt.Errorf("double ack")
	ErrAcksNotSorted           = fmt.Errorf("acks not sorted")
	ErrInvalidBlockHeight      = fmt.Errorf("invalid block height")
	ErrAlreadyInLattice        = fmt.Errorf("block already in lattice")
	ErrIncorrectBlockTime      = fmt.Errorf("block timestamp is incorrect")
	ErrUnknownRoundID          = fmt.Errorf("unknown round id")
)

// Errors for method usage
var (
	ErrRoundNotIncreasing  = errors.New("round not increasing")
	ErrPurgedBlockNotFound = errors.New("purged block not found")
)

// latticeDataConfig is the configuration for latticeData for each round.
type latticeDataConfig struct {
	// Number of chains between runs
	prevNumChains uint32
	numChains     uint32
	// Block interval specifies reasonable time difference between
	// parent/child blocks.
	minBlockTimeInterval time.Duration
	maxBlockTimeInterval time.Duration
	// the range of clean cut at the beginning of this round.
	cleanCutRange int
}

// newLatticeDataConfig constructs a latticeDataConfig instance.
func newLatticeDataConfig(prev, cur *types.Config) *latticeDataConfig {
	config := &latticeDataConfig{
		numChains:            cur.NumChains,
		minBlockTimeInterval: cur.MinBlockInterval,
		maxBlockTimeInterval: cur.MaxBlockInterval,
	}
	if prev != nil {
		config.prevNumChains = prev.NumChains
		if prev.K != cur.K ||
			prev.PhiRatio != cur.PhiRatio ||
			prev.NumChains != cur.NumChains {
			// K plus 2 is a magic from GOD.
			config.cleanCutRange = prev.K + 2
		}
	}
	return config
}

// latticeData is a module for storing lattice.
type latticeData struct {
	// we need blockdb to read blocks purged from cache in memory.
	db blockdb.Reader
	// chains stores chains' blocks and other info.
	chains []*chainStatus
	// blockByHash stores blocks, indexed by block hash.
	blockByHash map[common.Hash]*types.Block
	// This stores configuration for each round.
	configs  []*latticeDataConfig
	minRound uint64
}

// newLatticeData creates a new latticeData struct.
func newLatticeData(db blockdb.Reader,
	round uint64, config *latticeDataConfig) (data *latticeData) {
	data = &latticeData{
		db:          db,
		chains:      make([]*chainStatus, config.numChains),
		blockByHash: make(map[common.Hash]*types.Block),
		configs:     []*latticeDataConfig{config},
		minRound:    round,
	}
	for i := range data.chains {
		data.chains[i] = &chainStatus{
			ID:      uint32(i),
			blocks:  []*types.Block{},
			nextAck: make([]uint64, config.numChains),
		}
	}
	return
}

func (data *latticeData) sanityCheck(b *types.Block) error {
	config := data.getConfig(b.Position.Round)
	if config == nil {
		return ErrUnknownRoundID
	}
	// Check if the chain id is valid.
	if b.Position.ChainID >= config.numChains {
		return ErrInvalidChainID
	}

	// TODO(mission): Check if its proposer is in validator set somewhere,
	//                lattice doesn't have to know about node set.

	// Check if it forks
	if bInLattice := data.chains[b.Position.ChainID].getBlockByHeight(
		b.Position.Height); bInLattice != nil {

		if b.Hash != bInLattice.Hash {
			return ErrForkBlock
		}
		return ErrAlreadyInLattice
	}
	// TODO(mission): check if fork by loading blocks from DB if the block
	//                doesn't exists because forking is serious.

	// Check if it acks older blocks.
	acksByChainID := make(map[uint32]struct{}, len(data.chains))
	for _, hash := range b.Acks {
		bAck, err := data.findBlock(hash)
		if err != nil {
			if err == blockdb.ErrBlockDoesNotExist {
				return ErrAckingBlockNotExists
			}
			return err
		}
		if bAck.Position.Height <
			data.chains[bAck.Position.ChainID].nextAck[b.Position.ChainID] {
			return ErrDoubleAck
		}
		// Check if ack two blocks on the same chain. This would need
		// to check after we replace map with slice for acks.
		if _, acked := acksByChainID[bAck.Position.ChainID]; acked {
			return ErrDuplicatedAckOnOneChain
		}
		acksByChainID[bAck.Position.ChainID] = struct{}{}
	}

	// Check non-genesis blocks if it acks its parent.
	if b.Position.Height > 0 {
		if !b.IsAcking(b.ParentHash) {
			return ErrNotAckParent
		}
		bParent, err := data.findBlock(b.ParentHash)
		if err != nil {
			// This error should never happened, a parent block is always
			// an acked block.
			panic(err)
		}
		if bParent.Position.ChainID != b.Position.ChainID {
			return ErrInvalidParentChain
		}
		if bParent.Position.Height != b.Position.Height-1 {
			return ErrInvalidBlockHeight
		}
		if bParent.Witness.Height > b.Witness.Height {
			return ErrInvalidWitness
		}
		// Check if its timestamp is valid.
		if !b.Timestamp.After(bParent.Timestamp) {
			return ErrInvalidTimestamp
		}
		// Check if its timestamp is in expected range.
		if b.Timestamp.Before(
			bParent.Timestamp.Add(config.minBlockTimeInterval)) ||
			b.Timestamp.After(
				bParent.Timestamp.Add(config.maxBlockTimeInterval)) {

			return ErrIncorrectBlockTime
		}
	}
	return nil
}

// addBlock processes block, it does sanity check, inserts block into
// lattice and deletes blocks which will not be used.
func (data *latticeData) addBlock(
	block *types.Block) (deliverable []*types.Block, err error) {

	var (
		bAck    *types.Block
		updated bool
	)
	// TODO(mission): sanity check twice, might hurt performance.
	// If a block does not pass sanity check, report error.
	if err = data.sanityCheck(block); err != nil {
		return
	}
	if err = data.chains[block.Position.ChainID].addBlock(block); err != nil {
		return
	}
	data.blockByHash[block.Hash] = block
	// Update nextAcks.
	for _, ack := range block.Acks {
		if bAck, err = data.findBlock(ack); err != nil {
			return
		}
		data.chains[bAck.Position.ChainID].nextAck[block.Position.ChainID] =
			bAck.Position.Height + 1
	}
	// Extract blocks that deliverable to total ordering.
	// A block is deliverable to total ordering iff:
	//  - All its acking blocks are delivered to total ordering.
	for {
		updated = false
		for _, status := range data.chains {
			tip := status.getBlockByHeight(status.nextOutput)
			if tip == nil {
				continue
			}
			allAckingBlockDelivered := true
			for _, ack := range tip.Acks {
				if bAck, err = data.findBlock(ack); err != nil {
					return
				}
				if data.chains[bAck.Position.ChainID].nextOutput >
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
	for _, status := range data.chains {
		status.purge()
	}
	return
}

// prepareBlock helps to setup fields of block based on its ProposerID,
// including:
//  - Set 'Acks' and 'Timestamps' for the highest block of each validator not
//    acked by this proposer before.
//  - Set 'ParentHash' and 'Height' from parent block, if we can't find a
//    parent, these fields would be setup like a genesis block.
func (data *latticeData) prepareBlock(block *types.Block) {
	config := data.getConfig(block.Position.Round)
	// Reset fields to make sure we got these information from parent block.
	block.Position.Height = 0
	block.ParentHash = common.Hash{}
	acks := common.Hashes{}
	for chainID := range data.chains {
		// find height of the latest block for that validator.
		var (
			curBlock   *types.Block
			nextHeight = data.chains[chainID].nextAck[block.Position.ChainID]
		)
		for {
			tmpBlock := data.chains[chainID].getBlockByHeight(nextHeight)
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
			block.Witness.Height = curBlock.Witness.Height
			minTimestamp := curBlock.Timestamp.Add(config.minBlockTimeInterval)
			maxTimestamp := curBlock.Timestamp.Add(config.maxBlockTimeInterval)
			if block.Timestamp.Before(minTimestamp) {
				block.Timestamp = minTimestamp
			} else if block.Timestamp.After(maxTimestamp) {
				block.Timestamp = maxTimestamp
			}
		}
	}
	block.Acks = common.NewSortedHashes(acks)
	return
}

// TODO(mission): make more abstraction for this method.
// nextHeight returns the next height for the chain.
func (data *latticeData) nextPosition(chainID uint32) types.Position {
	return data.chains[chainID].nextPosition()
}

// findBlock seeks blocks in memory or db.
func (data *latticeData) findBlock(h common.Hash) (b *types.Block, err error) {
	if b = data.blockByHash[h]; b != nil {
		return
	}
	var tmpB types.Block
	if tmpB, err = data.db.Get(h); err != nil {
		return
	}
	b = &tmpB
	return
}

// purgeBlocks purges blocks from cache.
func (data *latticeData) purgeBlocks(blocks []*types.Block) error {
	for _, b := range blocks {
		if _, exists := data.blockByHash[b.Hash]; !exists {
			return ErrPurgedBlockNotFound
		}
		delete(data.blockByHash, b.Hash)
	}
	return nil
}

// getConfig get configuration for lattice-data by round ID.
func (data *latticeData) getConfig(round uint64) (config *latticeDataConfig) {
	if round < data.minRound {
		return
	}
	diff := round - data.minRound
	if diff >= uint64(len(data.configs)) {
		return
	}
	return data.configs[diff]
}

// appendConfig appends a configuration for upcoming round. When you append
// a config for round R, next time you can only append the config for round R+1.
func (data *latticeData) appendConfig(
	round uint64, config *latticeDataConfig) (err error) {

	if round != data.minRound+uint64(len(data.configs)) {
		return ErrRoundNotIncreasing
	}
	data.configs = append(data.configs, config)
	return nil
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
func (s *chainStatus) purge() {
	safe, ok := s.calcPurgeHeight()
	if !ok {
		return
	}
	newMinIndex := safe - s.minHeight + 1
	s.blocks = s.blocks[newMinIndex:]
	s.minHeight = safe + 1
	return
}

// nextPosition returns a valid position for new block in this chain.
func (s *chainStatus) nextPosition() types.Position {
	return types.Position{
		ChainID: s.ID,
		Height:  s.minHeight + uint64(len(s.blocks)),
	}
}
