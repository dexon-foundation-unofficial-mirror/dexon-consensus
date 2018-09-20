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
	"math"
	"os"

	"github.com/naoina/toml"
)

// NetworkType is the simulation network type.
type NetworkType string

// NetworkType enums.
const (
	NetworkTypeTCP      NetworkType = "tcp"
	NetworkTypeTCPLocal NetworkType = "tcp-local"
	NetworkTypeFake     NetworkType = "fake"
)

// Consensus settings.
type Consensus struct {
	PhiRatio   float32
	K          int
	ChainNum   uint32
	GenesisCRS string `toml:"genesis_crs"`
	Lambda     int
}

// Legacy config.
type Legacy struct {
	ProposeIntervalMean  float64
	ProposeIntervalSigma float64
}

// Node config for the simulation.
type Node struct {
	Consensus Consensus
	Legacy    Legacy
	Num       int
	MaxBlock  uint64
}

// Networking config.
type Networking struct {
	Type       NetworkType
	PeerServer string

	Mean          float64
	Sigma         float64
	LossRateValue float64
}

// Scheduler Settings.
type Scheduler struct {
	WorkerNum int
}

// Config represents the configuration for simulation.
type Config struct {
	Title      string
	Node       Node
	Networking Networking
	Scheduler  Scheduler
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
		Node: Node{
			Consensus: Consensus{
				PhiRatio:   float32(2) / 3,
				K:          1,
				ChainNum:   7,
				GenesisCRS: "In DEXON we trust.",
				Lambda:     250,
			},
			Legacy: Legacy{
				ProposeIntervalMean:  500,
				ProposeIntervalSigma: 50,
			},
			Num:      7,
			MaxBlock: math.MaxUint64,
		},
		Networking: Networking{
			Type:          NetworkTypeTCPLocal,
			PeerServer:    "127.0.0.1",
			Mean:          100,
			Sigma:         10,
			LossRateValue: 0,
		},
		Scheduler: Scheduler{
			WorkerNum: 2,
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

	if err := toml.NewDecoder(f).Decode(&config); err != nil {
		return nil, err
	}
	return &config, nil
}
