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
	"github.com/shopspring/decimal"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Governance interface specifies interface to control the governance contract.
// Note that there are a lot more methods in the governance contract, that this
// interface only define those that are required to run the consensus algorithm.
type Governance interface {
	// Get the current validator set and it's corresponding stake.
	GetValidatorSet() map[types.ValidatorID]decimal.Decimal

	// Get K
	GetTotalOrderingK() int

	// Get PhiRatio
	GetPhiRatio() float32

	// Get block proposing interval (in milliseconds).
	GetBlockProposingInterval() int

	// Get configuration change events after a certain epoch.
	GetConfigurationChangeEvent(epoch int) []types.ConfigurationChangeEvent

	// Get Genesis CRS.
	GetGenesisCRS() string
	// GetAgreementK returns number of blocks for a CRS.
	GetAgreementK() int
}
