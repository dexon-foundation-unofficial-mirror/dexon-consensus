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

package config

import (
	"fmt"
	"math"
	"os"

	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/naoina/toml"
)

// Consensus settings.
type Consensus struct {
	PhiRatio         float32
	K                int
	NumChains        uint32
	GenesisCRS       string `toml:"genesis_crs"`
	LambdaBA         int    `toml:"lambda_ba"`
	LambdaDKG        int    `toml:"lambda_dkg"`
	RoundInterval    int
	NotarySetSize    uint32
	DKGSetSize       uint32 `toml:"dkg_set_size"`
	MinBlockInterval int
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
	Num       uint32
	MaxBlock  uint64
	Changes   []Change
}

// LatencyModel for ths simulation.
type LatencyModel struct {
	Mean  float64
	Sigma float64
}

// Networking config.
type Networking struct {
	Type       test.NetworkType
	PeerServer string
	Direct     LatencyModel
	Gossip     LatencyModel
}

// Scheduler Settings.
type Scheduler struct {
	WorkerNum int
}

// Change represent future configuration changes.
type Change struct {
	Round uint64
	Type  string
	Value string
}

// RegisterChange reigster this change to a test.Governance instance.
func (c Change) RegisterChange(gov *test.Governance) error {
	if c.Round < core.ConfigRoundShift {
		panic(fmt.Errorf(
			"attempt to register config change that never be executed: %v",
			c.Round))
	}
	t := StateChangeTypeFromString(c.Type)
	return gov.RegisterConfigChange(
		c.Round, t, StateChangeValueFromString(t, c.Value))
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
				PhiRatio:         float32(2) / 3,
				K:                0,
				NumChains:        7,
				GenesisCRS:       "In DEXON we trust.",
				LambdaBA:         250,
				LambdaDKG:        1000,
				RoundInterval:    30 * 1000,
				NotarySetSize:    7,
				DKGSetSize:       7,
				MinBlockInterval: 750,
			},
			Legacy: Legacy{
				ProposeIntervalMean:  500,
				ProposeIntervalSigma: 50,
			},
			Num:      7,
			MaxBlock: math.MaxUint64,
		},
		Networking: Networking{
			Type:       test.NetworkTypeTCPLocal,
			PeerServer: "127.0.0.1",
			Direct: LatencyModel{
				Mean:  100,
				Sigma: 10,
			},
			Gossip: LatencyModel{
				Mean:  300,
				Sigma: 25,
			},
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
