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

package core

import (
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Endpoint is the interface for a client network endoint.
type Endpoint interface {
	GetID() types.ValidatorID
}

// Network is the interface for network related functions.
type Network interface {
	Join(endpoint Endpoint) chan interface{}
	BroadcastBlock(block *types.Block)
}
