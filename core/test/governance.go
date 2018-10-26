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

package test

import (
	"encoding/hex"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus-core/core/types/dkg"
)

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	configs  []*types.Config
	nodeSets [][]crypto.PublicKey
	state    *State
	lock     sync.RWMutex
}

// NewGovernance constructs a Governance instance.
func NewGovernance(genesisNodes []crypto.PublicKey,
	lambda time.Duration) (g *Governance, err error) {
	// Setup a State instance.
	// TODO(mission): it's not a good idea to embed initialization of one
	//                public class in another, I did this to make the range of
	//                modification smaller.
	g = &Governance{
		state: NewState(genesisNodes, lambda, true),
	}
	return
}

// NodeSet implements Governance interface to return current
// notary set.
func (g *Governance) NodeSet(round uint64) []crypto.PublicKey {
	if round == 0 || round == 1 {
		// Round 0, 1 are genesis round, their configs should be created
		// by default.
		g.CatchUpWithRound(round)
	}
	g.lock.RLock()
	defer g.lock.RUnlock()
	if round >= uint64(len(g.nodeSets)) {
		return nil
	}
	return g.nodeSets[round]
}

// Configuration returns the configuration at a given block height.
func (g *Governance) Configuration(round uint64) *types.Config {
	if round == 0 || round == 1 {
		// Round 0, 1 are genesis round, their configs should be created
		// by default.
		g.CatchUpWithRound(round)
	}
	g.lock.RLock()
	defer g.lock.RUnlock()
	if round >= uint64(len(g.nodeSets)) {
		return nil
	}
	return g.configs[round]
}

// CRS returns the CRS for a given round.
func (g *Governance) CRS(round uint64) common.Hash {
	return g.state.CRS(round)
}

// NotifyRoundHeight notifies governace contract to snapshot config.
func (g *Governance) NotifyRoundHeight(round, height uint64) {
	g.CatchUpWithRound(round)
}

// ProposeCRS propose a CRS.
func (g *Governance) ProposeCRS(round uint64, signedCRS []byte) {
	g.lock.Lock()
	defer g.lock.Unlock()
	crs := crypto.Keccak256Hash(signedCRS)
	if err := g.state.ProposeCRS(round, crs); err != nil {
		// CRS can be proposed multiple times, other errors are not
		// accepted.
		if err != ErrDuplicatedChange {
			panic(err)
		}
	}
}

// AddDKGComplaint add a DKGComplaint.
func (g *Governance) AddDKGComplaint(
	round uint64, complaint *typesDKG.Complaint) {
	if round != complaint.Round {
		return
	}
	if g.IsDKGFinal(complaint.Round) {
		return
	}
	g.state.RequestChange(StateAddDKGComplaint, complaint)
}

// DKGComplaints returns the DKGComplaints of round.
func (g *Governance) DKGComplaints(round uint64) []*typesDKG.Complaint {
	return g.state.DKGComplaints(round)
}

// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
func (g *Governance) AddDKGMasterPublicKey(
	round uint64, masterPublicKey *typesDKG.MasterPublicKey) {
	if round != masterPublicKey.Round {
		return
	}
	g.state.RequestChange(StateAddDKGMasterPublicKey, masterPublicKey)
}

// DKGMasterPublicKeys returns the DKGMasterPublicKeys of round.
func (g *Governance) DKGMasterPublicKeys(
	round uint64) []*typesDKG.MasterPublicKey {
	return g.state.DKGMasterPublicKeys(round)
}

// AddDKGFinalize adds a DKG finalize message.
func (g *Governance) AddDKGFinalize(round uint64, final *typesDKG.Finalize) {
	if round != final.Round {
		return
	}
	g.state.RequestChange(StateAddDKGFinal, final)
}

// IsDKGFinal checks if DKG is final.
func (g *Governance) IsDKGFinal(round uint64) bool {
	if round == 0 || round == 1 {
		// Round 0, 1 are genesis round, their configs should be created
		// by default.
		g.CatchUpWithRound(round)
	}
	g.lock.RLock()
	defer g.lock.RUnlock()
	if round >= uint64(len(g.configs)) {
		return false
	}
	return g.state.IsDKGFinal(round, int(g.configs[round].DKGSetSize)/3*2)
}

//
// Test Utilities
//

// State allows to access embed State instance.
func (g *Governance) State() *State {
	return g.state
}

// CatchUpWithRound attempts to perform state snapshot to
// provide configuration/nodeSet for round R.
func (g *Governance) CatchUpWithRound(round uint64) {
	if func() bool {
		g.lock.RLock()
		defer g.lock.RUnlock()
		return uint64(len(g.configs)) > round
	}() {
		return
	}
	g.lock.Lock()
	defer g.lock.Unlock()
	for uint64(len(g.configs)) <= round {
		config, nodeSet := g.state.Snapshot()
		g.configs = append(g.configs, config)
		g.nodeSets = append(g.nodeSets, nodeSet)
	}
}

// Clone a governance instance with replicate internal state.
func (g *Governance) Clone() *Governance {
	g.lock.RLock()
	defer g.lock.RUnlock()
	// Clone state.
	copiedState := g.state.Clone()
	// Clone configs.
	copiedConfigs := []*types.Config{}
	for _, c := range g.configs {
		copiedConfigs = append(copiedConfigs, c.Clone())
	}
	// Clone node sets.
	// NOTE: here I assume the key is from ecdsa.
	copiedNodeSets := [][]crypto.PublicKey{}
	for _, nodeSetForRound := range g.nodeSets {
		copiedNodeSet := []crypto.PublicKey{}
		for _, node := range nodeSetForRound {
			pubKey, err := ecdsa.NewPublicKeyFromByteSlice(node.Bytes())
			if err != nil {
				panic(err)
			}
			copiedNodeSet = append(copiedNodeSet, pubKey)
		}
		copiedNodeSets = append(copiedNodeSets, copiedNodeSet)
	}
	return &Governance{
		configs:  copiedConfigs,
		state:    copiedState,
		nodeSets: copiedNodeSets,
	}
}

// Equal checks equality between two Governance instances.
func (g *Governance) Equal(other *Governance, checkState bool) bool {
	// Check configs.
	if !reflect.DeepEqual(g.configs, other.configs) {
		return false
	}
	// Check node sets.
	if len(g.nodeSets) != len(other.nodeSets) {
		return false
	}
	getSortedKeys := func(keys []crypto.PublicKey) (encoded []string) {
		for _, key := range keys {
			encoded = append(encoded, hex.EncodeToString(key.Bytes()))
		}
		sort.Strings(encoded)
		return
	}
	for round, nodeSetsForRound := range g.nodeSets {
		otherNodeSetsForRound := other.nodeSets[round]
		if len(nodeSetsForRound) != len(otherNodeSetsForRound) {
			return false
		}
		if !reflect.DeepEqual(
			getSortedKeys(nodeSetsForRound),
			getSortedKeys(otherNodeSetsForRound)) {
			return false
		}
	}
	// Check state if needed.
	//
	// While testing, it's expected that two governance instances contain
	// different state, only the snapshots (configs and node sets) are
	// essentially equal.
	if checkState {
		return g.state.Equal(other.state) == nil
	}
	return true
}
