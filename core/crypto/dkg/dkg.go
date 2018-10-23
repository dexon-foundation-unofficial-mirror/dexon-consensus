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
	"encoding/json"
	"fmt"
	"io"

	"github.com/dexon-foundation/bls/ffi/go/bls"
	"github.com/dexon-foundation/dexon/rlp"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
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

const cryptoType = "bls"

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
	publicKey  PublicKey
}

// EncodeRLP implements rlp.Encoder
func (prv *PrivateKey) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, prv.Bytes())
}

// DecodeRLP implements rlp.Decoder
func (prv *PrivateKey) DecodeRLP(s *rlp.Stream) error {
	var b []byte
	if err := s.Decode(&b); err != nil {
		return err
	}
	return prv.SetBytes(b)
}

// MarshalJSON implements json.Marshaller.
func (prv *PrivateKey) MarshalJSON() ([]byte, error) {
	return json.Marshal(&prv.privateKey)
}

// UnmarshalJSON implements json.Unmarshaller.
func (prv *PrivateKey) UnmarshalJSON(data []byte) error {
	return json.Unmarshal(data, &prv.privateKey)
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

// Equal check equality between two PrivateKeyShares instances.
func (prvs *PrivateKeyShares) Equal(other *PrivateKeyShares) bool {
	// Check shares.
	if len(prvs.shareIndex) != len(other.shareIndex) {
		return false
	}
	for dID, idx := range prvs.shareIndex {
		otherIdx, exists := other.shareIndex[dID]
		if !exists {
			return false
		}
		if !prvs.shares[idx].privateKey.IsEqual(
			&other.shares[otherIdx].privateKey) {
			return false
		}
	}
	// Check master private keys.
	if len(prvs.masterPrivateKey) != len(other.masterPrivateKey) {
		return false
	}
	for idx, m := range prvs.masterPrivateKey {
		if m.GetHexString() != other.masterPrivateKey[idx].GetHexString() {
			return false
		}
	}
	return true
}

// PublicKeyShares represents a public key shares for DKG protocol.
type PublicKeyShares struct {
	shareCaches     []PublicKey
	shareCacheIndex map[ID]int
	masterPublicKey []bls.PublicKey
}

type rlpPublicKeyShares struct {
	ShareCaches      [][]byte
	ShareCacheIndexK [][]byte
	ShareCacheIndexV []uint32
	MasterPublicKey  [][]byte
}

// Equal checks equality of two PublicKeyShares instance.
func (pubs *PublicKeyShares) Equal(other *PublicKeyShares) bool {
	// Check shares.
	for dID, idx := range pubs.shareCacheIndex {
		otherIdx, exists := other.shareCacheIndex[dID]
		if !exists {
			continue
		}
		if !pubs.shareCaches[idx].publicKey.IsEqual(
			&other.shareCaches[otherIdx].publicKey) {
			return false
		}
	}
	// Check master public keys.
	if len(pubs.masterPublicKey) != len(other.masterPublicKey) {
		return false
	}
	for idx, m := range pubs.masterPublicKey {
		if m.GetHexString() != other.masterPublicKey[idx].GetHexString() {
			return false
		}
	}
	return true
}

// EncodeRLP implements rlp.Encoder
func (pubs *PublicKeyShares) EncodeRLP(w io.Writer) error {
	var rps rlpPublicKeyShares
	for _, share := range pubs.shareCaches {
		rps.ShareCaches = append(rps.ShareCaches, share.Serialize())
	}

	for id, v := range pubs.shareCacheIndex {
		rps.ShareCacheIndexK = append(
			rps.ShareCacheIndexK, id.GetLittleEndian())
		rps.ShareCacheIndexV = append(rps.ShareCacheIndexV, uint32(v))
	}

	for _, m := range pubs.masterPublicKey {
		rps.MasterPublicKey = append(rps.MasterPublicKey, m.Serialize())
	}

	return rlp.Encode(w, rps)
}

// DecodeRLP implements rlp.Decoder
func (pubs *PublicKeyShares) DecodeRLP(s *rlp.Stream) error {
	var dec rlpPublicKeyShares
	if err := s.Decode(&dec); err != nil {
		return err
	}

	if len(dec.ShareCacheIndexK) != len(dec.ShareCacheIndexV) {
		return fmt.Errorf("invalid shareIndex")
	}

	ps := NewEmptyPublicKeyShares()
	for _, share := range dec.ShareCaches {
		var publicKey PublicKey
		if err := publicKey.Deserialize(share); err != nil {
			return err
		}
		ps.shareCaches = append(ps.shareCaches, publicKey)
	}

	for i, k := range dec.ShareCacheIndexK {
		id, err := BytesID(k)
		if err != nil {
			return err
		}
		ps.shareCacheIndex[id] = int(dec.ShareCacheIndexV[i])
	}

	for _, k := range dec.MasterPublicKey {
		var key bls.PublicKey
		if err := key.Deserialize(k); err != nil {
			return err
		}
		ps.masterPublicKey = append(ps.masterPublicKey, key)
	}

	*pubs = *ps
	return nil
}

// MarshalJSON implements json.Marshaller.
func (pubs *PublicKeyShares) MarshalJSON() ([]byte, error) {
	type Alias PublicKeyShares
	data := &struct {
		MasterPublicKeys []*bls.PublicKey `json:"master_public_keys"`
	}{
		make([]*bls.PublicKey, len(pubs.masterPublicKey)),
	}
	for i := range pubs.masterPublicKey {
		data.MasterPublicKeys[i] = &pubs.masterPublicKey[i]
	}
	return json.Marshal(data)
}

// UnmarshalJSON implements json.Unmarshaller.
func (pubs *PublicKeyShares) UnmarshalJSON(data []byte) error {
	type Alias PublicKeyShares
	aux := &struct {
		MasterPublicKeys []*bls.PublicKey `json:"master_public_keys"`
	}{}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	mpk := make([]bls.PublicKey, len(aux.MasterPublicKeys))
	for i, pk := range aux.MasterPublicKeys {
		mpk[i] = *pk
	}
	pubs.masterPublicKey = mpk
	return nil
}

// Clone clones every fields of PublicKeyShares. This method is mainly
// for testing purpose thus would panic when error.
func (pubs *PublicKeyShares) Clone() *PublicKeyShares {
	b, err := rlp.EncodeToBytes(pubs)
	if err != nil {
		panic(err)
	}
	pubsCopy := NewEmptyPublicKeyShares()
	if err := rlp.DecodeBytes(b, pubsCopy); err != nil {
		panic(err)
	}
	return pubsCopy
}

// NewID creates a ew ID structure.
func NewID(id []byte) ID {
	var blsID bls.ID
	blsID.SetLittleEndian(id)
	return blsID
}

// BytesID creates a new ID structure,
// It returns err if the byte slice is not valid.
func BytesID(id []byte) (ID, error) {
	var blsID bls.ID
	err := blsID.SetLittleEndian(id)
	return blsID, err
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
			shareCacheIndex: make(map[ID]int),
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
		shareCacheIndex: make(map[ID]int),
	}
}

// Share returns the share for the ID.
func (pubs *PublicKeyShares) Share(ID ID) (*PublicKey, error) {
	idx, exist := pubs.shareCacheIndex[ID]
	if exist {
		return &pubs.shareCaches[idx], nil
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
	if idx, exist := pubs.shareCacheIndex[ID]; exist {
		if !share.publicKey.IsEqual(&pubs.shareCaches[idx].publicKey) {
			return ErrDuplicatedShare
		}
		return nil
	}
	pubs.shareCacheIndex[ID] = len(pubs.shareCaches)
	pubs.shareCaches = append(pubs.shareCaches, *share)
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
		pk, err := pubs.Share(ID)
		if err != nil {
			return nil, err
		}
		if i == 0 {
			pub.publicKey = pk.publicKey
			continue
		}
		pub.publicKey.Add(&pk.publicKey)
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

// newPublicKeyFromBytes create a new PublicKey structure
// from bytes representation of bls.PublicKey
func newPublicKeyFromBytes(b []byte) (*PublicKey, error) {
	var pub PublicKey
	err := pub.publicKey.Deserialize(b)
	return &pub, err
}

// PublicKey returns the public key associate this private key.
func (prv *PrivateKey) PublicKey() crypto.PublicKey {
	return prv.publicKey
}

// Sign calculates a signature.
func (prv *PrivateKey) Sign(hash common.Hash) (crypto.Signature, error) {
	msg := string(hash[:])
	sign := prv.privateKey.Sign(msg)
	return crypto.Signature{
		Type:      cryptoType,
		Signature: sign.Serialize(),
	}, nil
}

// Bytes returns []byte representation of private key.
func (prv *PrivateKey) Bytes() []byte {
	return prv.privateKey.GetLittleEndian()
}

// SetBytes sets the private key data to []byte.
func (prv *PrivateKey) SetBytes(bytes []byte) error {
	var key bls.SecretKey
	if err := key.SetLittleEndian(bytes); err != nil {
		return err
	}
	prv.privateKey = key
	prv.publicKey = *newPublicKey(&prv.privateKey)
	return nil
}

// String returns string representation of privat key.
func (prv *PrivateKey) String() string {
	return prv.privateKey.GetHexString()
}

// VerifySignature checks that the given public key created signature over hash.
func (pub PublicKey) VerifySignature(
	hash common.Hash, signature crypto.Signature) bool {
	if len(signature.Signature) == 0 {
		return false
	}
	var sig bls.Sign
	if err := sig.Deserialize(signature.Signature[:]); err != nil {
		fmt.Println(err)
		return false
	}
	msg := string(hash[:])
	return sig.Verify(&pub.publicKey, msg)
}

// Bytes returns []byte representation of public key.
func (pub PublicKey) Bytes() []byte {
	return pub.publicKey.Serialize()
}

// Serialize return bytes representation of public key.
func (pub *PublicKey) Serialize() []byte {
	return pub.publicKey.Serialize()
}

// Deserialize parses bytes representation of public key.
func (pub *PublicKey) Deserialize(b []byte) error {
	return pub.publicKey.Deserialize(b)
}
