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

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// Errors for compaction chain.
var (
	ErrIncorrectNotaryAck = fmt.Errorf(
		"compaction chain notary of block is incorrect")
)

type compactionChain struct {
	prevBlock        *types.Block
	lock             sync.RWMutex
	latestNotaryAcks map[types.ValidatorID]types.NotaryAck
}

func newCompactionChain() *compactionChain {
	return &compactionChain{}
}

func (cc *compactionChain) sanityCheck(notaryAck types.NotaryAck) bool {
	return true
}

func (cc *compactionChain) processBlock(block *types.Block) (err error) {
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.prevBlock = block
	/*
		prevBlock := cc.lastBlock()
		if prevBlock != nil {
			block.NotaryAck.NotarySignature, err =
				signNotary(prevBlock, prvKey)
			if err != nil {
				return
			}
			block.NotaryAck.NotaryBlockHash = prevBlock.Hash
		}
	*/
	return
}
func (cc *compactionChain) prepareNotaryAck(prvKey crypto.PrivateKey) (
	notaryAck types.NotaryAck, err error) {
	notaryAck.NotaryBlockHash = cc.lastBlock().Hash
	return
}

func (cc *compactionChain) processNotaryAck(notaryAck types.NotaryAck) (
	err error) {
	if !cc.sanityCheck(notaryAck) {
		err = ErrIncorrectNotaryAck
		return
	}
	/*
		prevBlock := cc.lastBlock()
		if prevBlock == nil {
			block.Notary.Height = 0
			block.NotaryParentHash = common.Hash{}
		} else {
			block.Notary.Height = prevBlock.Notary.Height + 1
			block.NotaryParentHash, err = hashNotary(prevBlock)
			if err != nil {
				return
			}
		}
		cc.lock.Lock()
		defer cc.lock.Unlock()
		cc.prevBlock = block
	*/
	cc.latestNotaryAcks[notaryAck.ProposerID] = notaryAck
	return
}

func (cc *compactionChain) notaryAcks() map[types.ValidatorID]types.NotaryAck {
	return cc.latestNotaryAcks
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	return cc.prevBlock
}
