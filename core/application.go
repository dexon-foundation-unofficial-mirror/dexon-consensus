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

package core

import (
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
)

// Application describes the application interface that interacts with DEXON
// consensus core.
type Application interface {
	// StronglyAcked is called when a block is strongly acked.
	StronglyAcked(blockHash common.Hash)

	// TotalOrderingDeliver is called when the total ordering algorithm deliver // a set of block.
	TotalOrderingDeliver(blockHashes common.Hashes, early bool)

	// DeliverBlock is called when a block is add to the compaction chain.
	DeliverBlock(blockHash common.Hash, timestamp time.Time)
}
