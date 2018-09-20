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

package test

import (
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// StopByConfirmedBlocks would make sure each nodes confirms
// at least X blocks proposed by itself.
type StopByConfirmedBlocks struct {
	apps               map[types.NodeID]*App
	dbs                map[types.NodeID]blockdb.BlockDatabase
	lastCheckDelivered map[types.NodeID]int
	confirmedBlocks    map[types.NodeID]int
	blockCount         int
	lock               sync.Mutex
}

// NewStopByConfirmedBlocks construct an StopByConfirmedBlocks instance.
func NewStopByConfirmedBlocks(
	blockCount int,
	apps map[types.NodeID]*App,
	dbs map[types.NodeID]blockdb.BlockDatabase) *StopByConfirmedBlocks {

	confirmedBlocks := make(map[types.NodeID]int)
	for nID := range apps {
		confirmedBlocks[nID] = 0
	}
	return &StopByConfirmedBlocks{
		apps:               apps,
		dbs:                dbs,
		lastCheckDelivered: make(map[types.NodeID]int),
		confirmedBlocks:    confirmedBlocks,
		blockCount:         blockCount,
	}
}

// ShouldStop implements Stopper interface.
func (s *StopByConfirmedBlocks) ShouldStop(nID types.NodeID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Accumulate confirmed blocks proposed by this node in this round.
	lastChecked := s.lastCheckDelivered[nID]
	currentConfirmedBlocks := s.confirmedBlocks[nID]
	db := s.dbs[nID]
	s.apps[nID].Check(func(app *App) {
		for _, h := range app.DeliverSequence[lastChecked:] {
			b, err := db.Get(h)
			if err != nil {
				panic(err)
			}
			if b.ProposerID == nID {
				currentConfirmedBlocks++
			}
		}
		s.lastCheckDelivered[nID] = len(app.DeliverSequence)
	})
	s.confirmedBlocks[nID] = currentConfirmedBlocks
	// Check if all nodes confirmed at least 'blockCount' blocks.
	for _, v := range s.confirmedBlocks {
		if v < s.blockCount {
			return false
		}
	}
	return true
}
