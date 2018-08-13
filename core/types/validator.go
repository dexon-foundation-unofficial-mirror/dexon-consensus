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

package types

import (
	"bytes"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// ValidatorID is the ID type for validators.
type ValidatorID struct {
	common.Hash
}

// NewValidatorID returns a ValidatorID with Hash set to the hash value of
// public key.
func NewValidatorID(pubKey crypto.PublicKey) ValidatorID {
	return ValidatorID{Hash: crypto.Keccak256Hash(pubKey.Bytes())}
}

// Equal checks if the hash representation is the same ValidatorID.
func (v ValidatorID) Equal(hash common.Hash) bool {
	return v.Hash == hash
}

// ValidatorIDs implements sort.Interface for ValidatorID.
type ValidatorIDs []ValidatorID

func (v ValidatorIDs) Len() int {
	return len(v)
}

func (v ValidatorIDs) Less(i int, j int) bool {
	return bytes.Compare([]byte(v[i].Hash[:]), []byte(v[j].Hash[:])) == -1
}

func (v ValidatorIDs) Swap(i int, j int) {
	v[i], v[j] = v[j], v[i]
}
