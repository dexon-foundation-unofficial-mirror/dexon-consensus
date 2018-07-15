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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// Run starts the simulation.
func Run(configPath string) {
	config, err := config.Read(configPath)
	if err != nil {
		panic(err)
	}

	networkModel := &NormalNetwork{
		Sigma:         config.Networking.Sigma,
		Mean:          config.Networking.Mean,
		LossRateValue: config.Networking.LossRateValue,
	}
	network := NewNetwork(networkModel)

	var vs []*Validator
	for i := 0; i < config.Validator.Num; i++ {
		id := types.ValidatorID(common.NewRandomHash())
		vs = append(vs, NewValidator(id, config.Validator, network, nil))
	}

	for i := 0; i < config.Validator.Num; i++ {
		vs[i].Bootstrap(vs)
	}

	for i := 0; i < config.Validator.Num; i++ {
		fmt.Printf("Validator %d: %s\n", i, vs[i].ID)
		go vs[i].Run()
	}

	select {}
}
