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
	"io"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon/rlp"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
)

// BlockVerifyStatus is the return code for core.Application.VerifyBlock
type BlockVerifyStatus int

// Enums for return value of core.Application.VerifyBlock.
const (
	// VerifyOK: Block is verified.
	VerifyOK BlockVerifyStatus = iota
	// VerifyRetryLater: Block is unable to be verified at this moment.
	// Try again later.
	VerifyRetryLater
	// VerifyInvalidBlock: Block is an invalid one.
	VerifyInvalidBlock
)

var (
	// blockPool is the blocks cache to reuse allocated blocks.
	blockPool = sync.Pool{
		New: func() interface{} {
			return &Block{}
		},
	}
)

type rlpTimestamp struct {
	time.Time
}

func (t *rlpTimestamp) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, uint64(t.UTC().UnixNano()))
}

func (t *rlpTimestamp) DecodeRLP(s *rlp.Stream) error {
	var nano uint64
	err := s.Decode(&nano)
	if err == nil {
		sec := int64(nano) / 1000000000
		nsec := int64(nano) % 1000000000
		t.Time = time.Unix(sec, nsec).UTC()
	}
	return err
}

// FinalizationResult represents the result of DEXON consensus algorithm.
type FinalizationResult struct {
	ParentHash common.Hash `json:"parent_hash"`
	Randomness []byte      `json:"randomness"`
	Timestamp  time.Time   `json:"timestamp"`
	Height     uint64      `json:"height"`
}

type rlpFinalizationResult struct {
	ParentHash common.Hash
	Randomness []byte
	Timestamp  *rlpTimestamp
	Height     uint64
}

// EncodeRLP implements rlp.Encoder
func (f *FinalizationResult) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, &rlpFinalizationResult{
		ParentHash: f.ParentHash,
		Randomness: f.Randomness,
		Timestamp:  &rlpTimestamp{f.Timestamp},
		Height:     f.Height,
	})
}

// DecodeRLP implements rlp.Decoder
func (f *FinalizationResult) DecodeRLP(s *rlp.Stream) error {
	var dec rlpFinalizationResult
	err := s.Decode(&dec)
	if err == nil {
		*f = FinalizationResult{
			ParentHash: dec.ParentHash,
			Randomness: dec.Randomness,
			Timestamp:  dec.Timestamp.Time,
			Height:     dec.Height,
		}
	}
	return err
}

// Witness represents the consensus information on the compaction chain.
type Witness struct {
	Height uint64 `json:"height"`
	Data   []byte `json:"data"`
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
	ProposerID   NodeID              `json:"proposer_id"`
	ParentHash   common.Hash         `json:"parent_hash"`
	Hash         common.Hash         `json:"hash"`
	Position     Position            `json:"position"`
	Timestamp    time.Time           `json:"timestamp"`
	Acks         common.SortedHashes `json:"acks"`
	Payload      []byte              `json:"payload"`
	PayloadHash  common.Hash         `json:"payload_hash"`
	Witness      Witness             `json:"witness"`
	Finalization FinalizationResult  `json:"finalization"`
	Signature    crypto.Signature    `json:"signature"`

	CRSSignature crypto.Signature `json:"crs_signature"`
}

type rlpBlock struct {
	ProposerID   NodeID
	ParentHash   common.Hash
	Hash         common.Hash
	Position     Position
	Timestamp    *rlpTimestamp
	Acks         common.SortedHashes
	Payload      []byte
	PayloadHash  common.Hash
	Witness      *Witness
	Finalization *FinalizationResult
	Signature    crypto.Signature

	CRSSignature crypto.Signature
}

// EncodeRLP implements rlp.Encoder
func (b *Block) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, rlpBlock{
		ProposerID:   b.ProposerID,
		ParentHash:   b.ParentHash,
		Hash:         b.Hash,
		Position:     b.Position,
		Timestamp:    &rlpTimestamp{b.Timestamp},
		Acks:         b.Acks,
		Payload:      b.Payload,
		PayloadHash:  b.PayloadHash,
		Witness:      &b.Witness,
		Finalization: &b.Finalization,
		Signature:    b.Signature,
		CRSSignature: b.CRSSignature,
	})
}

// DecodeRLP implements rlp.Decoder
func (b *Block) DecodeRLP(s *rlp.Stream) error {
	var dec rlpBlock
	err := s.Decode(&dec)
	if err == nil {
		*b = Block{
			ProposerID:   dec.ProposerID,
			ParentHash:   dec.ParentHash,
			Hash:         dec.Hash,
			Position:     dec.Position,
			Timestamp:    dec.Timestamp.Time,
			Acks:         dec.Acks,
			Payload:      dec.Payload,
			PayloadHash:  dec.PayloadHash,
			Witness:      *dec.Witness,
			Finalization: *dec.Finalization,
			Signature:    dec.Signature,
			CRSSignature: dec.CRSSignature,
		}
	}
	return err
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
	bcopy.Position.Round = b.Position.Round
	bcopy.Position.ChainID = b.Position.ChainID
	bcopy.Position.Height = b.Position.Height
	bcopy.Signature = b.Signature.Clone()
	bcopy.CRSSignature = b.CRSSignature.Clone()
	bcopy.Finalization.ParentHash = b.Finalization.ParentHash
	bcopy.Finalization.Timestamp = b.Finalization.Timestamp
	bcopy.Finalization.Height = b.Finalization.Height
	bcopy.Witness.Height = b.Witness.Height
	bcopy.Witness.Data = make([]byte, len(b.Witness.Data))
	copy(bcopy.Witness.Data, b.Witness.Data)
	bcopy.Timestamp = b.Timestamp
	bcopy.Acks = make(common.SortedHashes, len(b.Acks))
	copy(bcopy.Acks, b.Acks)
	bcopy.Payload = make([]byte, len(b.Payload))
	copy(bcopy.Payload, b.Payload)
	bcopy.PayloadHash = b.PayloadHash
	bcopy.Finalization.Randomness = make([]byte, len(b.Finalization.Randomness))
	copy(bcopy.Finalization.Randomness, b.Finalization.Randomness)
	return
}

// IsGenesis checks if the block is a genesisBlock
func (b *Block) IsGenesis() bool {
	return b.Position.Height == 0 && b.ParentHash == common.Hash{}
}

// IsFinalized checks if the finalization data is ready.
func (b *Block) IsFinalized() bool {
	return b.Finalization.Height != 0
}

// IsEmpty checks if the block is an 'empty block'.
func (b *Block) IsEmpty() bool {
	return b.ProposerID.Hash == common.Hash{}
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

// ByPosition is the helper type for sorting slice of blocks by position.
type ByPosition []*Block

// Len implements Len method in sort.Sort interface.
func (bs ByPosition) Len() int {
	return len(bs)
}

// Less implements Less method in sort.Sort interface.
func (bs ByPosition) Less(i int, j int) bool {
	return bs[j].Position.Newer(&bs[i].Position)
}

// Swap implements Swap method in sort.Sort interface.
func (bs ByPosition) Swap(i int, j int) {
	bs[i], bs[j] = bs[j], bs[i]
}

// Push implements Push method in heap interface.
func (bs *ByPosition) Push(x interface{}) {
	*bs = append(*bs, x.(*Block))
}

// Pop implements Pop method in heap interface.
func (bs *ByPosition) Pop() (ret interface{}) {
	n := len(*bs)
	*bs, ret = (*bs)[0:n-1], (*bs)[n-1]
	return
}

// ByFinalizationHeight is the helper type for sorting slice of blocks by
// finalization height.
type ByFinalizationHeight []*Block

// Len implements Len method in sort.Sort interface.
func (bs ByFinalizationHeight) Len() int {
	return len(bs)
}

// Less implements Less method in sort.Sort interface.
func (bs ByFinalizationHeight) Less(i int, j int) bool {
	return bs[i].Finalization.Height < bs[j].Finalization.Height
}

// Swap implements Swap method in sort.Sort interface.
func (bs ByFinalizationHeight) Swap(i int, j int) {
	bs[i], bs[j] = bs[j], bs[i]
}

// Push implements Push method in heap interface.
func (bs *ByFinalizationHeight) Push(x interface{}) {
	*bs = append(*bs, x.(*Block))
}

// Pop implements Pop method in heap interface.
func (bs *ByFinalizationHeight) Pop() (ret interface{}) {
	n := len(*bs)
	*bs, ret = (*bs)[0:n-1], (*bs)[n-1]
	return
}
