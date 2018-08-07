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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// SimApp is an DEXON app for simulation.
type SimApp struct {
	ValidatorID types.ValidatorID
	Outputs     []*types.Block
	Early       bool
	Network     PeerServerNetwork
	DeliverID   int
	// blockSeen stores the time when block is delivered by Total Ordering.
	blockSeen map[common.Hash]time.Time
	// uncofirmBlocks stores the blocks whose timestamps are not ready.
	unconfirmedBlocks map[types.ValidatorID]common.Hashes
	blockHash         map[common.Hash]*types.Block
}

// NewSimApp returns point to a new instance of SimApp.
func NewSimApp(id types.ValidatorID, Network PeerServerNetwork) *SimApp {
	return &SimApp{
		ValidatorID:       id,
		Network:           Network,
		DeliverID:         0,
		blockSeen:         make(map[common.Hash]time.Time),
		unconfirmedBlocks: make(map[types.ValidatorID]common.Hashes),
		blockHash:         make(map[common.Hash]*types.Block),
	}
}

// getAckedBlocks will return all unconfirmed blocks' hash with lower Height
// than the block with ackHash.
func (a *SimApp) getAckedBlocks(ackHash common.Hash) (output common.Hashes) {
	// TODO(jimmy-dexon): Why there are some acks never seen?
	ackBlock, exist := a.blockHash[ackHash]
	if !exist {
		return
	}
	hashes, exist := a.unconfirmedBlocks[ackBlock.ProposerID]
	if !exist {
		return
	}
	for i, blockHash := range hashes {
		if a.blockHash[blockHash].Height > ackBlock.Height {
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

// TotalOrderingDeliver is called when blocks are delivered by the total
// ordering algorithm.
func (a *SimApp) TotalOrderingDeliver(blocks []*types.Block, early bool) {
	now := time.Now()
	a.Outputs = blocks
	a.Early = early
	fmt.Println("OUTPUT", a.ValidatorID, a.Early, a.Outputs)

	blockHash := common.Hashes{}
	confirmLatency := []time.Duration{}

	payload := []TimestampMessage{}
	for _, block := range blocks {
		blockHash = append(blockHash, block.Hash)
		if block.ProposerID == a.ValidatorID {
			confirmLatency = append(confirmLatency,
				now.Sub(block.Timestamps[a.ValidatorID]))
		}
		// TODO(jimmy-dexon) : Remove block in this hash if it's no longer needed.
		a.blockHash[block.Hash] = block
		for hash := range block.Acks {
			for _, blockHash := range a.getAckedBlocks(hash) {
				payload = append(payload, TimestampMessage{
					BlockHash: blockHash,
					Event:     timestampAck,
					Timestamp: now,
				})
				delete(a.blockSeen, block.Hash)
			}
		}
	}
	if len(payload) > 0 {
		jsonPayload, err := json.Marshal(payload)
		if err != nil {
			fmt.Println(err)
		} else {
			msg := Message{
				Type:    blockTimestamp,
				Payload: jsonPayload,
			}
			a.Network.NotifyServer(msg)
		}
	}

	blockList := BlockList{
		ID:             a.DeliverID,
		BlockHash:      blockHash,
		ConfirmLatency: confirmLatency,
	}
	a.Network.DeliverBlocks(blockList)
	a.DeliverID++
	for _, block := range blocks {
		a.blockSeen[block.Hash] = now
		a.unconfirmedBlocks[block.ProposerID] = append(
			a.unconfirmedBlocks[block.ProposerID], block.Hash)
	}
}

// DeliverBlock is called when a block in compaction chain is delivered.
func (a *SimApp) DeliverBlock(blockHash common.Hash, timestamp time.Time) {
	seenTime, exist := a.blockSeen[blockHash]
	if !exist {
		return
	}
	now := time.Now()
	payload := []TimestampMessage{
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
	msg := Message{
		Type:    blockTimestamp,
		Payload: jsonPayload,
	}
	a.Network.NotifyServer(msg)
}
