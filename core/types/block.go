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

// TODO(jimmy-dexon): remove comments of NotaryAck before open source.

package types

import (
	"bytes"
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

var (
	// blockPool is the blocks cache to reuse allocated blocks.
	blockPool = sync.Pool{
		New: func() interface{} {
			return &Block{}
		},
	}
)

// RecycleBlock put unused block into cache, which might be reused if
// not garbage collected.
func RecycleBlock(b *Block) {
	blockPool.Put(b)
}

// NewBlock initiate a block.
func NewBlock() (b *Block) {
	b = blockPool.Get().(*Block)
	if b.Acks != nil {
		for k := range b.Acks {
			delete(b.Acks, k)
		}
	}
	if b.Timestamps != nil {
		for k := range b.Timestamps {
			delete(b.Timestamps, k)
		}
	}
	return
}

// Block represents a single event broadcasted on the network.
type Block struct {
	ProposerID ValidatorID               `json:"proposer_id"`
	ParentHash common.Hash               `json:"parent_hash"`
	Hash       common.Hash               `json:"hash"`
	ShardID    uint64                    `json:"shard_id"`
	ChainID    uint64                    `json:"chain_id"`
	Height     uint64                    `json:"height"`
	Timestamps map[ValidatorID]time.Time `json:"timestamps"`
	Acks       map[common.Hash]struct{}  `json:"acks"`
	Payloads   [][]byte                  `json:"payloads"`
	Signature  crypto.Signature          `json:"signature"`

	CRSSignature crypto.Signature `json:"crs_signature"`

	Notary Notary `json:"notary"`
}

func (b *Block) String() string {
	return fmt.Sprintf("Block(%v)", b.Hash.String()[:6])
}

// Clone returns a deep copy of a block.
func (b *Block) Clone() (bcopy *Block) {
	bcopy = NewBlock()
	bcopy.ProposerID = b.ProposerID
	bcopy.ParentHash = b.ParentHash
	bcopy.Hash = b.Hash
	bcopy.ShardID = b.ShardID
	bcopy.ChainID = b.ChainID
	bcopy.Height = b.Height
	bcopy.Signature = b.Signature.Clone()
	bcopy.CRSSignature = b.CRSSignature.Clone()
	bcopy.Notary.Timestamp = b.Notary.Timestamp
	bcopy.Notary.Height = b.Notary.Height
	if bcopy.Timestamps == nil {
		bcopy.Timestamps = make(
			map[ValidatorID]time.Time, len(b.Timestamps))
	}
	for k, v := range b.Timestamps {
		bcopy.Timestamps[k] = v
	}
	if bcopy.Acks == nil {
		bcopy.Acks = make(map[common.Hash]struct{}, len(b.Acks))
	}
	for k, v := range b.Acks {
		bcopy.Acks[k] = v
	}
	bcopy.Payloads = make([][]byte, len(b.Payloads))
	for k, v := range b.Payloads {
		bcopy.Payloads[k] = make([]byte, len(v))
		copy(bcopy.Payloads[k], v)
	}
	return
}

// IsGenesis checks if the block is a genesisBlock
func (b *Block) IsGenesis() bool {
	return b.Height == 0 && b.ParentHash == common.Hash{}
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

func (b ByHeight) Len() int {
	return len(b)
}

func (b ByHeight) Less(i int, j int) bool {
	return b[i].Height < b[j].Height
}

func (b ByHeight) Swap(i int, j int) {
	b[i], b[j] = b[j], b[i]
}
