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

func hashCompactionChainAck(block *types.Block) (common.Hash, error) {
	binaryTime, err := block.ConsensusInfo.Timestamp.MarshalBinary()
	if err != nil {
		return common.Hash{}, err
	}
	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, block.ConsensusInfo.Height)
	hash := crypto.Keccak256Hash(
		block.ConsensusInfoParentHash[:],
		binaryTime,
		binaryHeight)
	return hash, nil
}

func signCompactionChainAck(block *types.Block,
	prv crypto.PrivateKey) (crypto.Signature, error) {
	hash, err := hashCompactionChainAck(block)
	if err != nil {
		return crypto.Signature{}, err
	}
	return prv.Sign(hash)
}

func verifyCompactionChainAckSignature(pubkey crypto.PublicKey,
	ackingBlock *types.Block, sig crypto.Signature) (bool, error) {
	hash, err := hashCompactionChainAck(ackingBlock)
	if err != nil {
		return false, err
	}
	return pubkey.VerifySignature(hash, sig), nil
}

func hashBlock(blockConv types.BlockConverter) (common.Hash, error) {
	block := blockConv.Block()
	binaryHeight := make([]byte, 8)
	binary.LittleEndian.PutUint64(binaryHeight, block.Height)
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
	// Handle Block.Timestamps.
	// TODO(jimmy-dexon): Store and get the sorted validatorIDs.
	validators := make(types.ValidatorIDs, 0, len(block.Timestamps))
	for vID := range block.Timestamps {
		validators = append(validators, vID)
	}
	sort.Sort(validators)
	binaryTimestamps := make([][]byte, len(block.Timestamps))
	for idx, vID := range validators {
		var err error
		binaryTimestamps[idx], err = block.Timestamps[vID].MarshalBinary()
		if err != nil {
			return common.Hash{}, err
		}
	}
	hashTimestamps := crypto.Keccak256Hash(binaryTimestamps...)
	payloadHash := crypto.Keccak256Hash(blockConv.Payloads()...)

	hash := crypto.Keccak256Hash(
		block.ProposerID.Hash[:],
		block.ParentHash[:],
		binaryHeight,
		hashAcks[:],
		hashTimestamps[:],
		payloadHash[:])
	return hash, nil
}

func signBlock(blockConv types.BlockConverter,
	prv crypto.PrivateKey) (crypto.Signature, error) {
	hash, err := hashBlock(blockConv)
	if err != nil {
		return crypto.Signature{}, err
	}
	return prv.Sign(hash)
}

func verifyBlockSignature(pubkey crypto.PublicKey,
	blockConv types.BlockConverter, sig crypto.Signature) (bool, error) {
	hash, err := hashBlock(blockConv)
	if err != nil {
		return false, err
	}
	return pubkey.VerifySignature(hash, sig), nil
}
