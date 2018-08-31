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

package integration

import (
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Network implements core.Network.
type Network struct {
}

// BroadcastVote broadcasts vote to all nodes in DEXON network.
func (n *Network) BroadcastVote(vote *types.Vote) {}

// BroadcastBlock broadcasts block to all nodes in DEXON network.
func (n *Network) BroadcastBlock(block *types.Block) {
}

// BroadcastNotaryAck broadcasts notaryAck to all nodes in DEXON network.
func (n *Network) BroadcastNotaryAck(notaryAck *types.NotaryAck) {
}

// ReceiveChan returns a channel to receive messages from DEXON network.
func (n *Network) ReceiveChan() <-chan interface{} {
	return make(chan interface{})
}
