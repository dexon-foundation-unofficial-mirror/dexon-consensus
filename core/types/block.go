// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Pubic License as
// pubished by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Pubic License for more details.
//
// You should have received a copy of the GNU Lesser General Pubic License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package types

import (
	"bytes"
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
)

// State represent the block process state.
type State int

// Block Status.
const (
	BlockStatusInit State = iota
	BlockStatusAcked
	BlockStatusToTo
	BlockStatusFinal
)

// Block represents a single event broadcasted on the network.
type Block struct {
	ProposerID ValidatorID
	ParentHash common.Hash
	Hash       common.Hash
	Height     uint64
	Timestamps map[ValidatorID]time.Time
	Acks       map[common.Hash]struct{}

	IndirectAcks map[common.Hash]struct{}
	AckedBy      map[common.Hash]bool // bool: direct
	State        State
}

func (b *Block) String() string {
	return fmt.Sprintf("Block(%v)", b.Hash.String()[:6])
}

// Clone returns a deep copy of a block.
func (b *Block) Clone() *Block {
	bcopy := &Block{
		ProposerID: b.ProposerID,
		ParentHash: b.ParentHash,
		Hash:       b.Hash,
		Height:     b.Height,
		Timestamps: make(map[ValidatorID]time.Time),
		Acks:       make(map[common.Hash]struct{}),
	}
	for k, v := range b.Timestamps {
		bcopy.Timestamps[k] = v
	}
	for k, v := range b.Acks {
		bcopy.Acks[k] = v
	}
	return bcopy
}

// ByHash is the helper type for sorting slice of blocks by hash.
type ByHash []*Block

func (b ByHash) Len() int {
	return len(b)
}

func (b ByHash) Less(i int, j int) bool {
	return bytes.Compare([]byte(b[i].Hash[:]), []byte(b[j].Hash[:])) == -1
}

func (b ByHash) Swap(i int, j int) {
	b[i], b[j] = b[j], b[i]
}
