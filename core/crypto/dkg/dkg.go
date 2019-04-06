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

package dkg

import (
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"sync/atomic"

	"github.com/dexon-foundation/bls/ffi/go/bls"
	"github.com/dexon-foundation/dexon/rlp"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
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
	if err := bls.Init(curve); err != nil {
		panic(err)
	}

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

// EncodeRLP implements rlp.Encoder
func (prvs *PrivateKeyShares) EncodeRLP(w io.Writer) error {
	data := make([][][]byte, 3)
	shares := make([][]byte, len(prvs.shares))
	for i, s := range prvs.shares {
		shares[i] = s.Bytes()
	}
	data[0] = shares

	shareIndex := make([][]byte, 0)
	for k, v := range prvs.shareIndex {
		shareIndex = append(shareIndex, k.GetLittleEndian())

		vBytes, err := rlp.EncodeToBytes(uint64(v))
		if err != nil {
			return err
		}
		shareIndex = append(shareIndex, vBytes)
	}
	data[1] = shareIndex

	mpks := make([][]byte, len(prvs.masterPrivateKey))
	for i, m := range prvs.masterPrivateKey {
		mpks[i] = m.GetLittleEndian()
	}
	data[2] = mpks
	return rlp.Encode(w, data)
}

// DecodeRLP implements rlp.Decoder
func (prvs *PrivateKeyShares) DecodeRLP(s *rlp.Stream) error {
	*prvs = PrivateKeyShares{}
	var dec [][][]byte
	if err := s.Decode(&dec); err != nil {
		return err
	}

	var shares []PrivateKey
	for _, bs := range dec[0] {
		var key PrivateKey
		err := key.SetBytes(bs)
		if err != nil {
			return err
		}
		shares = append(shares, key)
	}
	(*prvs).shares = shares

	sharesIndex := map[ID]int{}
	for i := 0; i < len(dec[1]); i += 2 {
		var key ID
		err := key.SetLittleEndian(dec[1][i])
		if err != nil {
			return err
		}

		var value uint64
		err = rlp.DecodeBytes(dec[1][i+1], &value)
		if err != nil {
			return err
		}

		sharesIndex[key] = int(value)
	}
	(*prvs).shareIndex = sharesIndex

	var mpks []bls.SecretKey
	for _, bs := range dec[2] {
		var key bls.SecretKey
		if err := key.SetLittleEndian(bs); err != nil {
			return err
		}
		mpks = append(mpks, key)
	}
	(*prvs).masterPrivateKey = mpks

	return nil
}

type publicKeySharesCache struct {
	share []PublicKey
	index map[ID]int
}

// PublicKeyShares represents a public key shares for DKG protocol.
type PublicKeyShares struct {
	cache           atomic.Value
	lock            sync.Mutex
	masterPublicKey []bls.PublicKey
}

// Equal checks equality of two PublicKeyShares instance.
func (pubs *PublicKeyShares) Equal(other *PublicKeyShares) bool {
	cache := pubs.cache.Load().(*publicKeySharesCache)
	cacheOther := other.cache.Load().(*publicKeySharesCache)
	// Check shares.
	for dID, idx := range cache.index {
		otherIdx, exists := cacheOther.index[dID]
		if !exists {
			continue
		}
		if !cache.share[idx].publicKey.IsEqual(
			&cacheOther.share[otherIdx].publicKey) {
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
	mpks := make([][]byte, len(pubs.masterPublicKey))
	for i, m := range pubs.masterPublicKey {
		mpks[i] = m.Serialize()
	}
	return rlp.Encode(w, mpks)
}

// DecodeRLP implements rlp.Decoder
func (pubs *PublicKeyShares) DecodeRLP(s *rlp.Stream) error {
	var dec [][]byte
	if err := s.Decode(&dec); err != nil {
		return err
	}

	ps := NewEmptyPublicKeyShares()
	for _, k := range dec {
		var key bls.PublicKey
		if err := key.Deserialize(k); err != nil {
			return err
		}
		ps.masterPublicKey = append(ps.masterPublicKey, key)
	}

	*pubs = *ps.Move()
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
	// #nosec G104
	blsID.SetLittleEndian(id)
	return blsID
}

// BytesID creates a new ID structure,
// It returns err if the byte slice is not valid.
func BytesID(id []byte) (ID, error) {
	var blsID bls.ID
	// #nosec G104
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
	pubShare := NewEmptyPublicKeyShares()
	pubShare.masterPublicKey = mpk
	return &PrivateKeyShares{
		masterPrivateKey: msk,
		shareIndex:       make(map[ID]int),
	}, pubShare
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
		// #nosec G104
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
	pubShares := &PublicKeyShares{}
	pubShares.cache.Store(&publicKeySharesCache{
		index: make(map[ID]int),
	})
	return pubShares
}

// Move will invalidate itself. Do not access to original reference.
func (pubs *PublicKeyShares) Move() *PublicKeyShares {
	return pubs
}

// Share returns the share for the ID.
func (pubs *PublicKeyShares) Share(ID ID) (*PublicKey, error) {
	cache := pubs.cache.Load().(*publicKeySharesCache)
	idx, exist := cache.index[ID]
	if exist {
		return &cache.share[idx], nil
	}
	var pk PublicKey
	if err := pk.publicKey.Set(pubs.masterPublicKey, &ID); err != nil {
		return nil, err
	}
	if err := pubs.AddShare(ID, &pk); err != nil {
		return nil, err
	}
	return &pk, nil
}

// AddShare adds a share.
func (pubs *PublicKeyShares) AddShare(shareID ID, share *PublicKey) error {
	cache := pubs.cache.Load().(*publicKeySharesCache)
	if idx, exist := cache.index[shareID]; exist {
		if !share.publicKey.IsEqual(&cache.share[idx].publicKey) {
			return ErrDuplicatedShare
		}
		return nil
	}
	pubs.lock.Lock()
	defer pubs.lock.Unlock()
	cache = pubs.cache.Load().(*publicKeySharesCache)
	newCache := &publicKeySharesCache{
		index: make(map[ID]int, len(cache.index)+1),
		share: make([]PublicKey, len(cache.share), len(cache.share)+1),
	}
	for k, v := range cache.index {
		newCache.index[k] = v
	}
	copy(newCache.share, cache.share)
	newCache.index[shareID] = len(newCache.share)
	newCache.share = append(newCache.share, *share)
	pubs.cache.Store(newCache)
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
