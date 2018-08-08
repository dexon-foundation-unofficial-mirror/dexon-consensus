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
