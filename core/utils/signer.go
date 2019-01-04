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

package utils

import (
	"errors"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

// Errors for signer.
var (
	ErrInvalidProposerID  = errors.New("invalid proposer id")
	ErrIncorrectHash      = errors.New("hash of block is incorrect")
	ErrIncorrectSignature = errors.New("signature of block is incorrect")
)

// Signer signs a segment of data.
type Signer struct {
	prvKey     crypto.PrivateKey
	pubKey     crypto.PublicKey
	proposerID types.NodeID
}

// NewSigner constructs an Signer instance.
func NewSigner(prvKey crypto.PrivateKey) (s *Signer) {
	s = &Signer{
		prvKey: prvKey,
		pubKey: prvKey.PublicKey(),
	}
	s.proposerID = types.NewNodeID(s.pubKey)
	return
}

// SignBlock signs a types.Block.
func (s *Signer) SignBlock(b *types.Block) (err error) {
	b.ProposerID = s.proposerID
	b.PayloadHash = crypto.Keccak256Hash(b.Payload)
	if b.Hash, err = HashBlock(b); err != nil {
		return
	}
	if b.Signature, err = s.prvKey.Sign(b.Hash); err != nil {
		return
	}
	return
}

// SignVote signs a types.Vote.
func (s *Signer) SignVote(v *types.Vote) (err error) {
	v.ProposerID = s.proposerID
	v.Signature, err = s.prvKey.Sign(HashVote(v))
	return
}

// SignCRS signs CRS signature of types.Block.
func (s *Signer) SignCRS(b *types.Block, crs common.Hash) (err error) {
	if b.ProposerID != s.proposerID {
		err = ErrInvalidProposerID
		return
	}
	b.CRSSignature, err = s.prvKey.Sign(hashCRS(b, crs))
	return
}

// SignDKGComplaint signs a DKG complaint.
func (s *Signer) SignDKGComplaint(complaint *typesDKG.Complaint) (err error) {
	complaint.ProposerID = s.proposerID
	complaint.Signature, err = s.prvKey.Sign(hashDKGComplaint(complaint))
	return
}

// SignDKGMasterPublicKey signs a DKG master public key.
func (s *Signer) SignDKGMasterPublicKey(
	mpk *typesDKG.MasterPublicKey) (err error) {
	mpk.ProposerID = s.proposerID
	mpk.Signature, err = s.prvKey.Sign(hashDKGMasterPublicKey(mpk))
	return
}

// SignDKGPrivateShare signs a DKG private share.
func (s *Signer) SignDKGPrivateShare(
	prvShare *typesDKG.PrivateShare) (err error) {
	prvShare.ProposerID = s.proposerID
	prvShare.Signature, err = s.prvKey.Sign(hashDKGPrivateShare(prvShare))
	return
}

// SignDKGPartialSignature signs a DKG partial signature.
func (s *Signer) SignDKGPartialSignature(
	pSig *typesDKG.PartialSignature) (err error) {
	pSig.ProposerID = s.proposerID
	pSig.Signature, err = s.prvKey.Sign(hashDKGPartialSignature(pSig))
	return
}

// SignDKGMPKReady signs a DKG ready message.
func (s *Signer) SignDKGMPKReady(ready *typesDKG.MPKReady) (err error) {
	ready.ProposerID = s.proposerID
	ready.Signature, err = s.prvKey.Sign(hashDKGMPKReady(ready))
	return
}

// SignDKGFinalize signs a DKG finalize message.
func (s *Signer) SignDKGFinalize(final *typesDKG.Finalize) (err error) {
	final.ProposerID = s.proposerID
	final.Signature, err = s.prvKey.Sign(hashDKGFinalize(final))
	return
}
