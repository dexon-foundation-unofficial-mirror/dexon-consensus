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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Lattice represents a unit to produce a global ordering from multiple chains.
type Lattice struct {
	lock       sync.RWMutex
	authModule *Authenticator
	chainNum   uint32
	app        Application
	debug      Debug
	pool       blockPool
	data       *latticeData
	toModule   *totalOrdering
	ctModule   *consensusTimestamp
}

// NewLattice constructs an Lattice instance.
func NewLattice(
	dMoment time.Time,
	cfg *types.Config,
	authModule *Authenticator,
	app Application,
	debug Debug,
	db blockdb.BlockDatabase) (s *Lattice) {
	// Create genesis latticeDataConfig.
	dataConfig := newGenesisLatticeDataConfig(dMoment, cfg)
	toConfig := newGenesisTotalOrderingConfig(dMoment, cfg)
	s = &Lattice{
		authModule: authModule,
		chainNum:   cfg.NumChains,
		app:        app,
		debug:      debug,
		pool:       newBlockPool(cfg.NumChains),
		data:       newLatticeData(db, dataConfig),
		toModule:   newTotalOrdering(toConfig),
		ctModule:   newConsensusTimestamp(dMoment, cfg.NumChains),
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
	if b.Payload, err = s.app.PreparePayload(b.Position); err != nil {
		return
	}
	if b.Witness, err = s.app.PrepareWitness(b.Witness.Height); err != nil {
		return
	}
	if err = s.authModule.SignBlock(b); err != nil {
		return
	}
	return
}

// SanityCheck check if a block is valid based on current lattice status.
//
// If some acking blocks don't exists, Lattice would help to cache this block
// and retry when lattice updated in Lattice.ProcessBlock.
func (s *Lattice) SanityCheck(b *types.Block) (err error) {
	// Verify block's signature.
	if err = s.authModule.VerifyBlock(b); err != nil {
		return
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
	// Verify data in application layer.
	if !s.app.VerifyBlock(b) {
		err = ErrInvalidBlock
		return err
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if err = s.data.sanityCheck(b); err != nil {
		// Add to block pool, once the lattice updated,
		// would be checked again.
		if err == ErrAckingBlockNotExists {
			s.pool.addBlock(b)
		}
		return
	}
	return
}

// ProcessBlock adds a block into lattice, and deliver ordered blocks.
// If any block pass sanity check after this block add into lattice, they
// would be returned, too.
//
// NOTE: assume the block passed sanity check.
func (s *Lattice) ProcessBlock(
	input *types.Block) (verified, delivered []*types.Block, err error) {

	var (
		tip, b         *types.Block
		toDelivered    []*types.Block
		inLattice      []*types.Block
		earlyDelivered bool
	)
	s.lock.Lock()
	defer s.lock.Unlock()
	if inLattice, err = s.data.addBlock(input); err != nil {
		// TODO(mission): if sanity check failed with "acking block doesn't
		//                exists", we should keep it in a pool.
		return
	}
	// TODO(mission): remove this hack, BA related stuffs should not
	//                be done here.
	if s.debug != nil {
		s.debug.StronglyAcked(input.Hash)
	}
	s.app.BlockConfirmed(*input.Clone())
	// Purge blocks in pool with the same chainID and lower height.
	s.pool.purgeBlocks(input.Position.ChainID, input.Position.Height)
	// Replay tips in pool to check their validity.
	for i := uint32(0); i < s.chainNum; i++ {
		if tip = s.pool.tip(i); tip == nil {
			continue
		}
		err = s.data.sanityCheck(tip)
		if err == nil {
			verified = append(verified, tip)
		}
		if err == ErrAckingBlockNotExists {
			continue
		}
		s.pool.removeTip(i)
	}
	// Perform total ordering for each block added to lattice.
	for _, b = range inLattice {
		toDelivered, earlyDelivered, err = s.toModule.processBlock(b)
		if err != nil {
			return
		}
		if len(toDelivered) == 0 {
			continue
		}
		hashes := make(common.Hashes, len(toDelivered))
		for idx := range toDelivered {
			hashes[idx] = toDelivered[idx].Hash
		}
		if s.debug != nil {
			s.debug.TotalOrderingDelivered(hashes, earlyDelivered)
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
