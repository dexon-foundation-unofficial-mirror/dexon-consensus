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
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// Shard represents a unit to produce a global ordering from multiple chains.
type Shard struct {
	lock     sync.RWMutex
	ID       uint32
	nodeID   types.NodeID
	prvKey   crypto.PrivateKey
	sigToPub SigToPubFn
	chainNum uint32
	app      Application
	debug    Debug
	db       blockdb.BlockDatabase
	pool     blockPool
	lattice  *blockLattice
	toModule *totalOrdering
	ctModule *consensusTimestamp
}

// NewShard constructs an Shard instance.
func NewShard(
	ID uint32,
	cfg *types.Config,
	prvKey crypto.PrivateKey,
	sigToPub SigToPubFn,
	app Application,
	debug Debug,
	db blockdb.BlockDatabase) (s *Shard) {

	s = &Shard{
		ID:       ID,
		nodeID:   types.NewNodeID(prvKey.PublicKey()),
		prvKey:   prvKey,
		sigToPub: sigToPub,
		chainNum: cfg.NumChains,
		app:      app,
		debug:    debug,
		db:       db,
		pool:     newBlockPool(cfg.NumChains),
		lattice:  newBlockLattice(ID, cfg.NumChains),
		toModule: newTotalOrdering(
			uint64(cfg.K),
			uint64(float32(cfg.NumChains-1)*cfg.PhiRatio+1),
			cfg.NumChains),
		ctModule: newConsensusTimestamp(),
	}
	return
}

// PrepareBlock setup block's field based on current lattice status.
func (s *Shard) PrepareBlock(
	b *types.Block, proposeTime time.Time) (err error) {

	s.lock.RLock()
	defer s.lock.RUnlock()

	s.lattice.prepareBlock(b)
	// TODO(mission): the proposeTime might be earlier than tip block of
	//                that chain. We should let blockLattice suggest the time.
	b.ProposerID = s.nodeID
	b.Timestamp = proposeTime
	b.Payload = s.app.PreparePayload(b.Position)
	if b.Hash, err = hashBlock(b); err != nil {
		return
	}
	if b.Signature, err = s.prvKey.Sign(b.Hash); err != nil {
		return
	}
	return
}

// SanityCheck check if a block is valid based on current lattice status.
//
// If some acking blocks don't exists, Shard would help to cache this block
// and retry when lattice updated in Shard.ProcessBlock.
func (s *Shard) SanityCheck(b *types.Block) (err error) {
	// Check block.Position.
	if b.Position.ShardID != s.ID {
		err = ErrIncorrectBlockPosition
		return
	}
	// Check the hash of block.
	hash, err := hashBlock(b)
	if err != nil || hash != b.Hash {
		err = ErrIncorrectHash
		return
	}
	// Check the signer.
	pubKey, err := s.sigToPub(b.Hash, b.Signature)
	if err != nil {
		return
	}
	if !b.ProposerID.Equal(crypto.Keccak256Hash(pubKey.Bytes())) {
		err = ErrIncorrectSignature
		return
	}
	s.lock.RLock()
	defer s.lock.RUnlock()
	if err = s.lattice.sanityCheck(b); err != nil {
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
func (s *Shard) ProcessBlock(
	input *types.Block) (verified, delivered []*types.Block, err error) {

	var (
		tip, b         *types.Block
		toDelivered    []*types.Block
		inLattice      []*types.Block
		earlyDelivered bool
	)
	s.lock.Lock()
	defer s.lock.Unlock()
	if inLattice, err = s.lattice.addBlock(input); err != nil {
		return
	}
	if err = s.db.Put(*input); err != nil {
		return
	}
	// TODO(mission): remove this hack, BA related stuffs should not
	//                be done here.
	if s.debug != nil {
		s.debug.StronglyAcked(input.Hash)
		s.debug.BlockConfirmed(input.Hash)
	}
	// Purge blocks in pool with the same chainID and lower height.
	s.pool.purgeBlocks(input.Position.ChainID, input.Position.Height)
	// Replay tips in pool to check their validity.
	for i := uint32(0); i < s.chainNum; i++ {
		if tip = s.pool.tip(i); tip == nil {
			continue
		}
		err = s.lattice.sanityCheck(tip)
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
			s.debug.TotalOrderingDeliver(hashes, earlyDelivered)
		}
		// Perform timestamp generation.
		if err = s.ctModule.processBlocks(toDelivered); err != nil {
			return
		}
		delivered = append(delivered, toDelivered...)
	}
	return
}
