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
	"fmt"

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
}

// NewSimApp returns point to a new instance of SimApp.
func NewSimApp(id types.ValidatorID, Network PeerServerNetwork) *SimApp {
	return &SimApp{
		ValidatorID: id,
		Network:     Network,
		DeliverID:   0,
	}
}

// ValidateBlock validates a given block.
func (a *SimApp) ValidateBlock(b *types.Block) bool {
	return true
}

// Deliver is called when blocks are delivered by the total ordering algorithm.
func (a *SimApp) Deliver(blocks []*types.Block, early bool) {
	a.Outputs = blocks
	a.Early = early
	fmt.Println("OUTPUT", a.ValidatorID, a.Early, a.Outputs)
	blockHash := common.Hashes{}
	for _, block := range blocks {
		blockHash = append(blockHash, block.Hash)
	}
	a.Network.DeliverBlocks(blockHash, a.DeliverID)
	a.DeliverID++
}
