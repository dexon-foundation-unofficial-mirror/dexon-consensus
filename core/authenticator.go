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

package core

import (
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Authenticator verify data owner.
type Authenticator struct {
	prvKey crypto.PrivateKey
	pubKey crypto.PublicKey
}

// NewAuthenticator constructs an Authenticator instance.
func NewAuthenticator(prvKey crypto.PrivateKey) *Authenticator {
	return &Authenticator{
		prvKey: prvKey,
		pubKey: prvKey.PublicKey(),
	}
}

// SignBlock signs a types.Block.
func (au *Authenticator) SignBlock(b *types.Block) (err error) {
	b.ProposerID = types.NewNodeID(au.pubKey)
	if b.Hash, err = hashBlock(b); err != nil {
		return
	}
	if b.Signature, err = au.prvKey.Sign(b.Hash); err != nil {
		return
	}
	return
}

// SignVote signs a types.Vote.
func (au *Authenticator) SignVote(v *types.Vote) (err error) {
	v.ProposerID = types.NewNodeID(au.pubKey)
	v.Signature, err = au.prvKey.Sign(hashVote(v))
	return
}

// SignCRS signs CRS signature of types.Block.
func (au *Authenticator) SignCRS(b *types.Block, crs common.Hash) (err error) {
	if b.ProposerID != types.NewNodeID(au.pubKey) {
		err = ErrInvalidProposerID
		return
	}
	b.CRSSignature, err = au.prvKey.Sign(hashCRS(b, crs))
	return
}

// VerifyBlock verifies the signature of types.Block.
func (au *Authenticator) VerifyBlock(b *types.Block) (err error) {
	hash, err := hashBlock(b)
	if err != nil {
		return
	}
	if hash != b.Hash {
		err = ErrIncorrectHash
		return
	}
	pubKey, err := crypto.SigToPub(b.Hash, b.Signature)
	if err != nil {
		return
	}
	if !b.ProposerID.Equal(crypto.Keccak256Hash(pubKey.Bytes())) {
		err = ErrIncorrectSignature
		return
	}
	return
}

// VerifyVote verifies the signature of types.Vote.
func (au *Authenticator) VerifyVote(v *types.Vote) (bool, error) {
	return verifyVoteSignature(v)
}

// VerifyCRS verifies the CRS signature of types.Block.
func (au *Authenticator) VerifyCRS(b *types.Block, crs common.Hash) (bool, error) {
	return verifyCRSSignature(b, crs)
}
