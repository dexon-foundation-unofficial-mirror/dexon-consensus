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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// simGovernance is a simulated governance contract implementing the
// core.Governance interface.
type simGovernance struct {
	id                 types.NodeID
	lock               sync.RWMutex
	notarySet          map[types.NodeID]struct{}
	expectedNumNodes   int
	k                  int
	phiRatio           float32
	chainNum           uint32
	crs                []byte
	tsig               map[uint64]crypto.Signature
	dkgComplaint       map[uint64][]*types.DKGComplaint
	dkgMasterPublicKey map[uint64][]*types.DKGMasterPublicKey
	lambdaBA           time.Duration
	lambdaDKG          time.Duration
	network            *network
}

// newSimGovernance returns a new simGovernance instance.
func newSimGovernance(
	id types.NodeID,
	numNodes int, consensusConfig config.Consensus) *simGovernance {
	return &simGovernance{
		id:                 id,
		notarySet:          make(map[types.NodeID]struct{}),
		expectedNumNodes:   numNodes,
		k:                  consensusConfig.K,
		phiRatio:           consensusConfig.PhiRatio,
		chainNum:           consensusConfig.ChainNum,
		crs:                []byte(consensusConfig.GenesisCRS),
		tsig:               make(map[uint64]crypto.Signature),
		dkgComplaint:       make(map[uint64][]*types.DKGComplaint),
		dkgMasterPublicKey: make(map[uint64][]*types.DKGMasterPublicKey),
		lambdaBA:           time.Duration(consensusConfig.LambdaBA) * time.Millisecond,
		lambdaDKG:          time.Duration(consensusConfig.LambdaDKG) * time.Millisecond,
	}
}

func (g *simGovernance) setNetwork(network *network) {
	g.network = network
}

// GetNodeSet returns the current notary set.
func (g *simGovernance) GetNodeSet(
	blockHeight uint64) map[types.NodeID]struct{} {
	g.lock.RLock()
	defer g.lock.RUnlock()

	// Return the cloned notarySet.
	ret := make(map[types.NodeID]struct{})
	for k := range g.notarySet {
		ret[k] = struct{}{}
	}
	return ret
}

// GetConfiguration returns the configuration at a given round.
func (g *simGovernance) GetConfiguration(round uint64) *types.Config {
	return &types.Config{
		NumShards: 1,
		NumChains: g.chainNum,
		LambdaBA:  g.lambdaBA,
		LambdaDKG: g.lambdaDKG,
		K:         g.k,
		PhiRatio:  g.phiRatio,
	}
}

// GetCRS returns the CRS for a given round.
func (g *simGovernance) GetCRS(round uint64) []byte {
	return g.crs
}

// addNode add a new node into the simulated governance contract.
func (g *simGovernance) addNode(nID types.NodeID) {
	g.lock.Lock()
	defer g.lock.Unlock()

	if _, exists := g.notarySet[nID]; exists {
		return
	}
	if len(g.notarySet) == g.expectedNumNodes {
		panic(fmt.Errorf("attempt to add node when ready"))
	}
	g.notarySet[nID] = struct{}{}
}

// ProposeThresholdSignature porposes a ThresholdSignature of round.
func (g *simGovernance) ProposeThresholdSignature(
	round uint64, signature crypto.Signature) {
	g.tsig[round] = signature
}

// GetThresholdSignature gets a ThresholdSignature of round.
func (g *simGovernance) GetThresholdSignature(round uint64) (
	sig crypto.Signature, exist bool) {
	sig, exist = g.tsig[round]
	return
}

// AddDKGComplaint adds a DKGComplaint.
func (g *simGovernance) AddDKGComplaint(complaint *types.DKGComplaint) {
	// TODO(jimmy-dexon): check if the input is valid.
	g.dkgComplaint[complaint.Round] = append(
		g.dkgComplaint[complaint.Round], complaint)
	if complaint.ProposerID == g.id {
		g.network.broadcast(complaint)
	}
}

// DKGComplaints returns the DKGComplaints of round.
func (g *simGovernance) DKGComplaints(round uint64) []*types.DKGComplaint {
	complaints, exist := g.dkgComplaint[round]
	if !exist {
		return []*types.DKGComplaint{}
	}
	return complaints
}

// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
func (g *simGovernance) AddDKGMasterPublicKey(
	masterPublicKey *types.DKGMasterPublicKey) {
	// TODO(jimmy-dexon): check if the input is valid.
	g.dkgMasterPublicKey[masterPublicKey.Round] = append(
		g.dkgMasterPublicKey[masterPublicKey.Round], masterPublicKey)
	if masterPublicKey.ProposerID == g.id {
		g.network.broadcast(masterPublicKey)
	}
}

// DKGMasterPublicKeys returns the DKGMasterPublicKeys of round.
func (g *simGovernance) DKGMasterPublicKeys(
	round uint64) []*types.DKGMasterPublicKey {
	masterPublicKeys, exist := g.dkgMasterPublicKey[round]
	if !exist {
		return []*types.DKGMasterPublicKey{}
	}
	return masterPublicKeys
}
