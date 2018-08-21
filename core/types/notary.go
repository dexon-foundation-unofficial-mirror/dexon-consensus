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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// NotaryAck represents the acking to the compaction chain.
type NotaryAck struct {
	ProposerID      ValidatorID `json:"proposer_id"`
	NotaryBlockHash common.Hash `json:"notary_block_hash"`
	Hash            common.Hash `json:"hash"`
	// NotarySignature is the signature of the hash value of BlockNotary.
	Signature crypto.Signature `json:"signature"`
}

// Clone returns a deep copy of a NotaryAck.
func (a *NotaryAck) Clone() *NotaryAck {
	return &NotaryAck{
		ProposerID:      a.ProposerID,
		NotaryBlockHash: a.NotaryBlockHash,
		Hash:            a.Hash,
		Signature:       a.Signature,
	}
}

// Notary represents the consensus information on the compaction chain.
type Notary struct {
	ParentHash common.Hash `json:"parent_hash"`
	Timestamp  time.Time   `json:"timestamp"`
	Height     uint64      `json:"height"`
}
