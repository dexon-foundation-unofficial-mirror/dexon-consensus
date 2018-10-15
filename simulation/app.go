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

package simulation

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// simApp is an DEXON app for simulation.
type simApp struct {
	NodeID    types.NodeID
	Outputs   []*types.Block
	Early     bool
	netModule *network
	DeliverID int
	// blockSeen stores the time when block is delivered by Total Ordering.
	blockSeen map[common.Hash]time.Time
	// uncofirmBlocks stores the blocks whose timestamps are not ready.
	unconfirmedBlocks  map[types.NodeID]common.Hashes
	blockByHash        map[common.Hash]*types.Block
	blockByHashMutex   sync.RWMutex
	latestWitness      types.Witness
	latestWitnessReady *sync.Cond
}

// newSimApp returns point to a new instance of simApp.
func newSimApp(id types.NodeID, netModule *network) *simApp {
	return &simApp{
		NodeID:             id,
		netModule:          netModule,
		DeliverID:          0,
		blockSeen:          make(map[common.Hash]time.Time),
		unconfirmedBlocks:  make(map[types.NodeID]common.Hashes),
		blockByHash:        make(map[common.Hash]*types.Block),
		latestWitnessReady: sync.NewCond(&sync.Mutex{}),
	}
}

// BlockConfirmed implements core.Application.
func (a *simApp) BlockConfirmed(block types.Block) {
	a.blockByHashMutex.Lock()
	defer a.blockByHashMutex.Unlock()

	// TODO(jimmy-dexon) : Remove block in this hash if it's no longer needed.
	a.blockByHash[block.Hash] = &block
}

// VerifyBlock implements core.Application.
func (a *simApp) VerifyBlock(block *types.Block) bool {
	return true
}

// getAckedBlocks will return all unconfirmed blocks' hash with lower Height
// than the block with ackHash.
func (a *simApp) getAckedBlocks(ackHash common.Hash) (output common.Hashes) {
	// TODO(jimmy-dexon): Why there are some acks never seen?
	ackBlock, exist := a.blockByHash[ackHash]
	if !exist {
		return
	}
	hashes, exist := a.unconfirmedBlocks[ackBlock.ProposerID]
	if !exist {
		return
	}
	for i, blockHash := range hashes {
		if a.blockByHash[blockHash].Position.Height > ackBlock.Position.Height {
			output, a.unconfirmedBlocks[ackBlock.ProposerID] = hashes[:i], hashes[i:]
			break
		}
	}

	// All of the Height of unconfirmed blocks are lower than the acked block.
	if len(output) == 0 {
		output, a.unconfirmedBlocks[ackBlock.ProposerID] = hashes, common.Hashes{}
	}
	return
}

// PreparePayload implements core.Application.
func (a *simApp) PreparePayload(position types.Position) ([]byte, error) {
	return []byte{}, nil
}

// PrepareWitness implements core.Application.
func (a *simApp) PrepareWitness(height uint64) (types.Witness, error) {
	a.latestWitnessReady.L.Lock()
	defer a.latestWitnessReady.L.Unlock()
	for a.latestWitness.Height < height {
		a.latestWitnessReady.Wait()
	}
	return a.latestWitness, nil
}

// StronglyAcked is called when a block is strongly acked by DEXON
// Reliabe Broadcast algorithm.
func (a *simApp) StronglyAcked(blockHash common.Hash) {
}

// TotalOrderingDelivered is called when blocks are delivered by the total
// ordering algorithm.
func (a *simApp) TotalOrderingDelivered(blockHashes common.Hashes, early bool) {
	fmt.Println("OUTPUT", a.NodeID, early, blockHashes)
	blockList := &BlockList{
		ID:        a.DeliverID,
		BlockHash: blockHashes,
	}
	a.netModule.report(blockList)
	a.DeliverID++
}

// BlockDelivered is called when a block in compaction chain is delivered.
func (a *simApp) BlockDelivered(
	blockHash common.Hash, result types.FinalizationResult) {
	if len(result.Randomness) == 0 && func() bool {
		if block, exist := a.blockByHash[blockHash]; exist {
			if block.Position.Round == 0 {
				return false
			}
		}
		return true
	}() {
		panic(fmt.Errorf("Block %s randomness is empty", blockHash))
	}
	func() {
		a.latestWitnessReady.L.Lock()
		defer a.latestWitnessReady.L.Unlock()
		a.latestWitness = types.Witness{
			Timestamp: result.Timestamp,
			Height:    result.Height,
		}
		a.latestWitnessReady.Broadcast()
	}()

	seenTime, exist := a.blockSeen[blockHash]
	if !exist {
		return
	}
	now := time.Now()
	payload := []timestampMessage{
		{
			BlockHash: blockHash,
			Event:     blockSeen,
			Timestamp: seenTime,
		},
		{
			BlockHash: blockHash,
			Event:     timestampConfirm,
			Timestamp: now,
		},
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		fmt.Println(err)
		return
	}
	msg := &message{
		Type:    blockTimestamp,
		Payload: jsonPayload,
	}
	a.netModule.report(msg)
}
