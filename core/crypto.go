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
	"sort"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

func hashNotary(block *types.Block) (common.Hash, error) {
	binaryTime, err := block.Notary.Timestamp.MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, block.Notary.Height)
	hash := crypto.Keccak256Hash(
		block.Notary.ParentHash[:],
		binaryTime,
		binaryHeight)
	return hash, nil
}

func verifyNotarySignature(pubkey crypto.PublicKey,
	notaryBlock *types.Block, sig crypto.Signature) (bool, error) {
	hash, err := hashNotary(notaryBlock)
	if err != nil {
		return false, err
	}
	return pubkey.VerifySignature(hash, sig), nil
}

func hashBlock(block *types.Block) (common.Hash, error) {
	hashPosition := hashPosition(block.Position)
	// Handling Block.Acks.
	acks := make(common.Hashes, 0, len(block.Acks))
	for ack := range block.Acks {
		acks = append(acks, ack)
	}
	sort.Sort(acks)
	binaryAcks := make([][]byte, len(block.Acks))
	for idx := range acks {
		binaryAcks[idx] = acks[idx][:]
	}
	hashAcks := crypto.Keccak256Hash(binaryAcks...)
	binaryTimestamp, err := block.Timestamp.MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	payloadHash := crypto.Keccak256Hash(block.Payloads...)

	hash := crypto.Keccak256Hash(
		block.ProposerID.Hash[:],
		block.ParentHash[:],
		hashPosition[:],
		hashAcks[:],
		binaryTimestamp[:],
		payloadHash[:])
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

func verifyVoteSignature(vote *types.Vote, sigToPub SigToPubFn) (bool, error) {
	hash := hashVote(vote)
	pubKey, err := sigToPub(hash, vote.Signature)
	if err != nil {
		return false, err
	}
	if vote.ProposerID != types.NewValidatorID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashCRS(block *types.Block, crs common.Hash) common.Hash {
	hashPos := hashPosition(block.Position)
	return crypto.Keccak256Hash(crs[:], hashPos[:])
}

func verifyCRSSignature(block *types.Block, crs common.Hash, sigToPub SigToPubFn) (
	bool, error) {
	hash := hashCRS(block, crs)
	pubKey, err := sigToPub(hash, block.CRSSignature)
	if err != nil {
		return false, err
	}
	if block.ProposerID != types.NewValidatorID(pubKey) {
		return false, nil
	}
	return true, nil
}

func hashPosition(position types.Position) common.Hash {
	binaryShardID := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryShardID, position.ShardID)

	binaryChainID := make([]byte, 4)
	binary.LittleEndian.PutUint32(binaryChainID, position.ChainID)

	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, position.Height)

	return crypto.Keccak256Hash(
		binaryShardID,
		binaryChainID,
		binaryHeight,
	)
}
