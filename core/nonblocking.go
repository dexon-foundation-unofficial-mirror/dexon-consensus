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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type blockConfirmedEvent struct {
	blockHash common.Hash
}

type stronglyAckedEvent struct {
	blockHash common.Hash
}

type totalOrderingDeliverEvent struct {
	blockHashes common.Hashes
	early       bool
}

type blockDeliverEvent struct {
	block *types.Block
}

type witnessAckEvent struct {
	witnessAck *types.WitnessAck
}

// nonBlocking implements these interfaces and is a decorator for
// them that makes the methods to be non-blocking.
//  - Application
//  - Debug
//  - It also provides nonblockig for blockdb update.
type nonBlocking struct {
	app          Application
	debug        Debug
	eventChan    chan interface{}
	events       []interface{}
	eventsChange *sync.Cond
	running      sync.WaitGroup
}

func newNonBlocking(app Application, debug Debug) *nonBlocking {
	nonBlockingModule := &nonBlocking{
		app:          app,
		debug:        debug,
		eventChan:    make(chan interface{}, 6),
		events:       make([]interface{}, 0, 100),
		eventsChange: sync.NewCond(&sync.Mutex{}),
	}
	go nonBlockingModule.run()
	return nonBlockingModule
}

func (nb *nonBlocking) addEvent(event interface{}) {
	nb.eventsChange.L.Lock()
	defer nb.eventsChange.L.Unlock()
	nb.events = append(nb.events, event)
	nb.eventsChange.Broadcast()
}

func (nb *nonBlocking) run() {
	// This go routine consume the first event from events and call the
	// corresponding methods of Application/Debug/blockdb.
	for {
		var event interface{}
		func() {
			nb.eventsChange.L.Lock()
			defer nb.eventsChange.L.Unlock()
			for len(nb.events) == 0 {
				nb.eventsChange.Wait()
			}
			event = nb.events[0]
			nb.events = nb.events[1:]
			nb.running.Add(1)
		}()
		switch e := event.(type) {
		case stronglyAckedEvent:
			nb.debug.StronglyAcked(e.blockHash)
		case blockConfirmedEvent:
			nb.debug.BlockConfirmed(e.blockHash)
		case totalOrderingDeliverEvent:
			nb.debug.TotalOrderingDeliver(e.blockHashes, e.early)
		case blockDeliverEvent:
			nb.app.BlockDeliver(*e.block)
		case witnessAckEvent:
			nb.app.WitnessAckDeliver(e.witnessAck)
		default:
			fmt.Printf("Unknown event %v.", e)
		}
		nb.running.Done()
		nb.eventsChange.Broadcast()
	}
}

// wait will wait for all event in events finishes.
func (nb *nonBlocking) wait() {
	nb.eventsChange.L.Lock()
	defer nb.eventsChange.L.Unlock()
	for len(nb.events) > 0 {
		nb.eventsChange.Wait()
	}
	nb.running.Wait()
}

// PreparePayload cannot be non-blocking.
func (nb *nonBlocking) PreparePayload(
	position types.Position) []byte {
	return nb.app.PreparePayload(position)
}

// VerifyPayloads cannot be non-blocking.
func (nb *nonBlocking) VerifyPayloads(payloads []byte) bool {
	return nb.app.VerifyPayloads(payloads)
}

// BlockConfirmed is called when a block is confirmed and added to lattice.
func (nb *nonBlocking) BlockConfirmed(blockHash common.Hash) {
	if nb.debug != nil {
		nb.addEvent(blockConfirmedEvent{blockHash})
	}
}

// StronglyAcked is called when a block is strongly acked.
func (nb *nonBlocking) StronglyAcked(blockHash common.Hash) {
	if nb.debug != nil {
		nb.addEvent(stronglyAckedEvent{blockHash})
	}
}

// TotalOrderingDeliver is called when the total ordering algorithm deliver
// a set of block.
func (nb *nonBlocking) TotalOrderingDeliver(
	blockHashes common.Hashes, early bool) {
	if nb.debug != nil {
		nb.addEvent(totalOrderingDeliverEvent{blockHashes, early})
	}
}

// BlockDeliver is called when a block is add to the compaction chain.
func (nb *nonBlocking) BlockDeliver(block types.Block) {
	nb.addEvent(blockDeliverEvent{&block})
}

// BlockProcessedChan returns a channel to receive the block hashes that have
// finished processing by the application.
func (nb *nonBlocking) BlockProcessedChan() <-chan types.WitnessResult {
	return nb.app.BlockProcessedChan()
}

// WitnessAckDeliver is called when a witness ack is created.
func (nb *nonBlocking) WitnessAckDeliver(witnessAck *types.WitnessAck) {
	nb.addEvent(witnessAckEvent{witnessAck})
}
