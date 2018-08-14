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

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// SigToPubFn is a function to recover public key from signature.
type SigToPubFn func(hash common.Hash, signature crypto.Signature) (
	crypto.PublicKey, error)

// ErrMissingBlockInfo would be reported if some information is missing when
// calling PrepareBlock. It implements error interface.
type ErrMissingBlockInfo struct {
	MissingField string
}

func (e *ErrMissingBlockInfo) Error() string {
	return "missing " + e.MissingField + " in block"
}

// Errors for consensus core.
var (
	ErrIncorrectHash        = fmt.Errorf("hash of block is incorrect")
	ErrIncorrectSignature   = fmt.Errorf("signature of block is incorrect")
	ErrGenesisBlockNotEmpty = fmt.Errorf("genesis block should be empty")
)

// Consensus implements DEXON Consensus algorithm.
type Consensus struct {
	app      Application
	gov      Governance
	rbModule *reliableBroadcast
	toModule *totalOrdering
	ctModule *consensusTimestamp
	db       blockdb.BlockDatabase
	prvKey   crypto.PrivateKey
	sigToPub SigToPubFn
	lock     sync.RWMutex
}

// NewConsensus construct an Consensus instance.
func NewConsensus(
	app Application,
	gov Governance,
	db blockdb.BlockDatabase,
	prv crypto.PrivateKey,
	sigToPub SigToPubFn) *Consensus {
	validatorSet := gov.GetValidatorSet()

	// Setup acking by information returned from Governace.
	rb := newReliableBroadcast()
	for vID := range validatorSet {
		rb.addValidator(vID)
	}

	// Setup sequencer by information returned from Governace.
	to := newTotalOrdering(
		uint64(gov.GetTotalOrderingK()),
		uint64(float32(len(validatorSet)-1)*gov.GetPhiRatio()+1),
		uint64(len(validatorSet)))

	return &Consensus{
		rbModule: rb,
		toModule: to,
		ctModule: newConsensusTimestamp(),
		app:      app,
		gov:      gov,
		db:       db,
		prvKey:   prv,
		sigToPub: sigToPub,
	}
}

// sanityCheck checks if the block is a valid block
func (con *Consensus) sanityCheck(blockConv types.BlockConverter) (err error) {
	b := blockConv.Block()
	// Check the hash of block.
	hash, err := hashBlock(blockConv)
	if err != nil || hash != b.Hash {
		return ErrIncorrectHash
	}

	// Check the signer.
	pubKey, err := con.sigToPub(b.Hash, b.Signature)
	if err != nil {
		return err
	}
	if !b.ProposerID.Equal(crypto.Keccak256Hash(pubKey.Bytes())) {
		return ErrIncorrectSignature
	}

	return nil
}

// ProcessBlock is the entry point to submit one block to a Consensus instance.
func (con *Consensus) ProcessBlock(blockConv types.BlockConverter) (err error) {
	if err := con.sanityCheck(blockConv); err != nil {
		return err
	}
	b := blockConv.Block()
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
		hashes := make(common.Hashes, len(deliveredBlocks))
		for idx := range deliveredBlocks {
			hashes[idx] = deliveredBlocks[idx].Hash
		}
		con.app.TotalOrderingDeliver(hashes, earlyDelivered)
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

func (con *Consensus) checkPrepareBlock(
	b *types.Block, proposeTime time.Time) (err error) {
	if (b.ProposerID == types.ValidatorID{}) {
		err = &ErrMissingBlockInfo{MissingField: "ProposerID"}
		return
	}
	return
}

// PrepareBlock would setup header fields of block based on its ProposerID.
func (con *Consensus) PrepareBlock(blockConv types.BlockConverter,
	proposeTime time.Time) (err error) {
	b := blockConv.Block()
	if err = con.checkPrepareBlock(b, proposeTime); err != nil {
		return
	}
	con.lock.RLock()
	defer con.lock.RUnlock()

	con.rbModule.prepareBlock(b)
	b.Timestamps[b.ProposerID] = proposeTime
	b.Hash, err = hashBlock(b)
	if err != nil {
		return
	}
	b.Signature, err = con.prvKey.Sign(b.Hash)
	if err != nil {
		return
	}
	blockConv.SetBlock(b)
	return
}

// PrepareGenesisBlock would setup header fields for genesis block.
func (con *Consensus) PrepareGenesisBlock(blockConv types.BlockConverter,
	proposeTime time.Time) (err error) {
	b := blockConv.Block()
	if err = con.checkPrepareBlock(b, proposeTime); err != nil {
		return
	}
	if len(b.Payloads()) != 0 {
		err = ErrGenesisBlockNotEmpty
		return
	}
	b.Height = 0
	b.ParentHash = common.Hash{}
	b.Acks = make(map[common.Hash]struct{})
	b.Timestamps = make(map[types.ValidatorID]time.Time)
	for vID := range con.gov.GetValidatorSet() {
		b.Timestamps[vID] = time.Time{}
	}
	b.Timestamps[b.ProposerID] = proposeTime
	b.Hash, err = hashBlock(b)
	if err != nil {
		return
	}
	b.Signature, err = con.prvKey.Sign(b.Hash)
	if err != nil {
		return
	}
	blockConv.SetBlock(b)
	return
}
