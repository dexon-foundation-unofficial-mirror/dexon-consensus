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

// StopByConfirmedBlocks would make sure each validators confirms
// at least X blocks proposed by itself.
type StopByConfirmedBlocks struct {
	apps               map[types.ValidatorID]*App
	dbs                map[types.ValidatorID]blockdb.BlockDatabase
	lastCheckDelivered map[types.ValidatorID]int
	confirmedBlocks    map[types.ValidatorID]int
	blockCount         int
	lock               sync.Mutex
}

// NewStopByConfirmedBlocks construct an StopByConfirmedBlocks instance.
func NewStopByConfirmedBlocks(
	blockCount int,
	apps map[types.ValidatorID]*App,
	dbs map[types.ValidatorID]blockdb.BlockDatabase) *StopByConfirmedBlocks {

	confirmedBlocks := make(map[types.ValidatorID]int)
	for vID := range apps {
		confirmedBlocks[vID] = 0
	}
	return &StopByConfirmedBlocks{
		apps:               apps,
		dbs:                dbs,
		lastCheckDelivered: make(map[types.ValidatorID]int),
		confirmedBlocks:    confirmedBlocks,
		blockCount:         blockCount,
	}
}

// ShouldStop implements Stopper interface.
func (s *StopByConfirmedBlocks) ShouldStop(vID types.ValidatorID) bool {
	s.lock.Lock()
	defer s.lock.Unlock()

	// Accumulate confirmed blocks proposed by this validator in this round.
	lastChecked := s.lastCheckDelivered[vID]
	currentConfirmedBlocks := s.confirmedBlocks[vID]
	db := s.dbs[vID]
	s.apps[vID].Check(func(app *App) {
		for _, h := range app.DeliverSequence[lastChecked:] {
			b, err := db.Get(h)
			if err != nil {
				panic(err)
			}
			if b.ProposerID == vID {
				currentConfirmedBlocks++
			}
		}
		s.lastCheckDelivered[vID] = len(app.DeliverSequence)
	})
	s.confirmedBlocks[vID] = currentConfirmedBlocks
	// Check if all validators confirmed at least 'blockCount' blocks.
	for _, v := range s.confirmedBlocks {
		if v < s.blockCount {
			return false
		}
	}
	return true
}
