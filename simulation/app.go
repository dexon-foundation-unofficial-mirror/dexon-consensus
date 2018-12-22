// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package simulation

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type timestampEvent string

const (
	blockSeen        timestampEvent = "blockSeen"
	timestampConfirm timestampEvent = "timestampConfirm"
	timestampAck     timestampEvent = "timestampAck"
)

// TimestampMessage is a struct for peer sending consensus timestamp information
// to server.
type timestampMessage struct {
	BlockHash common.Hash    `json:"hash"`
	Event     timestampEvent `json:"event"`
	Timestamp time.Time      `json:"timestamp"`
}

const (
	// Block received or created in agreement.
	blockEventReceived int = iota
	// Block confirmed in agreement, sent into lattice
	blockEventConfirmed
	// Block delivered by lattice.
	blockEventDelivered
	// block is ready (Randomness calculated)
	blockEventReady

	blockEventCount
)

// simApp is an DEXON app for simulation.
type simApp struct {
	NodeID          types.NodeID
	Outputs         []*types.Block
	Early           bool
	netModule       *test.Network
	stateModule     *test.State
	DeliverID       int
	blockTimestamps map[common.Hash][]time.Time
	blockSeen       map[common.Hash]time.Time
	// uncofirmBlocks stores the blocks whose timestamps are not ready.
	unconfirmedBlocks  map[types.NodeID]common.Hashes
	blockByHash        map[common.Hash]*types.Block
	blockByHashMutex   sync.RWMutex
	latestWitness      types.Witness
	latestWitnessReady *sync.Cond
	lock               sync.RWMutex
}

// newSimApp returns point to a new instance of simApp.
func newSimApp(
	id types.NodeID, netModule *test.Network, stateModule *test.State) *simApp {
	return &simApp{
		NodeID:             id,
		netModule:          netModule,
		stateModule:        stateModule,
		DeliverID:          0,
		blockSeen:          make(map[common.Hash]time.Time),
		blockTimestamps:    make(map[common.Hash][]time.Time),
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
	a.blockSeen[block.Hash] = time.Now().UTC()
	a.updateBlockEvent(block.Hash)
}

// VerifyBlock implements core.Application.
func (a *simApp) VerifyBlock(block *types.Block) types.BlockVerifyStatus {
	return types.VerifyOK
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
	return a.stateModule.PackRequests()
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

// TotalOrderingDelivered is called when blocks are delivered by the total
// ordering algorithm.
func (a *simApp) TotalOrderingDelivered(
	blockHashes common.Hashes, mode uint32) {
	fmt.Println("OUTPUT", a.NodeID, mode, blockHashes)
	latencies := []time.Duration{}
	func() {
		a.lock.RLock()
		defer a.lock.RUnlock()
		for _, h := range blockHashes {
			latencies = append(latencies, time.Since(a.blockTimestamps[h][blockEventConfirmed]))
		}
	}()
	blockList := &BlockList{
		ID:             a.DeliverID,
		BlockHash:      blockHashes,
		ConfirmLatency: latencies,
	}
	a.netModule.Report(blockList)
	a.DeliverID++
}

// BlockDelivered is called when a block in compaction chain is delivered.
func (a *simApp) BlockDelivered(
	blockHash common.Hash, pos types.Position, result types.FinalizationResult) {

	if len(result.Randomness) == 0 && pos.Round > 0 {
		panic(fmt.Errorf("Block %s randomness is empty", blockHash))
	}
	func() {
		a.blockByHashMutex.Lock()
		defer a.blockByHashMutex.Unlock()
		if block, exist := a.blockByHash[blockHash]; exist {
			if err := a.stateModule.Apply(block.Payload); err != nil {
				if err != test.ErrDuplicatedChange {
					panic(err)
				}
			}
		} else {
			panic(fmt.Errorf("Block is not confirmed yet: %s", blockHash))
		}
	}()
	func() {
		a.latestWitnessReady.L.Lock()
		defer a.latestWitnessReady.L.Unlock()
		a.latestWitness = types.Witness{
			Height: result.Height,
		}
		a.latestWitnessReady.Broadcast()
	}()

	a.updateBlockEvent(blockHash)

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
	a.netModule.Report(msg)
}

// BlockReceived is called when a block is received in agreement.
func (a *simApp) BlockReceived(hash common.Hash) {
	a.updateBlockEvent(hash)
}

// BlockReady is called when a block is ready.
func (a *simApp) BlockReady(hash common.Hash) {
	a.updateBlockEvent(hash)
}

func (a *simApp) updateBlockEvent(hash common.Hash) {
	a.lock.Lock()
	defer a.lock.Unlock()
	a.blockTimestamps[hash] = append(a.blockTimestamps[hash], time.Now().UTC())
	if len(a.blockTimestamps[hash]) == blockEventCount {
		msg := &test.BlockEventMessage{
			BlockHash:  hash,
			Timestamps: a.blockTimestamps[hash],
		}
		if err := a.netModule.Report(msg); err != nil {
			panic(err)
		}
		delete(a.blockTimestamps, hash)
	}
}
