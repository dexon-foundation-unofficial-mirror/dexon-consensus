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
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package test

import (
	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
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
	ShouldStop(vID types.ValidatorID) bool
}

// EventHandler defines an interface to handle a Scheduler event.
type EventHandler interface {
	// Handle the event belongs to this handler, and return derivated events.
	Handle(*Event) []*Event
}
