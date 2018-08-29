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
	"sort"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type leaderSelector struct {
	common.Hashes
}

func newLeaderSelector() *leaderSelector {
	return &leaderSelector{}
}

func (l *leaderSelector) leaderBlockHash() common.Hash {
	// TODO(jimmy-dexon): return leader based on paper.
	sort.Sort(l.Hashes)
	return l.Hashes[len(l.Hashes)-1]
}

func (l *leaderSelector) processBlock(block *types.Block) {
	l.Hashes = append(l.Hashes, block.Hash)
}
