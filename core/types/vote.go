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
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
)

// VoteType is the type of vote.
type VoteType byte

// VoteType enum.
const (
	VoteInit VoteType = iota
	VotePreCom
	VoteCom
	// Do not add any type below MaxVoteType.
	MaxVoteType
)

// Vote is the vote structure defined in Crypto Shuffle Algorithm.
type Vote struct {
	ProposerID NodeID           `json:"proposer_id"`
	Type       VoteType         `json:"type"`
	BlockHash  common.Hash      `json:"block_hash"`
	Period     uint64           `json:"period"`
	Position   Position         `json:"position"`
	Signature  crypto.Signature `json:"signature"`
}

func (v *Vote) String() string {
	return fmt.Sprintf("Vote[%s:%d:%d](%d:%d):%s",
		v.ProposerID.String()[:6], v.Position.ChainID, v.Position.Height,
		v.Period, v.Type, v.BlockHash.String()[:6])
}

// Clone returns a deep copy of a vote.
func (v *Vote) Clone() *Vote {
	return &Vote{
		ProposerID: v.ProposerID,
		Type:       v.Type,
		BlockHash:  v.BlockHash,
		Period:     v.Period,
		Position:   v.Position,
		Signature:  v.Signature.Clone(),
	}
}
