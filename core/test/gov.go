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

package test

import (
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/shopspring/decimal"
)

// Gov is an implementation of Goverance for testing purpose.
type Gov struct {
	BlockProposingInterval int
	Validators             map[types.ValidatorID]decimal.Decimal
}

// NewGov constructs a Gov instance.
func NewGov(
	validatorCount, proposingInterval int) (gov *Gov) {

	gov = &Gov{
		BlockProposingInterval: proposingInterval,
		Validators:             make(map[types.ValidatorID]decimal.Decimal),
	}
	for i := 0; i < validatorCount; i++ {
		gov.Validators[types.ValidatorID{Hash: common.NewRandomHash()}] =
			decimal.NewFromFloat(0)
	}
	return
}

// GetValidatorSet implements Governance interface to return current
// validator set.
func (gov *Gov) GetValidatorSet() map[types.ValidatorID]decimal.Decimal {
	return gov.Validators
}

// GetBlockProposingInterval implements Governance interface to return maximum
// allowed block proposing interval in millisecond.
func (gov *Gov) GetBlockProposingInterval() int {
	return gov.BlockProposingInterval
}

// GetMembershipEvents implements Governance interface to return membership
// changed events.
func (gov *Gov) GetMembershipEvents(epoch int) []types.MembershipEvent {
	return nil
}
