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
	prvKey     crypto.PrivateKey
	pubKey     crypto.PublicKey
	proposerID types.NodeID
}

// NewAuthenticator constructs an Authenticator instance.
func NewAuthenticator(prvKey crypto.PrivateKey) (auth *Authenticator) {
	auth = &Authenticator{
		prvKey: prvKey,
		pubKey: prvKey.PublicKey(),
	}
	auth.proposerID = types.NewNodeID(auth.pubKey)
	return
}

// SignBlock signs a types.Block.
func (au *Authenticator) SignBlock(b *types.Block) (err error) {
	b.ProposerID = au.proposerID
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
	v.ProposerID = au.proposerID
	v.Signature, err = au.prvKey.Sign(hashVote(v))
	return
}

// SignCRS signs CRS signature of types.Block.
func (au *Authenticator) SignCRS(b *types.Block, crs common.Hash) (err error) {
	if b.ProposerID != au.proposerID {
		err = ErrInvalidProposerID
		return
	}
	b.CRSSignature, err = au.prvKey.Sign(hashCRS(b, crs))
	return
}

// SignDKGComplaint signs a DKG complaint.
func (au *Authenticator) SignDKGComplaint(
	complaint *types.DKGComplaint) (err error) {
	complaint.ProposerID = au.proposerID
	complaint.Signature, err = au.prvKey.Sign(hashDKGComplaint(complaint))
	return
}

// SignDKGMasterPublicKey signs a DKG master public key.
func (au *Authenticator) SignDKGMasterPublicKey(
	mpk *types.DKGMasterPublicKey) (err error) {
	mpk.ProposerID = au.proposerID
	mpk.Signature, err = au.prvKey.Sign(hashDKGMasterPublicKey(mpk))
	return
}

// SignDKGPrivateShare signs a DKG private share.
func (au *Authenticator) SignDKGPrivateShare(
	prvShare *types.DKGPrivateShare) (err error) {
	prvShare.ProposerID = au.proposerID
	prvShare.Signature, err = au.prvKey.Sign(hashDKGPrivateShare(prvShare))
	return
}

// SignDKGPartialSignature signs a DKG partial signature.
func (au *Authenticator) SignDKGPartialSignature(
	pSig *types.DKGPartialSignature) (err error) {
	pSig.ProposerID = au.proposerID
	pSig.Signature, err = au.prvKey.Sign(hashDKGPartialSignature(pSig))
	return
}

// SignDKGFinalize signs a DKG finalize message.
func (au *Authenticator) SignDKGFinalize(
	final *types.DKGFinalize) (err error) {
	final.ProposerID = au.proposerID
	final.Signature, err = au.prvKey.Sign(hashDKGFinalize(final))
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
	if !b.ProposerID.Equal(types.NewNodeID(pubKey)) {
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
