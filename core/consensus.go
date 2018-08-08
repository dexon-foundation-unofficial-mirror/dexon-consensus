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

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Consensus implements DEXON Consensus algorithm.
type Consensus struct {
	app      Application
	gov      Governance
	rbModule *reliableBroadcast
	toModule *totalOrdering
	ctModule *consensusTimestamp
	db       blockdb.BlockDatabase
	lock     sync.RWMutex
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	app Application,
	gov Governance,
	db blockdb.BlockDatabase) *Consensus {

	validatorSet := gov.GetValidatorSet()

	// Setup acking by information returned from Governace.
	rb := newReliableBroadcast()
	for vID := range validatorSet {
		rb.addValidator(vID)
	}

	// Setup sequencer by information returned from Governace.
	// TODO(mission): the value of 'K' should be in governace.
	// TODO(mission): the ratio of 'phi' should be in governance.
	to := newTotalOrdering(
		0,
		uint64(2*(len(validatorSet)-1)/3+1),
		uint64(len(validatorSet)))

	return &Consensus{
		rbModule: rb,
		toModule: to,
		ctModule: newConsensusTimestamp(),
		app:      app,
		gov:      gov,
		db:       db,
	}
}

// ProcessBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) ProcessBlock(b *types.Block) (err error) {
	var (
		deliveredBlocks []*types.Block
		earlyDelivered  bool
	)
	// To avoid application layer modify the content of block during
	// processing, we should always operate based on the cloned one.
	b = b.Clone()

	con.lock.Lock()
	defer con.lock.Unlock()
	// Perform reliable broadcast checking.
	if err = con.rbModule.processBlock(b); err != nil {
		return err
	}
	for _, b := range con.rbModule.extractBlocks() {
		// Notify application layer that some block is strongly acked.
		con.app.StronglyAcked(b.Hash)
		// Perform total ordering.
		deliveredBlocks, earlyDelivered, err = con.toModule.processBlock(b)
		if err != nil {
			return
		}
		if len(deliveredBlocks) == 0 {
			continue
		}
		for _, b := range deliveredBlocks {
			if err = con.db.Put(*b); err != nil {
				return
			}
		}
		// TODO(mission): handle membership events here.
		// TODO(mission): return block hash instead of whole block here.
		con.app.TotalOrderingDeliver(deliveredBlocks, earlyDelivered)
		// Perform timestamp generation.
		deliveredBlocks, _, err = con.ctModule.processBlocks(
			deliveredBlocks)
		if err != nil {
			return
		}
		for _, b := range deliveredBlocks {
			if err = con.db.Update(*b); err != nil {
				return
			}
			con.app.DeliverBlock(b.Hash, b.ConsensusInfo.Timestamp)
		}
	}
	return
}

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) PrepareBlock(b *types.Block) (err error) {
	con.lock.RLock()
	defer con.lock.RUnlock()

	con.rbModule.prepareBlock(b)
	b.Timestamps[b.ProposerID] = time.Now().UTC()
	return
}
