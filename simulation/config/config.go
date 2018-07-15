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

package config

import (
	"os"

	"github.com/naoina/toml"
)

// Validator config for the simulation.
type Validator struct {
	Num                  int
	ProposeIntervalMean  float64
	ProposeIntervalSigma float64
}

// Networking config.
type Networking struct {
	Mean          float64
	Sigma         float64
	LossRateValue float64
}

// Config represents the configuration for simulation.
type Config struct {
	Title      string
	Validator  Validator
	Networking Networking
}

// GenerateDefault generates a default configuration file.
func GenerateDefault(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	config := Config{
		Title: "DEXON Consensus Simulation Config",
		Validator: Validator{
			Num:                  4,
			ProposeIntervalMean:  500,
			ProposeIntervalSigma: 30,
		},
		Networking: Networking{
			Mean:          100,
			Sigma:         30,
			LossRateValue: 0,
		},
	}

	if err := toml.NewEncoder(f).Encode(&config); err != nil {
		return err
	}
	return nil
}

// Read reads the config from a file.
func Read(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var config Config

	if toml.NewDecoder(f).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
