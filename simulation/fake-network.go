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
	"math/rand"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// FakeNetwork implements the Network interface.
type FakeNetwork struct {
	model Model

	endpointMutex sync.RWMutex
	endpoints     map[types.ValidatorID]chan interface{}
}

// NewFakeNetwork returns pointer to a new Network instance.
func NewFakeNetwork(model Model) *FakeNetwork {
	return &FakeNetwork{
		model:     model,
		endpoints: make(map[types.ValidatorID]chan interface{}),
	}
}

// Start starts the network.
func (n *FakeNetwork) Start() {
}

// NumPeers returns the number of peers in the network.
func (n *FakeNetwork) NumPeers() int {
	n.endpointMutex.Lock()
	defer n.endpointMutex.Unlock()
	return len(n.endpoints)
}

// Join allow a client to join the network. It reutnrs a interface{} channel for
// the client to recieve information.
func (n *FakeNetwork) Join(endpoint Endpoint) chan interface{} {
	n.endpointMutex.Lock()
	defer n.endpointMutex.Unlock()

	if x, exists := n.endpoints[endpoint.GetID()]; exists {
		return x
	}
	recivingChannel := make(chan interface{}, msgBufferSize)

	n.endpoints[endpoint.GetID()] = recivingChannel
	return recivingChannel
}

// Send sends a msg to another client.
func (n *FakeNetwork) Send(destID types.ValidatorID, msg interface{}) {
	clientChannel, exists := n.endpoints[destID]
	if !exists {
		return
	}

	go func() {
		if rand.Float64() > n.model.LossRate() {
			time.Sleep(n.model.Delay())

			clientChannel <- msg
		}
	}()
}

// BroadcastBlock broadcast blocks into the network.
func (n *FakeNetwork) BroadcastBlock(block *types.Block) {
	n.endpointMutex.Lock()
	defer n.endpointMutex.Unlock()

	for endpoint := range n.endpoints {
		n.Send(endpoint, block.Clone())
	}
}

// DeliverBlocks sends blocks to peerServer.
func (n *FakeNetwork) DeliverBlocks(blocks common.Hashes, id int) {
	// TODO(jimmy-dexon): Implement this method.
	return
}

// NotifyServer sends message to peerServer
func (n *FakeNetwork) NotifyServer(msg Message) {
	// TODO(jimmy-dexon): Implement this method.
	return
}

// GetServerInfo retrieve the info message from peerServer.
func (n *FakeNetwork) GetServerInfo() InfoMessage {
	// TODO(jimmy-dexon): Implement this method.
	return InfoMessage{}
}
