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
	"github.com/shopspring/decimal"
)

type simGov struct {
	lock                  sync.RWMutex
	validatorSet          map[types.ValidatorID]decimal.Decimal
	expectedNumValidators int
}

func newSimGov(numValidators int) *simGov {
	return &simGov{
		validatorSet:          make(map[types.ValidatorID]decimal.Decimal),
		expectedNumValidators: numValidators,
	}
}

func (gov *simGov) GetValidatorSet() (
	ret map[types.ValidatorID]decimal.Decimal) {

	gov.lock.RLock()
	defer gov.lock.RUnlock()

	// Return the cloned validatorSet.
	ret = make(map[types.ValidatorID]decimal.Decimal)
	for k, v := range gov.validatorSet {
		ret[k] = v
	}
	return
}

func (gov *simGov) GetBlockProposingInterval() int {
	return 0
}

func (gov *simGov) GetMembershipEvents(epoch int) []types.MembershipEvent {
	return nil
}

func (gov *simGov) addValidator(vID types.ValidatorID) {
	gov.lock.Lock()
	defer gov.lock.Unlock()

	if _, exists := gov.validatorSet[vID]; exists {
		return
	}
	if len(gov.validatorSet) == gov.expectedNumValidators {
		panic(fmt.Errorf("attempt to add validator when ready"))
	}
	gov.validatorSet[vID] = decimal.NewFromFloat(0)
}
