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

	"github.com/herumi/bls/ffi/go/bls"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

var (
	// ErrDuplicatedShare is reported when adding an private key share of same id.
	ErrDuplicatedShare = fmt.Errorf("invalid share")
	// ErrNoIDToRecover is reported when no id is provided for recovering private
	// key.
	ErrNoIDToRecover = fmt.Errorf("no id to recover private key")
	// ErrShareNotFound is reported when the private key share of id is not found
	// when recovering private key.
	ErrShareNotFound = fmt.Errorf("share not found")
)

var publicKeyLength int

func init() {
	bls.Init(curve)

	pubKey := &bls.PublicKey{}
	publicKeyLength = len(pubKey.Serialize())
}

// PrivateKey represents a private key structure implments
// Crypto.PrivateKey interface.
type PrivateKey struct {
	privateKey bls.SecretKey
	id         bls.ID
	publicKey  PublicKey
}

// ID is the id for DKG protocol.
type ID = bls.ID

// IDs is an array of ID.
type IDs []ID

// PublicKey represents a public key structure implements
// Crypto.PublicKey interface.
type PublicKey struct {
	publicKey bls.PublicKey
}

// PrivateKeyShares represents a private key shares for DKG protocol.
type PrivateKeyShares struct {
	shares           []PrivateKey
	shareIndex       map[ID]int
	masterPrivateKey []bls.SecretKey
}

// PublicKeyShares represents a public key shares for DKG protocol.
type PublicKeyShares struct {
	shares          []PublicKey
	shareIndex      map[ID]int
	masterPublicKey []bls.PublicKey
}

// NewID creates a ew ID structure.
func NewID(id []byte) ID {
	var blsID bls.ID
	blsID.SetLittleEndian(id)
	return blsID
}

// NewPrivateKey creates a new PrivateKey structure.
func NewPrivateKey() *PrivateKey {
	var key bls.SecretKey
	key.SetByCSPRNG()
	return &PrivateKey{
		privateKey: key,
		publicKey:  *newPublicKey(&key),
	}
}

// NewPrivateKeyShares creates a DKG private key shares of threshold t.
func NewPrivateKeyShares(t int) (*PrivateKeyShares, *PublicKeyShares) {
	var prv bls.SecretKey
	prv.SetByCSPRNG()
	msk := prv.GetMasterSecretKey(t)
	mpk := bls.GetMasterPublicKey(msk)
	return &PrivateKeyShares{
			masterPrivateKey: msk,
			shareIndex:       make(map[ID]int),
		}, &PublicKeyShares{
			shareIndex:      make(map[ID]int),
			masterPublicKey: mpk,
		}
}

// NewEmptyPrivateKeyShares creates an empty private key shares.
func NewEmptyPrivateKeyShares() *PrivateKeyShares {
	return &PrivateKeyShares{
		shareIndex: make(map[ID]int),
	}
}

// SetParticipants sets the DKG participants.
func (prvs *PrivateKeyShares) SetParticipants(IDs IDs) {
	prvs.shares = make([]PrivateKey, len(IDs))
	prvs.shareIndex = make(map[ID]int, len(IDs))
	for idx, ID := range IDs {
		prvs.shares[idx].privateKey.Set(prvs.masterPrivateKey, &ID)
		prvs.shareIndex[ID] = idx
	}
}

// AddShare adds a share.
func (prvs *PrivateKeyShares) AddShare(ID ID, share *PrivateKey) error {
	if idx, exist := prvs.shareIndex[ID]; exist {
		if !share.privateKey.IsEqual(&prvs.shares[idx].privateKey) {
			return ErrDuplicatedShare
		}
		return nil
	}
	prvs.shareIndex[ID] = len(prvs.shares)
	prvs.shares = append(prvs.shares, *share)
	return nil
}

// RecoverPrivateKey recovers private key from the shares.
func (prvs *PrivateKeyShares) RecoverPrivateKey(qualifyIDs IDs) (
	*PrivateKey, error) {
	var prv PrivateKey
	if len(qualifyIDs) == 0 {
		return nil, ErrNoIDToRecover
	}
	for i, ID := range qualifyIDs {
		idx, exist := prvs.shareIndex[ID]
		if !exist {
			return nil, ErrShareNotFound
		}
		if i == 0 {
			prv.privateKey = prvs.shares[idx].privateKey
			continue
		}
		prv.privateKey.Add(&prvs.shares[idx].privateKey)
	}
	return &prv, nil
}

// RecoverPublicKey recovers public key from the shares.
func (prvs *PrivateKeyShares) RecoverPublicKey(qualifyIDs IDs) (
	*PublicKey, error) {
	var pub PublicKey
	if len(qualifyIDs) == 0 {
		return nil, ErrNoIDToRecover
	}
	for i, ID := range qualifyIDs {
		idx, exist := prvs.shareIndex[ID]
		if !exist {
			return nil, ErrShareNotFound
		}
		if i == 0 {
			pub.publicKey = *prvs.shares[idx].privateKey.GetPublicKey()
			continue
		}
		pub.publicKey.Add(prvs.shares[idx].privateKey.GetPublicKey())
	}
	return &pub, nil
}

// Share returns the share for the ID.
func (prvs *PrivateKeyShares) Share(ID ID) (*PrivateKey, bool) {
	idx, exist := prvs.shareIndex[ID]
	if !exist {
		return nil, false
	}
	return &prvs.shares[idx], true
}

// NewEmptyPublicKeyShares creates an empty public key shares.
func NewEmptyPublicKeyShares() *PublicKeyShares {
	return &PublicKeyShares{
		shareIndex: make(map[ID]int),
	}
}

// Share returns the share for the ID.
func (pubs *PublicKeyShares) Share(ID ID) (*PublicKey, error) {
	idx, exist := pubs.shareIndex[ID]
	if exist {
		return &pubs.shares[idx], nil
	}
	var pk PublicKey
	if err := pk.publicKey.Set(pubs.masterPublicKey, &ID); err != nil {
		return nil, err
	}
	pubs.AddShare(ID, &pk)
	return &pk, nil
}

// AddShare adds a share.
func (pubs *PublicKeyShares) AddShare(ID ID, share *PublicKey) error {
	if idx, exist := pubs.shareIndex[ID]; exist {
		if !share.publicKey.IsEqual(&pubs.shares[idx].publicKey) {
			return ErrDuplicatedShare
		}
		return nil
	}
	pubs.shareIndex[ID] = len(pubs.shares)
	pubs.shares = append(pubs.shares, *share)
	return nil
}

// VerifyPrvShare verifies if the private key shares is valid.
func (pubs *PublicKeyShares) VerifyPrvShare(ID ID, share *PrivateKey) (
	bool, error) {
	var pk bls.PublicKey
	if err := pk.Set(pubs.masterPublicKey, &ID); err != nil {
		return false, err
	}
	return pk.IsEqual(share.privateKey.GetPublicKey()), nil
}

// VerifyPubShare verifies if the public key shares is valid.
func (pubs *PublicKeyShares) VerifyPubShare(ID ID, share *PublicKey) (
	bool, error) {
	var pk bls.PublicKey
	if err := pk.Set(pubs.masterPublicKey, &ID); err != nil {
		return false, err
	}
	return pk.IsEqual(&share.publicKey), nil
}

// RecoverPublicKey recovers private key from the shares.
func (pubs *PublicKeyShares) RecoverPublicKey(qualifyIDs IDs) (
	*PublicKey, error) {
	var pub PublicKey
	if len(qualifyIDs) == 0 {
		return nil, ErrNoIDToRecover
	}
	for i, ID := range qualifyIDs {
		idx, exist := pubs.shareIndex[ID]
		if !exist {
			return nil, ErrShareNotFound
		}
		if i == 0 {
			pub.publicKey = pubs.shares[idx].publicKey
			continue
		}
		pub.publicKey.Add(&pubs.shares[idx].publicKey)
	}
	return &pub, nil
}

// MasterKeyBytes returns []byte representation of master public key.
func (pubs *PublicKeyShares) MasterKeyBytes() []byte {
	bytes := make([]byte, 0, len(pubs.masterPublicKey)*publicKeyLength)
	for _, pk := range pubs.masterPublicKey {
		bytes = append(bytes, pk.Serialize()...)
	}
	return bytes
}

// newPublicKey creates a new PublicKey structure.
func newPublicKey(prvKey *bls.SecretKey) *PublicKey {
	return &PublicKey{
		publicKey: *prvKey.GetPublicKey(),
	}
}

// PublicKey returns the public key associate this private key.
func (prv *PrivateKey) PublicKey() crypto.PublicKey {
	return prv.publicKey
}

// Sign calculates a signature.
func (prv *PrivateKey) Sign(hash common.Hash) (crypto.Signature, error) {
	msg := string(hash[:])
	sign := prv.privateKey.Sign(msg)
	return crypto.Signature(sign.Serialize()), nil
}

// Bytes returns []byte representation of private key.
func (prv *PrivateKey) Bytes() []byte {
	return prv.privateKey.GetLittleEndian()
}

// VerifySignature checks that the given public key created signature over hash.
func (pub PublicKey) VerifySignature(
	hash common.Hash, signature crypto.Signature) bool {
	var sig bls.Sign
	if err := sig.Deserialize(signature[:]); err != nil {
		fmt.Println(err)
		return false
	}
	msg := string(hash[:])
	return sig.Verify(&pub.publicKey, msg)
}

// Bytes returns []byte representation of public key.
func (pub PublicKey) Bytes() []byte {
	var bytes []byte
	pub.publicKey.Deserialize(bytes)
	return bytes
}
