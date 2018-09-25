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
	unconfirmedBlocks map[types.NodeID]common.Hashes
	blockByHash       map[common.Hash]*types.Block
	blockByHashMutex  sync.RWMutex
	witnessResultChan chan types.WitnessResult
}

// newSimApp returns point to a new instance of simApp.
func newSimApp(id types.NodeID, netModule *network) *simApp {
	return &simApp{
		NodeID:            id,
		netModule:         netModule,
		DeliverID:         0,
		blockSeen:         make(map[common.Hash]time.Time),
		unconfirmedBlocks: make(map[types.NodeID]common.Hashes),
		blockByHash:       make(map[common.Hash]*types.Block),
		witnessResultChan: make(chan types.WitnessResult),
	}
}

// BlockConfirmed implements core.Application.
func (a *simApp) BlockConfirmed(_ common.Hash) {
}

// VerifyPayload implements core.Application.
func (a *simApp) VerifyPayload(payload []byte) bool {
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
func (a *simApp) PreparePayload(position types.Position) []byte {
	return []byte{}
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
func (a *simApp) BlockDelivered(block types.Block) {
	func() {
		a.blockByHashMutex.Lock()
		defer a.blockByHashMutex.Unlock()

		// TODO(jimmy-dexon) : Remove block in this hash if it's no longer needed.
		a.blockByHash[block.Hash] = &block
	}()

	seenTime, exist := a.blockSeen[block.Hash]
	if !exist {
		return
	}
	now := time.Now()
	payload := []timestampMessage{
		{
			BlockHash: block.Hash,
			Event:     blockSeen,
			Timestamp: seenTime,
		},
		{
			BlockHash: block.Hash,
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

	go func() {
		a.witnessResultChan <- types.WitnessResult{
			BlockHash: block.Hash,
			Data:      []byte(fmt.Sprintf("Block %s", block.Hash)),
		}
	}()
}

// BlockProcessedChan returns a channel to receive the block hashes that have
// finished processing by the application.
func (a *simApp) BlockProcessedChan() <-chan types.WitnessResult {
	return a.witnessResultChan
}

// WitnessAckDelivered is called when a witness ack is created.
func (a *simApp) WitnessAckDelivered(witnessAck *types.WitnessAck) {
}
