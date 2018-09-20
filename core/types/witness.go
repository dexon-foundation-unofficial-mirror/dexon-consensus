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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// WitnessAck represents the acking to the compaction chain.
type WitnessAck struct {
	ProposerID       NodeID      `json:"proposer_id"`
	WitnessBlockHash common.Hash `json:"witness_block_hash"`
	Hash             common.Hash `json:"hash"`
	// WitnessSignature is the signature of the hash value of BlockWitness.
	Signature crypto.Signature `json:"signature"`
}

// Clone returns a deep copy of a WitnessAck.
func (a *WitnessAck) Clone() *WitnessAck {
	return &WitnessAck{
		ProposerID:       a.ProposerID,
		WitnessBlockHash: a.WitnessBlockHash,
		Hash:             a.Hash,
		Signature:        a.Signature,
	}
}

// Witness represents the consensus information on the compaction chain.
type Witness struct {
	ParentHash common.Hash `json:"parent_hash"`
	Timestamp  time.Time   `json:"timestamp"`
	Height     uint64      `json:"height"`
	Data       []byte      `json:"data"`
}

// WitnessResult is the result pass from application containing the witness
// data.
type WitnessResult struct {
	BlockHash common.Hash
	Data      []byte
}
