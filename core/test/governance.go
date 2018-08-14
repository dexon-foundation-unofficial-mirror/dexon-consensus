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
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/shopspring/decimal"
)

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	BlockProposingInterval int
	Validators             map[types.ValidatorID]decimal.Decimal
	PrivateKeys            map[types.ValidatorID]crypto.PrivateKey
}

// NewGovernance constructs a Governance instance.
func NewGovernance(validatorCount, proposingInterval int) (
	g *Governance, err error) {
	g = &Governance{
		BlockProposingInterval: proposingInterval,
		Validators:             make(map[types.ValidatorID]decimal.Decimal),
		PrivateKeys:            make(map[types.ValidatorID]crypto.PrivateKey),
	}
	for i := 0; i < validatorCount; i++ {
		prv, err := eth.NewPrivateKey()
		if err != nil {
			return nil, err
		}
		vID := types.NewValidatorID(prv.PublicKey())
		g.Validators[vID] = decimal.NewFromFloat(0)
		g.PrivateKeys[vID] = prv
	}
	return
}

// GetValidatorSet implements Governance interface to return current
// validator set.
func (g *Governance) GetValidatorSet() map[types.ValidatorID]decimal.Decimal {
	return g.Validators
}

// GetBlockProposingInterval implements Governance interface to return maximum
// allowed block proposing interval in millisecond.
func (g *Governance) GetBlockProposingInterval() int {
	return g.BlockProposingInterval
}

// GetTotalOrderingK returns K.
func (g *Governance) GetTotalOrderingK() int {
	return 0
}

// GetPhiRatio returns phi ratio.
func (g *Governance) GetPhiRatio() float32 {
	return 0.667
}

// GetConfigurationChangeEvent Get configuration change events after a certain
// epoch.
func (g *Governance) GetConfigurationChangeEvent(
	epoch int) []types.ConfigurationChangeEvent {
	return nil
}
