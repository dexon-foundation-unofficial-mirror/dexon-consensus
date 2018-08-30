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

package simulation

import (
	"fmt"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
	"github.com/shopspring/decimal"
)

// simGovernance is a simulated governance contract implementing the
// core.Governance interface.
type simGovernance struct {
	lock                  sync.RWMutex
	validatorSet          map[types.ValidatorID]decimal.Decimal
	expectedNumValidators int
	k                     int
	phiRatio              float32
	crs                   string
	agreementK            int
}

// newSimGovernance returns a new simGovernance instance.
func newSimGovernance(
	numValidators int, consensusConfig config.Consensus) *simGovernance {
	return &simGovernance{
		validatorSet:          make(map[types.ValidatorID]decimal.Decimal),
		expectedNumValidators: numValidators,
		k:          consensusConfig.K,
		phiRatio:   consensusConfig.PhiRatio,
		crs:        consensusConfig.Agreement.GenesisCRS,
		agreementK: consensusConfig.Agreement.K,
	}
}

// GetValidatorSet returns the validator set.
func (g *simGovernance) GetValidatorSet() (
	ret map[types.ValidatorID]decimal.Decimal) {

	g.lock.RLock()
	defer g.lock.RUnlock()

	// Return the cloned validatorSet.
	ret = make(map[types.ValidatorID]decimal.Decimal)
	for k, v := range g.validatorSet {
		ret[k] = v
	}
	return
}

// GetTotalOrderingK returns K.
func (g *simGovernance) GetTotalOrderingK() int {
	return g.k
}

// GetTotalOrderingK returns PhiRatio.
func (g *simGovernance) GetPhiRatio() float32 {
	return g.phiRatio
}

// GetBlockProposingInterval returns block proposing interval.
func (g *simGovernance) GetBlockProposingInterval() int {
	return 0
}

// GetConfigurationChangeEvent returns configuration change event since last
// epoch.
func (g *simGovernance) GetConfigurationChangeEvent(
	epoch int) []types.ConfigurationChangeEvent {
	return nil
}

// GetGenesisCRS returns CRS.
func (g *simGovernance) GetGenesisCRS() string {
	return g.crs
}

// GetAgreementK returns K for agreement.
func (g *simGovernance) GetAgreementK() int {
	return g.agreementK
}

// addValidator add a new validator into the simulated governance contract.
func (g *simGovernance) addValidator(vID types.ValidatorID) {
	g.lock.Lock()
	defer g.lock.Unlock()

	if _, exists := g.validatorSet[vID]; exists {
		return
	}
	if len(g.validatorSet) == g.expectedNumValidators {
		panic(fmt.Errorf("attempt to add validator when ready"))
	}
	g.validatorSet[vID] = decimal.NewFromFloat(0)
}
