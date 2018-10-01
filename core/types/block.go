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

// TODO(jimmy-dexon): remove comments of WitnessAck before open source.

package types

import (
	"bytes"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
)

var (
	// blockPool is the blocks cache to reuse allocated blocks.
	blockPool = sync.Pool{
		New: func() interface{} {
			return &Block{}
		},
	}
)

// Witness represents the consensus information on the compaction chain.
type Witness struct {
	Timestamp time.Time `json:"timestamp"`
	Height    uint64    `json:"height"`
	Data      []byte    `json:"data"`
}

// RecycleBlock put unused block into cache, which might be reused if
// not garbage collected.
func RecycleBlock(b *Block) {
	blockPool.Put(b)
}

// NewBlock initiate a block.
func NewBlock() (b *Block) {
	b = blockPool.Get().(*Block)
	b.Acks = b.Acks[:0]
	return
}

// Block represents a single event broadcasted on the network.
type Block struct {
	ProposerID NodeID              `json:"proposer_id"`
	ParentHash common.Hash         `json:"parent_hash"`
	Hash       common.Hash         `json:"hash"`
	Position   Position            `json:"position"`
	Timestamp  time.Time           `json:"timestamps"`
	Acks       common.SortedHashes `json:"acks"`
	Payload    []byte              `json:"payload"`
	Witness    Witness             `json:"witness"`
	Signature  crypto.Signature    `json:"signature"`

	CRSSignature crypto.Signature `json:"crs_signature"`
}

func (b *Block) String() string {
	return fmt.Sprintf("Block(%v:%d:%d)", b.Hash.String()[:6],
		b.Position.ChainID, b.Position.Height)
}

// Clone returns a deep copy of a block.
func (b *Block) Clone() (bcopy *Block) {
	bcopy = NewBlock()
	bcopy.ProposerID = b.ProposerID
	bcopy.ParentHash = b.ParentHash
	bcopy.Hash = b.Hash
	bcopy.Position.ChainID = b.Position.ChainID
	bcopy.Position.Height = b.Position.Height
	bcopy.Signature = b.Signature.Clone()
	bcopy.CRSSignature = b.CRSSignature.Clone()
	bcopy.Witness.Timestamp = b.Witness.Timestamp
	bcopy.Witness.Height = b.Witness.Height
	bcopy.Timestamp = b.Timestamp
	bcopy.Acks = make(common.SortedHashes, len(b.Acks))
	copy(bcopy.Acks, b.Acks)
	bcopy.Payload = make([]byte, len(b.Payload))
	copy(bcopy.Payload, b.Payload)
	return
}

// IsGenesis checks if the block is a genesisBlock
func (b *Block) IsGenesis() bool {
	return b.Position.Height == 0 && b.ParentHash == common.Hash{}
}

// IsAcking checks if a block acking another by it's hash.
func (b *Block) IsAcking(hash common.Hash) bool {
	idx := sort.Search(len(b.Acks), func(i int) bool {
		return bytes.Compare(b.Acks[i][:], hash[:]) >= 0
	})
	return !(idx == len(b.Acks) || b.Acks[idx] != hash)
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

// ByHeight is the helper type for sorting slice of blocks by height.
type ByHeight []*Block

// Len implements Len method in sort.Sort interface.
func (bs ByHeight) Len() int {
	return len(bs)
}

// Less implements Less method in sort.Sort interface.
func (bs ByHeight) Less(i int, j int) bool {
	return bs[i].Position.Height < bs[j].Position.Height
}

// Swap implements Swap method in sort.Sort interface.
func (bs ByHeight) Swap(i int, j int) {
	bs[i], bs[j] = bs[j], bs[i]
}

// Push implements Push method in heap interface.
func (bs *ByHeight) Push(x interface{}) {
	*bs = append(*bs, x.(*Block))
}

// Pop implements Pop method in heap interface.
func (bs *ByHeight) Pop() (ret interface{}) {
	n := len(*bs)
	*bs, ret = (*bs)[0:n-1], (*bs)[n-1]
	return
}
