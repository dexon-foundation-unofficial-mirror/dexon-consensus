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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// NotaryAck represents the acking to the compaction chain.
type NotaryAck struct {
	NotaryBlockHash common.Hash `json:"notary_block_hash"`
	// NotarySignature is the signature of the hash value of
	// Block.NotaryParentHash and Block.Notary.
	NotarySignature crypto.Signature `json:"notary_signature"`
}

// CompactionChainAck represents the acking to the compaction chain.
type CompactionChainAck struct {
	AckingBlockHash common.Hash `json:"acking_block_hash"`
	// Signature is the signature of the hash value of
	// Block.ConsensusInfoParentHash and Block.ConsensusInfo.
	ConsensusSignature crypto.Signature `json:"consensus_signature"`
}

// Notary represents the consensus information on the compaction chain.
type Notary struct {
	Timestamp time.Time `json:"timestamp"`
	Height    uint64    `json:"height"`
}

// Block represents a single event broadcasted on the network.
type Block struct {
	ProposerID ValidatorID               `json:"proposer_id"`
	ParentHash common.Hash               `json:"parent_hash"`
	Hash       common.Hash               `json:"hash"`
	Height     uint64                    `json:"height"`
	Timestamps map[ValidatorID]time.Time `json:"timestamps"`
	Acks       map[common.Hash]struct{}  `json:"acks"`
	//NotaryAck  NotaryAck                 `json:"notary_ack"`
	Signature crypto.Signature `json:"signature"`

	Notary Notary `json:"notary"`
	// NotaryParentHash is the hash value of Block.NotaryParentHash
	// and Block.Notary, where Block is the previous block on
	// the compaction chain.
	NotaryParentHash common.Hash `json:"notary_parent_hash"`

	ConsensusInfoParentHash common.Hash `json:"consensus_info_parent_hash"`
}

// Block implements BlockConverter interface.
func (b *Block) Block() *Block {
	return b
}

// Payloads impelmemnts BlockConverter interface.
func (b *Block) Payloads() [][]byte {
	return [][]byte{}
}

// SetBlock implments BlockConverter interface.
func (b *Block) SetBlock(block *Block) {
	*b = *block
}

// BlockConverter interface define the interface for extracting block
// information from an existing object.
type BlockConverter interface {
	Block() *Block
	Payloads() [][]byte
	SetBlock(block *Block)
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
		/*
			NotaryAck: NotaryAck{
				NotaryBlockHash: b.NotaryAck.NotaryBlockHash,
			},
		*/
		Signature: b.Signature,
		Notary: Notary{
			Timestamp: b.Notary.Timestamp,
			Height:    b.Notary.Height,
		},
		NotaryParentHash: b.NotaryParentHash,
	}
	/*
		bcopy.NotaryAck.NotarySignature = append(
			crypto.Signature(nil), b.NotaryAck.NotarySignature...)
	*/
	for k, v := range b.Timestamps {
		bcopy.Timestamps[k] = v
	}
	for k, v := range b.Acks {
		bcopy.Acks[k] = v
	}
	return bcopy
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
