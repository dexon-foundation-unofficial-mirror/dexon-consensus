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
	"fmt"

	"github.com/dexon-foundation/dexon/crypto"

	"github.com/dexon-foundation/dexon-consensus-core/common"
)

var (
	// ErrSigToPubTypeNotFound is reported if the type is already used.
	ErrSigToPubTypeNotFound = fmt.Errorf("type of sigToPub is not found")

	// ErrSigToPubTypeAlreadyExist is reported if the type is already used.
	ErrSigToPubTypeAlreadyExist = fmt.Errorf("type of sigToPub is already exist")
)

// SigToPubFn is a function to recover public key from signature.
type SigToPubFn func(hash common.Hash, signature Signature) (PublicKey, error)

var sigToPubCB map[string]SigToPubFn

func init() {
	sigToPubCB = make(map[string]SigToPubFn)
}

// Keccak256Hash calculates and returns the Keccak256 hash of the input data,
// converting it to an internal Hash data structure.
func Keccak256Hash(data ...[]byte) (h common.Hash) {
	return common.Hash(crypto.Keccak256Hash(data...))
}

// Clone returns a deep copy of a signature.
func (sig Signature) Clone() Signature {
	return Signature{
		Type:      sig.Type,
		Signature: sig.Signature[:],
	}
}

func (sig Signature) String() string {
	return hex.EncodeToString([]byte(sig.Signature[:]))
}

// RegisterSigToPub registers a sigToPub function of type.
func RegisterSigToPub(sigType string, sigToPub SigToPubFn) error {
	if _, exist := sigToPubCB[sigType]; exist {
		return ErrSigToPubTypeAlreadyExist
	}
	sigToPubCB[sigType] = sigToPub
	return nil
}

// SigToPub recovers public key from signature.
func SigToPub(hash common.Hash, signature Signature) (PublicKey, error) {
	sigToPub, exist := sigToPubCB[signature.Type]
	if !exist {
		return nil, ErrSigToPubTypeNotFound
	}
	return sigToPub(hash, signature)
}
