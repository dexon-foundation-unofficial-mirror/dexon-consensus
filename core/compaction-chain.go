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

// TODO(jimmy-dexon): remove those comments before open source.

package core

import (
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// Errors for compaction chain.
var (
	ErrNoWitnessToAck = fmt.Errorf(
		"no witness to ack")
	ErrIncorrectWitnessHash = fmt.Errorf(
		"hash of witness ack is incorrect")
	ErrIncorrectWitnessSignature = fmt.Errorf(
		"signature of witness ack is incorrect")
)

type pendingAck struct {
	receivedTime time.Time
	witnessAck   *types.WitnessAck
}

type compactionChain struct {
	db                blockdb.Reader
	pendingAckLock    sync.RWMutex
	pendingAck        map[common.Hash]*pendingAck
	prevBlockLock     sync.RWMutex
	prevBlock         *types.Block
	witnessAcksLock   sync.RWMutex
	latestWitnessAcks map[types.ValidatorID]*types.WitnessAck
	sigToPub          SigToPubFn
}

func newCompactionChain(
	db blockdb.Reader,
	sigToPub SigToPubFn,
) *compactionChain {
	return &compactionChain{
		db:                db,
		pendingAck:        make(map[common.Hash]*pendingAck),
		latestWitnessAcks: make(map[types.ValidatorID]*types.WitnessAck),
		sigToPub:          sigToPub,
	}
}

func (cc *compactionChain) sanityCheck(
	witnessAck *types.WitnessAck, witnessBlock *types.Block) error {
	if witnessBlock != nil {
		hash, err := hashWitness(witnessBlock)
		if err != nil {
			return err
		}
		if hash != witnessAck.Hash {
			return ErrIncorrectWitnessHash
		}
	}
	pubKey, err := cc.sigToPub(witnessAck.Hash, witnessAck.Signature)
	if err != nil {
		return err
	}
	if witnessAck.ProposerID != types.NewValidatorID(pubKey) {
		return ErrIncorrectWitnessSignature
	}
	return nil
}

// TODO(jimmy-dexon): processBlock and prepareWitnessAck can be extraced to
// another struct.
func (cc *compactionChain) processBlock(block *types.Block) error {
	prevBlock := cc.lastBlock()
	if prevBlock != nil {
		hash, err := hashWitness(prevBlock)
		if err != nil {
			return err
		}
		block.Witness.Height = prevBlock.Witness.Height + 1
		block.Witness.ParentHash = hash
	}
	cc.prevBlockLock.Lock()
	defer cc.prevBlockLock.Unlock()
	cc.prevBlock = block
	return nil
}

func (cc *compactionChain) prepareWitnessAck(prvKey crypto.PrivateKey) (
	witnessAck *types.WitnessAck, err error) {
	lastBlock := cc.lastBlock()
	if lastBlock == nil {
		err = ErrNoWitnessToAck
		return
	}
	hash, err := hashWitness(lastBlock)
	if err != nil {
		return
	}
	sig, err := prvKey.Sign(hash)
	if err != nil {
		return
	}
	witnessAck = &types.WitnessAck{
		ProposerID:       types.NewValidatorID(prvKey.PublicKey()),
		WitnessBlockHash: lastBlock.Hash,
		Signature:        sig,
		Hash:             hash,
	}
	return
}

func (cc *compactionChain) processWitnessAck(witnessAck *types.WitnessAck) (
	err error) {
	// Before getting the Block from witnessAck.WitnessBlockHash, we can still
	// do some sanityCheck to prevent invalid ack appending to pendingAck.
	if err = cc.sanityCheck(witnessAck, nil); err != nil {
		return
	}
	pendingFinished := make(chan struct{})
	go func() {
		cc.processPendingWitnessAcks()
		pendingFinished <- struct{}{}
	}()
	defer func() {
		<-pendingFinished
	}()
	witnessBlock, err := cc.db.Get(witnessAck.WitnessBlockHash)
	if err != nil {
		if err == blockdb.ErrBlockDoesNotExist {
			cc.pendingAckLock.Lock()
			defer cc.pendingAckLock.Unlock()
			cc.pendingAck[witnessAck.Hash] = &pendingAck{
				receivedTime: time.Now().UTC(),
				witnessAck:   witnessAck,
			}
			err = nil
		}
		return
	}
	return cc.processOneWitnessAck(witnessAck, &witnessBlock)
}

func (cc *compactionChain) processOneWitnessAck(
	witnessAck *types.WitnessAck, witnessBlock *types.Block) (
	err error) {
	if err = cc.sanityCheck(witnessAck, witnessBlock); err != nil {
		return
	}
	lastWitnessAck, exist := func() (ack *types.WitnessAck, exist bool) {
		cc.witnessAcksLock.RLock()
		defer cc.witnessAcksLock.RUnlock()
		ack, exist = cc.latestWitnessAcks[witnessAck.ProposerID]
		return
	}()
	if exist {
		lastWitnessBlock, err2 := cc.db.Get(lastWitnessAck.WitnessBlockHash)
		err = err2
		if err != nil {
			return
		}
		if lastWitnessBlock.Witness.Height > witnessBlock.Witness.Height {
			return
		}
	}
	cc.witnessAcksLock.Lock()
	defer cc.witnessAcksLock.Unlock()
	cc.latestWitnessAcks[witnessAck.ProposerID] = witnessAck
	return
}

func (cc *compactionChain) processPendingWitnessAcks() {
	pendingAck := func() map[common.Hash]*pendingAck {
		pendingAck := make(map[common.Hash]*pendingAck)
		cc.pendingAckLock.RLock()
		defer cc.pendingAckLock.RUnlock()
		for k, v := range cc.pendingAck {
			pendingAck[k] = v
		}
		return pendingAck
	}()

	for hash, ack := range pendingAck {
		// TODO(jimmy-dexon): customizable timeout.
		if ack.receivedTime.Add(30 * time.Second).Before(time.Now().UTC()) {
			delete(pendingAck, hash)
			continue
		}
	}
	for hash, ack := range pendingAck {
		witnessBlock, err := cc.db.Get(ack.witnessAck.WitnessBlockHash)
		if err != nil {
			if err == blockdb.ErrBlockDoesNotExist {
				continue
			}
			// TODO(jimmy-dexon): this error needs to be handled properly.
			fmt.Println(err)
			delete(pendingAck, hash)
		}
		delete(pendingAck, hash)
		cc.processOneWitnessAck(ack.witnessAck, &witnessBlock)
	}

	cc.pendingAckLock.Lock()
	defer cc.pendingAckLock.Unlock()
	for k, v := range cc.pendingAck {
		pendingAck[k] = v
	}
	cc.pendingAck = pendingAck
}

func (cc *compactionChain) witnessAcks() map[types.ValidatorID]*types.WitnessAck {
	cc.witnessAcksLock.RLock()
	defer cc.witnessAcksLock.RUnlock()
	acks := make(map[types.ValidatorID]*types.WitnessAck)
	for k, v := range cc.latestWitnessAcks {
		acks[k] = v.Clone()
	}
	return acks
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.prevBlockLock.RLock()
	defer cc.prevBlockLock.RUnlock()
	return cc.prevBlock
}
