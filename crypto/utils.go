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

package crypto

import (
	"encoding/hex"

	"github.com/ethereum/go-ethereum/crypto"

	"github.com/dexon-foundation/dexon-consensus-core/common"
)

// Keccak256Hash calculates and returns the Keccak256 hash of the input data,
// converting it to an internal Hash data structure.
func Keccak256Hash(data ...[]byte) (h common.Hash) {
	return common.Hash(crypto.Keccak256Hash(data...))
}

// Clone returns a deep copy of a signature.
func (sig Signature) Clone() Signature {
	return append(Signature{}, sig...)
}

func (sig Signature) String() string {
	return hex.EncodeToString([]byte(sig[:]))
}
