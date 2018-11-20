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
)

// AgreementResult describes an agremeent result.
type AgreementResult struct {
	BlockHash    common.Hash `json:"block_hash"`
	Position     Position    `json:"position"`
	Votes        []Vote      `json:"votes"`
	IsEmptyBlock bool        `json:"is_empty_block"`
}

func (r *AgreementResult) String() string {
	return fmt.Sprintf("agreementResult{Hash:%s %s}",
		r.BlockHash.String()[:6], &r.Position)
}

// BlockRandomnessResult describes a block randomness result
type BlockRandomnessResult struct {
	BlockHash  common.Hash `json:"block_hash"`
	Position   Position    `json:"position"`
	Randomness []byte      `json:"randomness"`
}
