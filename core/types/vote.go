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

package types

import (
	"fmt"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
)

// VoteType is the type of vote.
type VoteType byte

// VoteType enum.
const (
	VoteInit VoteType = iota
	VotePreCom
	VoteCom
	VoteFast
	// Do not add any type below MaxVoteType.
	MaxVoteType
)

// VoteHeader is the header for vote, which can be used as map keys.
type VoteHeader struct {
	ProposerID NodeID      `json:"proposer_id"`
	Type       VoteType    `json:"type"`
	BlockHash  common.Hash `json:"block_hash"`
	Period     uint64      `json:"period"`
	Position   Position    `json:"position"`
}

// Vote is the vote structure defined in Crypto Shuffle Algorithm.
type Vote struct {
	VoteHeader `json:"header"`
	Signature  crypto.Signature `json:"signature"`
}

func (v *Vote) String() string {
	return fmt.Sprintf("Vote{BP:%s %s Period:%d Type:%d Hash:%s}",
		v.ProposerID.String()[:6],
		&v.Position, v.Period, v.Type, v.BlockHash.String()[:6])
}

// NewVote constructs a Vote instance with header fields.
func NewVote(t VoteType, hash common.Hash, period uint64) *Vote {
	return &Vote{
		VoteHeader: VoteHeader{
			Type:      t,
			BlockHash: hash,
			Period:    period,
		}}
}

// Clone returns a deep copy of a vote.
func (v *Vote) Clone() *Vote {
	return &Vote{
		VoteHeader: VoteHeader{
			ProposerID: v.ProposerID,
			Type:       v.Type,
			BlockHash:  v.BlockHash,
			Period:     v.Period,
			Position:   v.Position,
		},
		Signature: v.Signature.Clone(),
	}
}
