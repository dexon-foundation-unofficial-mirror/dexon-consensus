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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

type compactionChain struct {
	prevBlock *types.Block
	lock      sync.RWMutex
}

func newCompactionChain() *compactionChain {
	return &compactionChain{}
}

func (cc *compactionChain) prepareBlock(
	block *types.Block, prvKey crypto.PrivateKey) (err error) {
	prevBlock := cc.lastBlock()
	if prevBlock != nil {
		block.CompactionChainAck.ConsensusInfoSignature, err =
			signConsensusInfo(prevBlock, prvKey)
		if err != nil {
			return
		}
		block.CompactionChainAck.AckingBlockHash = prevBlock.Hash
	}
	return
}

func (cc *compactionChain) processBlock(block *types.Block) (err error) {
	prevBlock := cc.lastBlock()
	if prevBlock == nil {
		block.ConsensusInfo.Height = 0
		block.ConsensusInfoParentHash = common.Hash{}
	} else {
		block.ConsensusInfo.Height = prevBlock.ConsensusInfo.Height + 1
		block.ConsensusInfoParentHash, err = hashConsensusInfo(prevBlock)
		if err != nil {
			return
		}
	}
	cc.lock.Lock()
	defer cc.lock.Unlock()
	cc.prevBlock = block
	return
}

func (cc *compactionChain) lastBlock() *types.Block {
	cc.lock.RLock()
	defer cc.lock.RUnlock()
	return cc.prevBlock
}
