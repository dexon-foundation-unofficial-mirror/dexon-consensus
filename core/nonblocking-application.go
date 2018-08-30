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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type stronglyAckedEvent struct {
	blockHash common.Hash
}

type totalOrderingDeliverEvent struct {
	blockHashes common.Hashes
	early       bool
}

type deliverBlockEvent struct {
	blockHash common.Hash
	timestamp time.Time
}

type notaryAckEvent struct {
	notaryAck *types.NotaryAck
}

// nonBlockingApplication implements Application and is a decorator for
// Application that makes the methods to be non-blocking.
type nonBlockingApplication struct {
	app          Application
	eventChan    chan interface{}
	events       []interface{}
	eventsChange *sync.Cond
	running      sync.WaitGroup
}

func newNonBlockingApplication(app Application) *nonBlockingApplication {
	nonBlockingApp := &nonBlockingApplication{
		app:          app,
		eventChan:    make(chan interface{}, 6),
		events:       make([]interface{}, 0, 100),
		eventsChange: sync.NewCond(&sync.Mutex{}),
	}
	go nonBlockingApp.run()
	return nonBlockingApp
}

func (app *nonBlockingApplication) addEvent(event interface{}) {
	app.eventsChange.L.Lock()
	defer app.eventsChange.L.Unlock()
	app.events = append(app.events, event)
	app.eventsChange.Broadcast()
}

func (app *nonBlockingApplication) run() {
	// This go routine consume the first event from events and call the
	// corresponding method of app.
	for {
		var event interface{}
		func() {
			app.eventsChange.L.Lock()
			defer app.eventsChange.L.Unlock()
			for len(app.events) == 0 {
				app.eventsChange.Wait()
			}
			event = app.events[0]
			app.events = app.events[1:]
			app.running.Add(1)
		}()
		switch e := event.(type) {
		case stronglyAckedEvent:
			app.app.StronglyAcked(e.blockHash)
		case totalOrderingDeliverEvent:
			app.app.TotalOrderingDeliver(e.blockHashes, e.early)
		case deliverBlockEvent:
			app.app.DeliverBlock(e.blockHash, e.timestamp)
		case notaryAckEvent:
			app.app.NotaryAckDeliver(e.notaryAck)
		default:
			fmt.Printf("Unknown event %v.", e)
		}
		app.running.Done()
		app.eventsChange.Broadcast()
	}
}

// wait will wait for all event in events finishes.
func (app *nonBlockingApplication) wait() {
	app.eventsChange.L.Lock()
	defer app.eventsChange.L.Unlock()
	for len(app.events) > 0 {
		app.eventsChange.Wait()
	}
	app.running.Wait()
}

// PreparePayloads cannot be non-blocking.
func (app *nonBlockingApplication) PreparePayloads(
	shardID, chainID, height uint64) [][]byte {
	return app.app.PreparePayloads(shardID, chainID, height)
}

// StronglyAcked is called when a block is strongly acked.
func (app *nonBlockingApplication) StronglyAcked(blockHash common.Hash) {
	app.addEvent(stronglyAckedEvent{blockHash})
}

// TotalOrderingDeliver is called when the total ordering algorithm deliver
// a set of block.
func (app *nonBlockingApplication) TotalOrderingDeliver(
	blockHashes common.Hashes, early bool) {
	app.addEvent(totalOrderingDeliverEvent{blockHashes, early})
}

// DeliverBlock is called when a block is add to the compaction chain.
func (app *nonBlockingApplication) DeliverBlock(
	blockHash common.Hash, timestamp time.Time) {
	app.addEvent(deliverBlockEvent{blockHash, timestamp})
}

// NotaryAckDeliver is called when a notary ack is created.
func (app *nonBlockingApplication) NotaryAckDeliver(notaryAck *types.NotaryAck) {
	app.addEvent(notaryAckEvent{notaryAck})
}
