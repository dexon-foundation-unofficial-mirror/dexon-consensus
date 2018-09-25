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
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

type keyHolder struct {
	prvKey   crypto.PrivateKey
	pubKey   crypto.PublicKey
	sigToPub SigToPubFn
}

func newKeyHolder(prvKey crypto.PrivateKey, sigToPub SigToPubFn) *keyHolder {
	return &keyHolder{
		prvKey:   prvKey,
		pubKey:   prvKey.PublicKey(),
		sigToPub: sigToPub,
	}
}

// SignBlock implements core.Signer.
func (h *keyHolder) SignBlock(b *types.Block) (err error) {
	b.ProposerID = types.NewNodeID(h.pubKey)
	if b.Hash, err = hashBlock(b); err != nil {
		return
	}
	if b.Signature, err = h.prvKey.Sign(b.Hash); err != nil {
		return
	}
	return
}

// SignVote implements core.Signer.
func (h *keyHolder) SignVote(v *types.Vote) (err error) {
	v.ProposerID = types.NewNodeID(h.pubKey)
	v.Signature, err = h.prvKey.Sign(hashVote(v))
	return
}

// SignCRS implements core.Signer
func (h *keyHolder) SignCRS(b *types.Block, crs common.Hash) (err error) {
	if b.ProposerID != types.NewNodeID(h.pubKey) {
		err = ErrInvalidProposerID
		return
	}
	b.CRSSignature, err = h.prvKey.Sign(hashCRS(b, crs))
	return
}

// VerifyBlock implements core.CryptoVerifier.
func (h *keyHolder) VerifyBlock(b *types.Block) (err error) {
	hash, err := hashBlock(b)
	if err != nil {
		return
	}
	if hash != b.Hash {
		err = ErrIncorrectHash
		return
	}
	pubKey, err := h.sigToPub(b.Hash, b.Signature)
	if err != nil {
		return
	}
	if !b.ProposerID.Equal(crypto.Keccak256Hash(pubKey.Bytes())) {
		err = ErrIncorrectSignature
		return
	}
	return
}

// VerifyVote implements core.CryptoVerifier.
func (h *keyHolder) VerifyVote(v *types.Vote) (bool, error) {
	return verifyVoteSignature(v, h.sigToPub)
}

// VerifyWitness implements core.CryptoVerifier.
func (h *keyHolder) VerifyCRS(b *types.Block, crs common.Hash) (bool, error) {
	return verifyCRSSignature(b, crs, h.sigToPub)
}
