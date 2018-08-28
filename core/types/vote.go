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

package types

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
)

// VoteType is the type of vote.
type VoteType byte

// VoteType enum.
const (
	VoteAck VoteType = iota
	VoteConfirm
	VotePass
	// Do not add any type below MaxVoteType.
	MaxVoteType
)

// Vote is the vote structure defined in Crypto Shuffle Algorithm.
type Vote struct {
	ProposerID ValidatorID
	Type       VoteType
	BlockHash  common.Hash
	Period     uint64
	Signature  crypto.Signature
}

func (v *Vote) String() string {
	return fmt.Sprintf("Vote(%d:%d):%s",
		v.Period, v.Type, v.BlockHash.String()[:6])
}

// Clone returns a deep copy of a vote.
func (v *Vote) Clone() *Vote {
	return &Vote{
		ProposerID: v.ProposerID,
		Type:       v.Type,
		BlockHash:  v.BlockHash,
		Period:     v.Period,
		Signature:  v.Signature.Clone(),
	}
}
