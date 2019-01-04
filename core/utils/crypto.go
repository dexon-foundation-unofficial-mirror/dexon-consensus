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
	"encoding/binary"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

func hashWitness(witness *types.Witness) (common.Hash, error) {
	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, witness.Height)
	return crypto.Keccak256Hash(
		binaryHeight,
		witness.Data), nil
}

// HashBlock generates hash of a types.Block.
func HashBlock(block *types.Block) (common.Hash, error) {
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

	hash := crypto.Keccak256Hash(
		block.ProposerID.Hash[:],
		block.ParentHash[:],
		hashPosition[:],
		hashAcks[:],
		binaryTimestamp[:],
		block.PayloadHash[:],
		binaryWitness[:])
	return hash, nil
}

// VerifyBlockSignature verifies the signature of types.Block.
func VerifyBlockSignature(b *types.Block) (err error) {
	payloadHash := crypto.Keccak256Hash(b.Payload)
	if payloadHash != b.PayloadHash {
		err = ErrIncorrectHash
		return
	}
	hash, err := HashBlock(b)
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

// HashVote generates hash of a types.Vote.
func HashVote(vote *types.Vote) common.Hash {
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

// VerifyVoteSignature verifies the signature of types.Vote.
func VerifyVoteSignature(vote *types.Vote) (bool, error) {
	hash := HashVote(vote)
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

// VerifyCRSSignature verifies the CRS signature of types.Block.
func VerifyCRSSignature(block *types.Block, crs common.Hash) (
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

	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, position.Round)

	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, position.Height)

	return crypto.Keccak256Hash(
		binaryChainID,
		binaryRound,
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

// VerifyDKGPrivateShareSignature verifies the signature of
// typesDKG.PrivateShare.
func VerifyDKGPrivateShareSignature(
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
		return VerifyDKGPrivateShareSignature(&complaint.PrivateShare)
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

// VerifyDKGPartialSignatureSignature verifies the signature of
// typesDKG.PartialSignature.
func VerifyDKGPartialSignatureSignature(
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

func hashDKGMPKReady(ready *typesDKG.MPKReady) common.Hash {
	binaryRound := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryRound, ready.Round)

	return crypto.Keccak256Hash(
		ready.ProposerID.Hash[:],
		binaryRound,
	)
}

// VerifyDKGMPKReadySignature verifies DKGMPKReady signature.
func VerifyDKGMPKReadySignature(
	ready *typesDKG.MPKReady) (bool, error) {
	hash := hashDKGMPKReady(ready)
	pubKey, err := crypto.SigToPub(hash, ready.Signature)
	if err != nil {
		return false, err
	}
	if ready.ProposerID != types.NewNodeID(pubKey) {
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
