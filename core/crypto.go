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
	typesDKG "github.com/dexon-foundation/dexon-consensus-core/core/types/dkg"
)

func hashWitness(witness *types.Witness) (common.Hash, error) {
	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, witness.Height)
	return crypto.Keccak256Hash(
		binaryHeight,
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

func hashDKGPrivateShare(prvShare *typesDKG.PrivateShare) common.Hash {
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
	prvShare *typesDKG.PrivateShare) (bool, error) {
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

func hashDKGMasterPublicKey(mpk *typesDKG.MasterPublicKey) common.Hash {
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
	mpk *typesDKG.MasterPublicKey) (bool, error) {
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

func hashDKGComplaint(complaint *typesDKG.Complaint) common.Hash {
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
	complaint *typesDKG.Complaint) (bool, error) {
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

func hashDKGPartialSignature(psig *typesDKG.PartialSignature) common.Hash {
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
	psig *typesDKG.PartialSignature) (bool, error) {
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

func hashDKGFinalize(final *typesDKG.Finalize) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, final.Round)

	return crypto.Keccak256Hash(
		final.ProposerID.Hash[:],
		binaryRound,
	)
}

// VerifyDKGFinalizeSignature verifies DKGFinalize signature.
func VerifyDKGFinalizeSignature(
	final *typesDKG.Finalize) (bool, error) {
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
