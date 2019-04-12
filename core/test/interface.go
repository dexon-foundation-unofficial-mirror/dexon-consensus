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

package test

import (
	"time"

	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// BlockRevealer defines the interface to reveal a group
// of pre-generated blocks.
type BlockRevealer interface {
	db.BlockIterator

	// Reset the revealing.
	Reset()
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

	// SetDMoment
	SetDMoment(time.Time)
}

// TransportClient defines those peers in the network.
type TransportClient interface {
	Transport
	// Report a message to the peer server.
	Report(msg interface{}) error
	// Join the network, should block until joined.
	Join(serverEndpoint interface{}) (<-chan *TransportEnvelope, error)

	// DMoment returns the DMoment of the network.
	DMoment() time.Time
}

// Transport defines the interface for basic transportation capabilities.
type Transport interface {
	// Broadcast a message to all peers in network.
	Broadcast(endpoints map[types.NodeID]struct{}, latency LatencyModel,
		msg interface{}) error
	// Send one message to a peer.
	Send(endpoint types.NodeID, msg interface{}) error
	// Close would cleanup allocated resources.
	Close() error

	// Peers return public keys of all connected nodes in p2p favor.
	// This method should be accessed after ether 'Join' or 'WaitForPeers'
	// returned.
	Peers() []crypto.PublicKey

	Disconnect(endpoint types.NodeID)
}

// Marshaller defines an interface to convert between interface{} and []byte.
type Marshaller interface {
	// Unmarshal converts a []byte back to interface{} based on the type
	// of message.
	Unmarshal(msgType string, payload []byte) (msg interface{}, err error)
	// Marshal converts a message to byte string
	Marshal(msg interface{}) (msgType string, payload []byte, err error)
}
