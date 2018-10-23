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

package dkg

import (
	"fmt"

	"github.com/dexon-foundation/bls/ffi/go/bls"

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
)

// PartialSignature is a partial signature in DKG+TSIG protocol.
type PartialSignature crypto.Signature

var (
	// ErrEmptySignature is reported if the signature is empty.
	ErrEmptySignature = fmt.Errorf("invalid empty signature")
)

// RecoverSignature recovers TSIG signature.
func RecoverSignature(sigs []PartialSignature, signerIDs IDs) (
	crypto.Signature, error) {
	blsSigs := make([]bls.Sign, len(sigs))
	for i, sig := range sigs {
		if len(sig.Signature) == 0 {
			return crypto.Signature{}, ErrEmptySignature
		}
		if err := blsSigs[i].Deserialize([]byte(sig.Signature)); err != nil {
			return crypto.Signature{}, err
		}
	}
	var recoverSig bls.Sign
	if err := recoverSig.Recover(blsSigs, []bls.ID(signerIDs)); err != nil {
		return crypto.Signature{}, err
	}
	return crypto.Signature{
		Type:      cryptoType,
		Signature: recoverSig.Serialize()}, nil
}

// RecoverGroupPublicKey recovers group public key.
func RecoverGroupPublicKey(pubShares []*PublicKeyShares) *PublicKey {
	var pub *PublicKey
	for _, pubShare := range pubShares {
		pk0 := pubShare.masterPublicKey[0]
		if pub == nil {
			pub = &PublicKey{
				publicKey: pk0,
			}
		} else {
			pub.publicKey.Add(&pk0)
		}
	}
	return pub
}
