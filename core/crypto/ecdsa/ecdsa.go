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

package ecdsa

import (
	"crypto/ecdsa"

	dexCrypto "github.com/dexon-foundation/dexon/crypto"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
)

const cryptoType = "ecdsa"

func init() {
	crypto.RegisterSigToPub(cryptoType, SigToPub)
}

// PrivateKey represents a private key structure used in geth and implments
// Crypto.PrivateKey interface.
type PrivateKey struct {
	privateKey *ecdsa.PrivateKey
}

// PublicKey represents a public key structure used in geth and implements
// Crypto.PublicKey interface.
type PublicKey struct {
	publicKey *ecdsa.PublicKey
}

// NewPrivateKey creates a new PrivateKey structure.
func NewPrivateKey() (*PrivateKey, error) {
	key, err := dexCrypto.GenerateKey()
	if err != nil {
		return nil, err
	}
	return &PrivateKey{privateKey: key}, nil
}

// NewPrivateKeyFromECDSA creates a new PrivateKey structure from
// ecdsa.PrivateKey.
func NewPrivateKeyFromECDSA(key *ecdsa.PrivateKey) *PrivateKey {
	return &PrivateKey{privateKey: key}
}

// NewPublicKeyFromECDSA creates a new PublicKey structure from
// ecdsa.PublicKey.
func NewPublicKeyFromECDSA(key *ecdsa.PublicKey) *PublicKey {
	return &PublicKey{publicKey: key}
}

// NewPublicKeyFromByteSlice constructs an eth.publicKey instance from
// a byte slice.
func NewPublicKeyFromByteSlice(b []byte) (crypto.PublicKey, error) {
	pub, err := dexCrypto.UnmarshalPubkey(b)
	if err != nil {
		return &PublicKey{}, err
	}
	return &PublicKey{publicKey: pub}, nil
}

// PublicKey returns the public key associate this private key.
func (prv *PrivateKey) PublicKey() crypto.PublicKey {
	return NewPublicKeyFromECDSA(&(prv.privateKey.PublicKey))
}

// Sign calculates an ECDSA signature.
//
// This function is susceptible to chosen plaintext attacks that can leak
// information about the private key that is used for signing. Callers must
// be aware that the given hash cannot be chosen by an adversery. Common
// solution is to hash any input before calculating the signature.
//
// The produced signature is in the [R || S || V] format where V is 0 or 1.
func (prv *PrivateKey) Sign(hash common.Hash) (
	sig crypto.Signature, err error) {
	s, err := dexCrypto.Sign(hash[:], prv.privateKey)
	sig = crypto.Signature{
		Type:      cryptoType,
		Signature: s,
	}
	return
}

// VerifySignature checks that the given public key created signature over hash.
// The public key should be in compressed (33 bytes) or uncompressed (65 bytes)
// format.
// The signature should have the 64 byte [R || S] format.
func (pub *PublicKey) VerifySignature(
	hash common.Hash, signature crypto.Signature) bool {
	sig := signature.Signature
	if len(sig) == 65 {
		// The last byte is for ecrecover.
		sig = sig[:64]
	}
	return dexCrypto.VerifySignature(pub.Bytes(), hash[:], sig)
}

// Compress encodes a public key to the 33-byte compressed format.
func (pub *PublicKey) Compress() []byte {
	return dexCrypto.CompressPubkey(pub.publicKey)
}

// Bytes returns the []byte representation of uncompressed public key. (65 bytes)
func (pub *PublicKey) Bytes() []byte {
	return dexCrypto.FromECDSAPub(pub.publicKey)
}

// SigToPub returns the PublicKey that created the given signature.
func SigToPub(
	hash common.Hash, signature crypto.Signature) (crypto.PublicKey, error) {
	key, err := dexCrypto.SigToPub(hash[:], signature.Signature[:])
	if err != nil {
		return &PublicKey{}, err
	}
	return &PublicKey{publicKey: key}, nil
}
