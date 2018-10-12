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

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// simGovernance is a simulated governance contract implementing the
// core.Governance interface.
type simGovernance struct {
	id                 types.NodeID
	lock               sync.RWMutex
	nodeSet            map[types.NodeID]crypto.PublicKey
	expectedNumNodes   int
	k                  int
	phiRatio           float32
	chainNum           uint32
	crs                []common.Hash
	tsig               map[uint64]crypto.Signature
	dkgComplaint       map[uint64][]*types.DKGComplaint
	dkgMasterPublicKey map[uint64][]*types.DKGMasterPublicKey
	dkgFinal           map[uint64]map[types.NodeID]struct{}
	lambdaBA           time.Duration
	lambdaDKG          time.Duration
	roundInterval      time.Duration
	network            *network
}

// newSimGovernance returns a new simGovernance instance.
func newSimGovernance(
	id types.NodeID,
	numNodes int, consensusConfig config.Consensus) *simGovernance {
	hashCRS := crypto.Keccak256Hash([]byte(consensusConfig.GenesisCRS))
	return &simGovernance{
		id:                 id,
		nodeSet:            make(map[types.NodeID]crypto.PublicKey),
		expectedNumNodes:   numNodes,
		k:                  consensusConfig.K,
		phiRatio:           consensusConfig.PhiRatio,
		chainNum:           consensusConfig.ChainNum,
		crs:                []common.Hash{hashCRS},
		tsig:               make(map[uint64]crypto.Signature),
		dkgComplaint:       make(map[uint64][]*types.DKGComplaint),
		dkgMasterPublicKey: make(map[uint64][]*types.DKGMasterPublicKey),
		dkgFinal:           make(map[uint64]map[types.NodeID]struct{}),
		lambdaBA: time.Duration(consensusConfig.LambdaBA) *
			time.Millisecond,
		lambdaDKG: time.Duration(consensusConfig.LambdaDKG) *
			time.Millisecond,
		roundInterval: time.Duration(consensusConfig.RoundInterval) *
			time.Millisecond,
	}
}

func (g *simGovernance) setNetwork(network *network) {
	g.network = network
}

// NodeSet returns the current notary set.
func (g *simGovernance) NodeSet(round uint64) (ret []crypto.PublicKey) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	for _, pubKey := range g.nodeSet {
		ret = append(ret, pubKey)
	}
	return
}

// Configuration returns the configuration at a given round.
func (g *simGovernance) Configuration(round uint64) *types.Config {
	return &types.Config{
		NumChains:        g.chainNum,
		LambdaBA:         g.lambdaBA,
		LambdaDKG:        g.lambdaDKG,
		K:                g.k,
		PhiRatio:         g.phiRatio,
		NotarySetSize:    uint32(len(g.nodeSet)),
		DKGSetSize:       uint32(len(g.nodeSet)),
		MinBlockInterval: g.lambdaBA * 3,
		MaxBlockInterval: g.lambdaBA * 8,
		RoundInterval:    g.roundInterval,
	}
}

// CRS returns the CRS for a given round.
func (g *simGovernance) CRS(round uint64) common.Hash {
	if round >= uint64(len(g.crs)) {
		return common.Hash{}
	}
	return g.crs[round]
}

// NotifyRoundHeight notifies governance contract to snapshot configuration
// for that round with the block on that consensus height.
func (g *simGovernance) NotifyRoundHeight(round, height uint64) {
}

// ProposeCRS proposes a CRS of round.
func (g *simGovernance) ProposeCRS(signedCRS []byte) {
	crs := crypto.Keccak256Hash(signedCRS)
	if g.crs[len(g.crs)-1].Equal(crs) {
		return
	}
	g.crs = append(g.crs, crs)
}

// addNode add a new node into the simulated governance contract.
func (g *simGovernance) addNode(pubKey crypto.PublicKey) {
	nID := types.NewNodeID(pubKey)

	g.lock.Lock()
	defer g.lock.Unlock()

	if _, exists := g.nodeSet[nID]; exists {
		return
	}
	if len(g.nodeSet) == g.expectedNumNodes {
		panic(fmt.Errorf("attempt to add node when ready"))
	}
	g.nodeSet[nID] = pubKey
}

// AddDKGComplaint adds a DKGComplaint.
func (g *simGovernance) AddDKGComplaint(
	round uint64, complaint *types.DKGComplaint) {
	if round != complaint.Round {
		return
	}
	if g.IsDKGFinal(complaint.Round) {
		return
	}
	if _, exist := g.dkgFinal[complaint.Round][complaint.ProposerID]; exist {
		return
	}
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
	round uint64, masterPublicKey *types.DKGMasterPublicKey) {
	if round != masterPublicKey.Round {
		return
	}
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

// AddDKGFinalize adds a DKG finalize message.
func (g *simGovernance) AddDKGFinalize(
	round uint64, final *types.DKGFinalize) {
	if round != final.Round {
		return
	}
	// TODO(jimmy-dexon): check if the input is valid.
	if _, exist := g.dkgFinal[final.Round]; !exist {
		g.dkgFinal[final.Round] = make(map[types.NodeID]struct{})
	}
	g.dkgFinal[final.Round][final.ProposerID] = struct{}{}
	if final.ProposerID == g.id {
		g.network.broadcast(final)
	}
}

// IsDKGFinal checks if DKG is final.
func (g *simGovernance) IsDKGFinal(round uint64) bool {
	return len(g.dkgFinal[round]) > int(g.Configuration(round).DKGSetSize)/3*2
}
