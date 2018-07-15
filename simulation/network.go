// copyright 2018 the dexon-consensus-core authors
// this file is part of the dexon-consensus-core library.
//
// the dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the gnu lesser general public license as
// published by the free software foundation, either version 3 of the license,
// or (at your option) any later version.
//
// the dexon-consensus-core library is distributed in the hope that it will be
// useful, but without any warranty; without even the implied warranty of
// merchantability or fitness for a particular purpose. see the gnu lesser
// general public license for more details.
//
// you should have received a copy of the gnu lesser general public license
// along with the dexon-consensus-core library. if not, see
// <http://www.gnu.org/licenses/>.

package simulation

import (
	"math/rand"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

const msgBufferSize = 128

// Network implements the consensus.Network interface.
type Network struct {
	model Model

	endpointMutex sync.RWMutex
	endpoints     map[types.ValidatorID]chan interface{}
}

// NewNetwork returns pointer to a new Network instance.
func NewNetwork(model Model) *Network {
	return &Network{
		model:     model,
		endpoints: make(map[types.ValidatorID]chan interface{}),
	}
}

// Join allow a client to join the network. It reutnrs a interface{} channel for
// the client to recieve information.
func (n *Network) Join(endpoint core.Endpoint) chan interface{} {
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
func (n *Network) Send(destID types.ValidatorID, msg interface{}) {
	n.endpointMutex.RLock()
	defer n.endpointMutex.RUnlock()

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
func (n *Network) BroadcastBlock(block *types.Block) {
	for endpoint := range n.endpoints {
		n.Send(endpoint, block.Clone())
	}
}
