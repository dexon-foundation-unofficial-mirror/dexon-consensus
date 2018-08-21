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

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// Errors for compaction chain.
var (
	ErrNoNotaryToAck = fmt.Errorf(
		"no notary to ack")
	ErrIncorrectNotaryHash = fmt.Errorf(
		"hash of notary ack is incorrect")
	ErrIncorrectNotarySignature = fmt.Errorf(
		"signature of notary ack is incorrect")
)

type pendingAck struct {
	receivedTime time.Time
	notaryAck    *types.NotaryAck
}

type compactionChain struct {
	db               blockdb.Reader
	pendingAckLock   sync.RWMutex
	pendingAck       map[common.Hash]*pendingAck
	prevBlockLock    sync.RWMutex
	prevBlock        *types.Block
	notaryAcksLock   sync.RWMutex
	latestNotaryAcks map[types.ValidatorID]*types.NotaryAck
	sigToPub         SigToPubFn
}

func newCompactionChain(
	db blockdb.Reader,
	sigToPub SigToPubFn,
) *compactionChain {
	return &compactionChain{
		db:               db,
		pendingAck:       make(map[common.Hash]*pendingAck),
		latestNotaryAcks: make(map[types.ValidatorID]*types.NotaryAck),
		sigToPub:         sigToPub,
	}
}

func (cc *compactionChain) sanityCheck(
	notaryAck *types.NotaryAck, notaryBlock *types.Block) error {
	if notaryBlock != nil {
		hash, err := hashNotary(notaryBlock)
		if err != nil {
			return err
		}
		if hash != notaryAck.Hash {
			return ErrIncorrectNotaryHash
		}
	}
	pubKey, err := cc.sigToPub(notaryAck.Hash, notaryAck.Signature)
	if err != nil {
		return err
	}
	if notaryAck.ProposerID != types.NewValidatorID(pubKey) {
		return ErrIncorrectNotarySignature
	}
	return nil
}

// TODO(jimmy-dexon): processBlock and prepareNotaryAck can be extraced to
// another struct.
func (cc *compactionChain) processBlock(block *types.Block) error {
	prevBlock := cc.lastBlock()
	if prevBlock != nil {
		hash, err := hashNotary(prevBlock)
		if err != nil {
			return err
		}
		block.Notary.Height = prevBlock.Notary.Height + 1
		block.Notary.ParentHash = hash
	}
	cc.prevBlockLock.Lock()
	defer cc.prevBlockLock.Unlock()
	cc.prevBlock = block
	return nil
}

func (cc *compactionChain) prepareNotaryAck(prvKey crypto.PrivateKey) (
	notaryAck *types.NotaryAck, err error) {
	lastBlock := cc.lastBlock()
	if lastBlock == nil {
		err = ErrNoNotaryToAck
		return
	}
	hash, err := hashNotary(lastBlock)
	if err != nil {
		return
	}
	sig, err := prvKey.Sign(hash)
	if err != nil {
		return
	}
	notaryAck = &types.NotaryAck{
		ProposerID:      types.NewValidatorID(prvKey.PublicKey()),
		NotaryBlockHash: lastBlock.Hash,
		Signature:       sig,
		Hash:            hash,
	}
	return
}

func (cc *compactionChain) processNotaryAck(notaryAck *types.NotaryAck) (
	err error) {
	// Before getting the Block from notaryAck.NotaryBlockHash, we can still
	// do some sanityCheck to prevent invalid ack appending to pendingAck.
	if err = cc.sanityCheck(notaryAck, nil); err != nil {
		return
	}
	pendingFinished := make(chan struct{})
	go func() {
		cc.processPendingNotaryAcks()
		pendingFinished <- struct{}{}
	}()
	defer func() {
		<-pendingFinished
	}()
	notaryBlock, err := cc.db.Get(notaryAck.NotaryBlockHash)
	if err != nil {
		if err == blockdb.ErrBlockDoesNotExist {
			cc.pendingAckLock.Lock()
			defer cc.pendingAckLock.Unlock()
			cc.pendingAck[notaryAck.Hash] = &pendingAck{
				receivedTime: time.Now().UTC(),
				notaryAck:    notaryAck,
			}
			err = nil
		}
		return
	}
	return cc.processOneNotaryAck(notaryAck, &notaryBlock)
}

func (cc *compactionChain) processOneNotaryAck(
	notaryAck *types.NotaryAck, notaryBlock *types.Block) (
	err error) {
	if err = cc.sanityCheck(notaryAck, notaryBlock); err != nil {
		return
	}
	lastNotaryAck, exist := func() (ack *types.NotaryAck, exist bool) {
		cc.notaryAcksLock.RLock()
		defer cc.notaryAcksLock.RUnlock()
		ack, exist = cc.latestNotaryAcks[notaryAck.ProposerID]
		return
	}()
	if exist {
		lastNotaryBlock, err2 := cc.db.Get(lastNotaryAck.NotaryBlockHash)
		err = err2
		if err != nil {
			return
		}
		if lastNotaryBlock.Notary.Height > notaryBlock.Notary.Height {
			return
		}
	}
	cc.notaryAcksLock.Lock()
	defer cc.notaryAcksLock.Unlock()
	cc.latestNotaryAcks[notaryAck.ProposerID] = notaryAck
	return
}

func (cc *compactionChain) processPendingNotaryAcks() {
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
		notaryBlock, err := cc.db.Get(ack.notaryAck.NotaryBlockHash)
		if err != nil {
			if err == blockdb.ErrBlockDoesNotExist {
				continue
			}
			// TODO(jimmy-dexon): this error needs to be handled properly.
			fmt.Println(err)
			delete(pendingAck, hash)
		}
		delete(pendingAck, hash)
		cc.processOneNotaryAck(ack.notaryAck, &notaryBlock)
	}

	cc.pendingAckLock.Lock()
	defer cc.pendingAckLock.Unlock()
	for k, v := range cc.pendingAck {
		pendingAck[k] = v
	}
	cc.pendingAck = pendingAck
}

func (cc *compactionChain) notaryAcks() map[types.ValidatorID]*types.NotaryAck {
	cc.notaryAcksLock.RLock()
	defer cc.notaryAcksLock.RUnlock()
	acks := make(map[types.ValidatorID]*types.NotaryAck)
	for k, v := range cc.latestNotaryAcks {
		acks[k] = v.Clone()
	}
	return acks
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.prevBlockLock.RLock()
	defer cc.prevBlockLock.RUnlock()
	return cc.prevBlock
}
