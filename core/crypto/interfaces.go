// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package crypto

import (
	"github.com/dexon-foundation/dexon-consensus/common"
)

// Signature is the basic signature type in DEXON.
type Signature struct {
	Type      string
	Signature []byte
}

// PrivateKey describes the asymmetric cryptography interface that interacts
// with the private key.
type PrivateKey interface {
	// PublicKey returns the public key associate this private key.
	PublicKey() PublicKey

	// Sign calculates a signature.
	Sign(hash common.Hash) (Signature, error)
}

// PublicKey describes the asymmetric cryptography interface that interacts
// with the public key.
type PublicKey interface {
	// VerifySignature checks that the given public key created signature over hash.
	VerifySignature(hash common.Hash, signature Signature) bool

	// Bytes returns the []byte representation of public key.
	Bytes() []byte
}
