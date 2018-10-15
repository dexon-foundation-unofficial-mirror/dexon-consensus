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
	"encoding/binary"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

func hashWitness(witness *types.Witness) (common.Hash, error) {
	binaryTimestamp, err := witness.Timestamp.UTC().MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, witness.Height)
	return crypto.Keccak256Hash(
		binaryHeight,
		binaryTimestamp,
		witness.Data), nil
}

func hashBlock(block *types.Block) (common.Hash, error) {
	hashPosition := hashPosition(block.Position)
	// Handling Block.Acks.
	binaryAcks := make([][]byte, len(block.Acks))
	for idx, ack := range block.Acks {
		binaryAcks[idx] = ack[:]
	}
	hashAcks := crypto.Keccak256Hash(binaryAcks...)
	binaryTimestamp, err := block.Timestamp.UTC().MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	binaryWitness, err := hashWitness(&block.Witness)
	if err != nil {
		return common.Hash{}, err
	}
	payloadHash := crypto.Keccak256Hash(block.Payload)

	hash := crypto.Keccak256Hash(
		block.ProposerID.Hash[:],
		block.ParentHash[:],
		hashPosition[:],
		hashAcks[:],
		binaryTimestamp[:],
		payloadHash[:],
		binaryWitness[:])
	return hash, nil
}

func verifyBlockSignature(pubkey crypto.PublicKey,
	block *types.Block, sig crypto.Signature) (bool, error) {
	hash, err := hashBlock(block)
	if err != nil {
		return false, err
	}
	return pubkey.VerifySignature(hash, sig), nil
}

func hashVote(vote *types.Vote) common.Hash {
	binaryPeriod := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryPeriod, vote.Period)

	hashPosition := hashPosition(vote.Position)

	hash := crypto.Keccak256Hash(
		vote.ProposerID.Hash[:],
		vote.BlockHash[:],
		binaryPeriod,
		hashPosition[:],
		[]byte{byte(vote.Type)},
	)
	return hash
}

func verifyVoteSignature(vote *types.Vote) (bool, error) {
	hash := hashVote(vote)
	pubKey, err := crypto.SigToPub(hash, vote.Signature)
	if err != nil {
		return false, err
	}
	if vote.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashCRS(block *types.Block, crs common.Hash) common.Hash {
	hashPos := hashPosition(block.Position)
	return crypto.Keccak256Hash(crs[:], hashPos[:])
}

func verifyCRSSignature(block *types.Block, crs common.Hash) (
	bool, error) {
	hash := hashCRS(block, crs)
	pubKey, err := crypto.SigToPub(hash, block.CRSSignature)
	if err != nil {
		return false, err
	}
	if block.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashPosition(position types.Position) common.Hash {
	binaryChainID := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryChainID, position.ChainID)

	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, position.Height)

	return crypto.Keccak256Hash(
		binaryChainID,
		binaryHeight,
	)
}

func hashDKGPrivateShare(prvShare *types.DKGPrivateShare) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, prvShare.Round)

	return crypto.Keccak256Hash(
		prvShare.ProposerID.Hash[:],
		prvShare.ReceiverID.Hash[:],
		binaryRound,
		prvShare.PrivateShare.Bytes(),
	)
}

func verifyDKGPrivateShareSignature(
	prvShare *types.DKGPrivateShare) (bool, error) {
	hash := hashDKGPrivateShare(prvShare)
	pubKey, err := crypto.SigToPub(hash, prvShare.Signature)
	if err != nil {
		return false, err
	}
	if prvShare.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashDKGMasterPublicKey(mpk *types.DKGMasterPublicKey) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, mpk.Round)

	return crypto.Keccak256Hash(
		mpk.ProposerID.Hash[:],
		mpk.DKGID.GetLittleEndian(),
		mpk.PublicKeyShares.MasterKeyBytes(),
		binaryRound,
	)
}

// VerifyDKGMasterPublicKeySignature verifies DKGMasterPublicKey signature.
func VerifyDKGMasterPublicKeySignature(
	mpk *types.DKGMasterPublicKey) (bool, error) {
	hash := hashDKGMasterPublicKey(mpk)
	pubKey, err := crypto.SigToPub(hash, mpk.Signature)
	if err != nil {
		return false, err
	}
	if mpk.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashDKGComplaint(complaint *types.DKGComplaint) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, complaint.Round)

	hashPrvShare := hashDKGPrivateShare(&complaint.PrivateShare)

	return crypto.Keccak256Hash(
		complaint.ProposerID.Hash[:],
		binaryRound,
		hashPrvShare[:],
	)
}

// VerifyDKGComplaintSignature verifies DKGCompliant signature.
func VerifyDKGComplaintSignature(
	complaint *types.DKGComplaint) (bool, error) {
	if complaint.Round != complaint.PrivateShare.Round {
		return false, nil
	}
	hash := hashDKGComplaint(complaint)
	pubKey, err := crypto.SigToPub(hash, complaint.Signature)
	if err != nil {
		return false, err
	}
	if complaint.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	if !complaint.IsNack() {
		return verifyDKGPrivateShareSignature(&complaint.PrivateShare)
	}
	return true, nil
}

func hashDKGPartialSignature(psig *types.DKGPartialSignature) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, psig.Round)

	return crypto.Keccak256Hash(
		psig.ProposerID.Hash[:],
		binaryRound,
		psig.Hash[:],
		psig.PartialSignature.Signature[:],
	)
}

func verifyDKGPartialSignatureSignature(
	psig *types.DKGPartialSignature) (bool, error) {
	hash := hashDKGPartialSignature(psig)
	pubKey, err := crypto.SigToPub(hash, psig.Signature)
	if err != nil {
		return false, err
	}
	if psig.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashDKGFinalize(final *types.DKGFinalize) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, final.Round)

	return crypto.Keccak256Hash(
		final.ProposerID.Hash[:],
		binaryRound,
	)
}

// VerifyDKGFinalizeSignature verifies DKGFinalize signature.
func VerifyDKGFinalizeSignature(
	final *types.DKGFinalize) (bool, error) {
	hash := hashDKGFinalize(final)
	pubKey, err := crypto.SigToPub(hash, final.Signature)
	if err != nil {
		return false, err
	}
	if final.ProposerID != types.NewNodeID(pubKey) {
		return false, nil
	}
	return true, nil
}
