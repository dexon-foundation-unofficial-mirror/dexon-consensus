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
	cfg, err := config.Read(configPath)
	if err != nil {
		panic(err)
	}

	networkType := cfg.Networking.Type

	var vs []*Validator

	if networkType == config.NetworkTypeFake ||
		networkType == config.NetworkTypeTCPLocal {

		var network Network

		if networkType == config.NetworkTypeFake {
			networkModel := &NormalNetwork{
				Sigma:         cfg.Networking.Sigma,
				Mean:          cfg.Networking.Mean,
				LossRateValue: cfg.Networking.LossRateValue,
			}
			network = NewFakeNetwork(networkModel)

			for i := 0; i < cfg.Validator.Num; i++ {
				id := types.ValidatorID{Hash: common.NewRandomHash()}
				vs = append(vs, NewValidator(id, cfg.Validator, network))
			}
		} else if networkType == config.NetworkTypeTCPLocal {
			for i := 0; i < cfg.Validator.Num; i++ {
				id := types.ValidatorID{Hash: common.NewRandomHash()}
				network := NewTCPNetwork(true, cfg.Networking.PeerServer)
				go network.Start()
				vs = append(vs, NewValidator(id, cfg.Validator, network))
			}
		}

		for i := 0; i < cfg.Validator.Num; i++ {
			fmt.Printf("Validator %d: %s\n", i, vs[i].ID)
			go vs[i].Run()
		}
	} else if networkType == config.NetworkTypeTCP {
		id := types.ValidatorID{Hash: common.NewRandomHash()}
		network := NewTCPNetwork(false, cfg.Networking.PeerServer)
		go network.Start()
		v := NewValidator(id, cfg.Validator, network)
		go v.Run()
		vs = append(vs, v)
	}

	for _, v := range vs {
		v.Wait()
		fmt.Printf("Validator %s is shutdown\n", v.GetID())
	}
}
