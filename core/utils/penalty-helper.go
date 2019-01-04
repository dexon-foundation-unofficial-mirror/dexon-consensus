// Copyright 2019 The dexon-consensus Authors
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

	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

var (
	// ErrInvalidDKGMasterPublicKey means the DKG MasterPublicKey is invalid.
	ErrInvalidDKGMasterPublicKey = errors.New("invalid DKG master public key")
)

// NeedPenaltyDKGPrivateShare checks if the proposer of dkg private share
// should be penalized.
func NeedPenaltyDKGPrivateShare(
	complaint *typesDKG.Complaint, mpk *typesDKG.MasterPublicKey) (bool, error) {
	if complaint.IsNack() {
		return false, nil
	}
	if mpk.ProposerID != complaint.PrivateShare.ProposerID {
		return false, nil
	}
	ok, err := VerifyDKGMasterPublicKeySignature(mpk)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, ErrInvalidDKGMasterPublicKey
	}
	ok, err = VerifyDKGComplaintSignature(complaint)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	ok, err = mpk.PublicKeyShares.VerifyPrvShare(
		typesDKG.NewID(complaint.PrivateShare.ReceiverID),
		&complaint.PrivateShare.PrivateShare)
	if err != nil {
		return false, err
	}
	return !ok, nil
}

// NeedPenaltyForkVote checks if two votes are fork vote.
func NeedPenaltyForkVote(vote1, vote2 *types.Vote) (bool, error) {
	if vote1.ProposerID != vote2.ProposerID ||
		vote1.Type != vote2.Type ||
		vote1.Period != vote2.Period ||
		vote1.Position != vote2.Position ||
		vote1.BlockHash == vote2.BlockHash {
		return false, nil
	}
	ok, err := VerifyVoteSignature(vote1)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	ok, err = VerifyVoteSignature(vote2)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return true, nil
}

// NeedPenaltyForkBlock checks if two blocks are fork block.
func NeedPenaltyForkBlock(block1, block2 *types.Block) (bool, error) {
	if block1.ProposerID != block2.ProposerID ||
		block1.Position != block2.Position ||
		block1.Hash == block2.Hash {
		return false, nil
	}
	verifyBlock := func(block *types.Block) (bool, error) {
		err := VerifyBlockSignature(block)
		switch err {
		case nil:
			return true, nil
		case ErrIncorrectSignature:
			return false, nil
		case ErrIncorrectHash:
			return false, nil
		default:
			return false, err
		}
	}
	ok, err := verifyBlock(block1)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	ok, err = verifyBlock(block2)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}
	return true, nil
}
