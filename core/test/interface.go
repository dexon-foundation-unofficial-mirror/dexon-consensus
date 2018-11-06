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
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package test

import (
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// Revealer defines the interface to reveal a group
// of pre-generated blocks.
type Revealer interface {
	blockdb.BlockIterator

	// Reset the revealing.
	Reset()
}

// Stopper defines an interface for Scheduler to tell when to stop execution.
type Stopper interface {
	// ShouldStop is provided with the ID of the handler just finishes an event.
	// It's thread-safe to access internal/shared state of the handler at this
	// moment.
	// The Stopper should check state of that handler and return 'true'
	// if the execution could be stopped.
	ShouldStop(nID types.NodeID) bool
}

// EventHandler defines an interface to handle a Scheduler event.
type EventHandler interface {
	// Handle the event belongs to this handler, and return derivated events.
	Handle(*Event) []*Event
}

// TransportPeerType defines the type of peer, either 'peer' or 'server'.
type TransportPeerType string

const (
	// TransportPeerServer is the type of peer server.
	TransportPeerServer TransportPeerType = "server"
	// TransportPeer is the type of peer.
	TransportPeer TransportPeerType = "peer"
)

// TransportEnvelope define the payload format of a message when transporting.
type TransportEnvelope struct {
	// PeerType defines the type of source peer, could be either "peer" or
	// "server".
	PeerType TransportPeerType
	// From defines the nodeID of the source peer.
	From types.NodeID
	// Msg is the actual payload of this message.
	Msg interface{}
}

// TransportServer defines the peer server in the network.
type TransportServer interface {
	Transport
	// Host the server, consider it's a setup procedure. The
	// returned channel could be used after 'WaitForPeers' returns.
	Host() (chan *TransportEnvelope, error)
	// WaitForPeers waits for all peers to join the network.
	WaitForPeers(numPeers uint32) error
}

// TransportClient defines those peers in the network.
type TransportClient interface {
	Transport
	// Report a message to the peer server.
	Report(msg interface{}) error
	// Join the network, should block until joined.
	Join(serverEndpoint interface{}) (<-chan *TransportEnvelope, error)
}

// Transport defines the interface for basic transportation capabilities.
type Transport interface {
	// Broadcast a message to all peers in network.
	Broadcast(msg interface{}) error
	// Send one message to a peer.
	Send(endpoint types.NodeID, msg interface{}) error
	// Close would cleanup allocated resources.
	Close() error

	// Peers return public keys of all connected nodes in p2p favor.
	// This method should be accessed after ether 'Join' or 'WaitForPeers'
	// returned.
	Peers() []crypto.PublicKey
}

// Marshaller defines an interface to convert between interface{} and []byte.
type Marshaller interface {
	// Unmarshal converts a []byte back to interface{} based on the type
	// of message.
	Unmarshal(msgType string, payload []byte) (msg interface{}, err error)
	// Marshal converts a message to byte string
	Marshal(msg interface{}) (msgType string, payload []byte, err error)
}
