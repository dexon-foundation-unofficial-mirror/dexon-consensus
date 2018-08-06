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

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type messageType string

const (
	shutdownAck messageType = "shutdownAck"
)

// Message is a struct for peer sending message to server.
type Message struct {
	Type    messageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type infoStatus string

const (
	statusInit     infoStatus = "init"
	statusNormal   infoStatus = "normal"
	statusShutdown infoStatus = "shutdown"
)

// InfoMessage is a struct used by peerServer's /info.
type InfoMessage struct {
	Status infoStatus                   `json:"status"`
	Peers  map[types.ValidatorID]string `json:"peers"`
}

// Endpoint is the interface for a client network endpoint.
type Endpoint interface {
	GetID() types.ValidatorID
}

// Network is the interface for network related functions.
type Network interface {
	PeerServerNetwork
	Start()
	NumPeers() int
	Join(endpoint Endpoint) chan interface{}
	BroadcastBlock(block *types.Block)
}

// PeerServerNetwork is the interface for peerServer network related functions
type PeerServerNetwork interface {
	DeliverBlocks(blocks BlockList)
	NotifyServer(msg Message)
	GetServerInfo() InfoMessage
}
