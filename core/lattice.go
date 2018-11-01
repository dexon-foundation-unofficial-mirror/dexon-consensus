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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for sanity check error.
var (
	ErrRetrySanityCheckLater = fmt.Errorf("retry sanity check later")
)

// Lattice represents a unit to produce a global ordering from multiple chains.
type Lattice struct {
	lock       sync.RWMutex
	authModule *Authenticator
	app        Application
	debug      Debug
	pool       blockPool
	retryAdd   bool
	data       *latticeData
	toModule   *totalOrdering
	ctModule   *consensusTimestamp
	logger     common.Logger
}

// NewLattice constructs an Lattice instance.
func NewLattice(
	dMoment time.Time,
	cfg *types.Config,
	authModule *Authenticator,
	app Application,
	debug Debug,
	db blockdb.BlockDatabase,
	logger common.Logger) (s *Lattice) {
	// Create genesis latticeDataConfig.
	dataConfig := newGenesisLatticeDataConfig(dMoment, cfg)
	toConfig := newGenesisTotalOrderingConfig(dMoment, cfg)
	s = &Lattice{
		authModule: authModule,
		app:        app,
		debug:      debug,
		pool:       newBlockPool(cfg.NumChains),
		data:       newLatticeData(db, dataConfig),
		toModule:   newTotalOrdering(toConfig),
		ctModule:   newConsensusTimestamp(dMoment, 0, cfg.NumChains),
		logger:     logger,
	}
	return
}

// PrepareBlock setup block's field based on current lattice status.
func (s *Lattice) PrepareBlock(
	b *types.Block, proposeTime time.Time) (err error) {

	s.lock.RLock()
	defer s.lock.RUnlock()

	b.Timestamp = proposeTime
	if err = s.data.prepareBlock(b); err != nil {
		return
	}
	s.logger.Debug("Calling Application.PreparePayload", "position", b.Position)
	if b.Payload, err = s.app.PreparePayload(b.Position); err != nil {
		return
	}
	s.logger.Debug("Calling Application.PrepareWitness",
		"height", b.Witness.Height)
	if b.Witness, err = s.app.PrepareWitness(b.Witness.Height); err != nil {
		return
	}
	if err = s.authModule.SignBlock(b); err != nil {
		return
	}
	return
}

// PrepareEmptyBlock setup block's field based on current lattice status.
func (s *Lattice) PrepareEmptyBlock(b *types.Block) (err error) {
	s.lock.RLock()
	defer s.lock.RUnlock()
	s.data.prepareEmptyBlock(b)
	if b.Hash, err = hashBlock(b); err != nil {
		return
	}
	return
}

// SanityCheck check if a block is valid.
//
// If some acking blocks don't exists, Lattice would help to cache this block
// and retry when lattice updated in Lattice.ProcessBlock.
func (s *Lattice) SanityCheck(b *types.Block) (err error) {
	if b.IsEmpty() {
		// Only need to verify block's hash.
		var hash common.Hash
		if hash, err = hashBlock(b); err != nil {
			return
		}
		if b.Hash != hash {
			return ErrInvalidBlock
		}
	} else {
		// Verify block's signature.
		if err = s.authModule.VerifyBlock(b); err != nil {
			return
		}
	}
	// Make sure acks are sorted.
	for i := range b.Acks {
		if i == 0 {
			continue
		}
		if !b.Acks[i-1].Less(b.Acks[i]) {
			err = ErrAcksNotSorted
			return
		}
	}
	if err = func() (err error) {
		s.lock.RLock()
		defer s.lock.RUnlock()
		if err = s.data.sanityCheck(b); err != nil {
			if _, ok := err.(*ErrAckingBlockNotExists); ok {
				err = ErrRetrySanityCheckLater
			}
			s.logger.Error("Sanity Check failed", "error", err)
			return
		}
		return
	}(); err != nil {
		return
	}
	// Verify data in application layer.
	s.logger.Debug("Calling Application.VerifyBlock", "block", b)
	switch s.app.VerifyBlock(b) {
	case types.VerifyInvalidBlock:
		err = ErrInvalidBlock
	case types.VerifyRetryLater:
		err = ErrRetrySanityCheckLater
	}
	return
}

// addBlockToLattice adds a block into lattice, and deliver blocks with the acks
// already delivered.
//
// NOTE: assume the block passed sanity check.
func (s *Lattice) addBlockToLattice(
	input *types.Block) (outputBlocks []*types.Block, err error) {
	if tip := s.data.chains[input.Position.ChainID].tip; tip != nil {
		if !input.Position.Newer(&tip.Position) {
			return
		}
	}
	s.pool.addBlock(input)
	// Replay tips in pool to check their validity.
	for {
		hasOutput := false
		for i := uint32(0); i < uint32(len(s.pool)); i++ {
			var tip *types.Block
			if tip = s.pool.tip(i); tip == nil {
				continue
			}
			err = s.data.sanityCheck(tip)
			if err == nil {
				var output []*types.Block
				if output, err = s.data.addBlock(tip); err != nil {
					s.logger.Error("Sanity Check failed", "error", err)
					continue
				}
				hasOutput = true
				outputBlocks = append(outputBlocks, output...)
			}
			if _, ok := err.(*ErrAckingBlockNotExists); ok {
				err = nil
				continue
			}
			s.pool.removeTip(i)
		}
		if !hasOutput {
			break
		}
	}

	for _, b := range outputBlocks {
		// TODO(jimmy-dexon): change this name of classic DEXON algorithm.
		if s.debug != nil {
			s.debug.StronglyAcked(b.Hash)
		}
		s.logger.Debug("Calling Application.BlockConfirmed", "block", input)
		s.app.BlockConfirmed(*b.Clone())
		// Purge blocks in pool with the same chainID and lower height.
		s.pool.purgeBlocks(b.Position.ChainID, b.Position.Height)
	}

	return
}

// ProcessBlock adds a block into lattice, and deliver ordered blocks.
// If any block pass sanity check after this block add into lattice, they
// would be returned, too.
//
// NOTE: assume the block passed sanity check.
func (s *Lattice) ProcessBlock(
	input *types.Block) (delivered []*types.Block, err error) {
	var (
		b             *types.Block
		inLattice     []*types.Block
		toDelivered   []*types.Block
		deliveredMode uint32
	)

	s.lock.Lock()
	defer s.lock.Unlock()

	if inLattice, err = s.addBlockToLattice(input); err != nil {
		return
	}

	if len(inLattice) == 0 {
		return
	}

	// Perform total ordering for each block added to lattice.
	for _, b = range inLattice {
		toDelivered, deliveredMode, err = s.toModule.processBlock(b)
		if err != nil {
			// All errors from total ordering is serious, should panic.
			panic(err)
		}
		if len(toDelivered) == 0 {
			continue
		}
		hashes := make(common.Hashes, len(toDelivered))
		for idx := range toDelivered {
			hashes[idx] = toDelivered[idx].Hash
		}
		if s.debug != nil {
			s.debug.TotalOrderingDelivered(hashes, deliveredMode)
		}
		// Perform timestamp generation.
		if err = s.ctModule.processBlocks(toDelivered); err != nil {
			return
		}
		delivered = append(delivered, toDelivered...)
	}
	return
}

// NextPosition returns expected position of incoming block for that chain.
func (s *Lattice) NextPosition(chainID uint32) types.Position {
	s.lock.RLock()
	defer s.lock.RUnlock()

	return s.data.nextPosition(chainID)
}

// PurgeBlocks from cache of blocks in memory, this is called when the caller
// make sure those blocks are saved to db.
func (s *Lattice) PurgeBlocks(blocks []*types.Block) error {
	s.lock.Lock()
	defer s.lock.Unlock()

	return s.data.purgeBlocks(blocks)
}

// AppendConfig add new configs for upcoming rounds. If you add a config for
// round R, next time you can only add the config for round R+1.
func (s *Lattice) AppendConfig(round uint64, config *types.Config) (err error) {
	s.lock.Lock()
	defer s.lock.Unlock()

	s.pool.resize(config.NumChains)
	if err = s.data.appendConfig(round, config); err != nil {
		return
	}
	if err = s.toModule.appendConfig(round, config); err != nil {
		return
	}
	if err = s.ctModule.appendConfig(round, config); err != nil {
		return
	}
	return
}

// ProcessFinalizedBlock is used for syncing lattice data.
func (s *Lattice) ProcessFinalizedBlock(input *types.Block) {
	defer func() { s.retryAdd = true }()
	s.lock.Lock()
	defer s.lock.Unlock()
	if err := s.data.addFinalizedBlock(input); err != nil {
		panic(err)
	}
	s.pool.purgeBlocks(input.Position.ChainID, input.Position.Height)
}
