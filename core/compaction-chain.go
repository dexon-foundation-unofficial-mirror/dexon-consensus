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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type pendingAck struct {
	receivedTime time.Time
}

type compactionChain struct {
	db              blockdb.Reader
	pendingAckLock  sync.RWMutex
	pendingAck      map[common.Hash]*pendingAck
	prevBlockLock   sync.RWMutex
	prevBlock       *types.Block
	witnessAcksLock sync.RWMutex
}

func newCompactionChain(
	db blockdb.Reader,
) *compactionChain {
	return &compactionChain{
		db:         db,
		pendingAck: make(map[common.Hash]*pendingAck),
	}
}

func (cc *compactionChain) sanityCheck(witnessBlock *types.Block) error {
	return nil
}

// TODO(jimmy-dexon): processBlock can be extraced to
// another struct.
func (cc *compactionChain) processBlock(block *types.Block) error {
	prevBlock := cc.lastBlock()
	if prevBlock != nil {
		block.Witness.Height = prevBlock.Witness.Height + 1
	}
	cc.prevBlockLock.Lock()
	defer cc.prevBlockLock.Unlock()
	cc.prevBlock = block
	return nil
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.prevBlockLock.RLock()
	defer cc.prevBlockLock.RUnlock()
	return cc.prevBlock
}
