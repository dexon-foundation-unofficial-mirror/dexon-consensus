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

package test

import (
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"sync"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

// TODO(mission): add a method to compare config/crs between governance
//                instances.

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	roundShift           uint64
	configs              []*types.Config
	nodeSets             [][]crypto.PublicKey
	stateModule          *State
	networkModule        *Network
	pendingConfigChanges map[uint64]map[StateChangeType]interface{}
	lock                 sync.RWMutex
}

// NewGovernance constructs a Governance instance.
func NewGovernance(state *State, roundShift uint64) (g *Governance, err error) {
	// Setup a State instance.
	g = &Governance{
		roundShift:           roundShift,
		pendingConfigChanges: make(map[uint64]map[StateChangeType]interface{}),
		stateModule:          state,
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
	if round >= uint64(len(g.configs)) {
		return nil
	}
	return g.configs[round]
}

// CRS returns the CRS for a given round.
func (g *Governance) CRS(round uint64) common.Hash {
	return g.stateModule.CRS(round)
}

// NotifyRoundHeight notifies governace contract to snapshot config.
func (g *Governance) NotifyRoundHeight(round, height uint64) {
	// Snapshot configuration for the shifted round, this behavior is synced with
	// full node's implementation.
	shiftedRound := round + g.roundShift
	g.CatchUpWithRound(shiftedRound)
	// Apply change request for next round.
	func() {
		g.lock.Lock()
		defer g.lock.Unlock()
		// Check if there is any pending changes for previous rounds.
		for r := range g.pendingConfigChanges {
			if r < shiftedRound+1 {
				panic(fmt.Errorf("pending change no longer applied: %v, now: %v",
					r, shiftedRound+1))
			}
		}
		for t, v := range g.pendingConfigChanges[shiftedRound+1] {
			if err := g.stateModule.RequestChange(t, v); err != nil {
				panic(err)
			}
		}
		delete(g.pendingConfigChanges, shiftedRound+1)
		g.broadcastPendingStateChanges()
	}()
}

// ProposeCRS propose a CRS.
func (g *Governance) ProposeCRS(round uint64, signedCRS []byte) {
	g.lock.Lock()
	defer g.lock.Unlock()
	crs := crypto.Keccak256Hash(signedCRS)
	if err := g.stateModule.ProposeCRS(round, crs); err != nil {
		// CRS can be proposed multiple times, other errors are not
		// accepted.
		if err != ErrDuplicatedChange {
			panic(err)
		}
		return
	}
	g.broadcastPendingStateChanges()
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
	if err := g.stateModule.RequestChange(
		StateAddDKGComplaint, complaint); err != nil {
		panic(err)
	}
	g.broadcastPendingStateChanges()
}

// DKGComplaints returns the DKGComplaints of round.
func (g *Governance) DKGComplaints(round uint64) []*typesDKG.Complaint {
	return g.stateModule.DKGComplaints(round)
}

// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
func (g *Governance) AddDKGMasterPublicKey(
	round uint64, masterPublicKey *typesDKG.MasterPublicKey) {
	if round != masterPublicKey.Round {
		return
	}
	if g.IsDKGMPKReady(masterPublicKey.Round) {
		return
	}
	if err := g.stateModule.RequestChange(
		StateAddDKGMasterPublicKey, masterPublicKey); err != nil {
		panic(err)
	}
	g.broadcastPendingStateChanges()
}

// DKGMasterPublicKeys returns the DKGMasterPublicKeys of round.
func (g *Governance) DKGMasterPublicKeys(
	round uint64) []*typesDKG.MasterPublicKey {
	return g.stateModule.DKGMasterPublicKeys(round)
}

// AddDKGMPKReady adds a DKG ready message.
func (g *Governance) AddDKGMPKReady(round uint64, ready *typesDKG.MPKReady) {
	if round != ready.Round {
		return
	}
	if err := g.stateModule.RequestChange(StateAddDKGMPKReady, ready); err != nil {
		panic(err)
	}
	g.broadcastPendingStateChanges()
}

// IsDKGMPKReady checks if DKG is ready.
func (g *Governance) IsDKGMPKReady(round uint64) bool {
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
	return g.stateModule.IsDKGMPKReady(round, int(g.configs[round].DKGSetSize)/3*2)
}

// AddDKGFinalize adds a DKG finalize message.
func (g *Governance) AddDKGFinalize(round uint64, final *typesDKG.Finalize) {
	if round != final.Round {
		return
	}
	if err := g.stateModule.RequestChange(StateAddDKGFinal, final); err != nil {
		panic(err)
	}
	g.broadcastPendingStateChanges()
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
	return g.stateModule.IsDKGFinal(round, int(g.configs[round].DKGSetSize)/3*2)
}

//
// Test Utilities
//

type packedStateChanges []byte

// This method broadcasts pending state change requests in the underlying
// State instance, this behavior is to simulate tx-gossiping in full nodes.
func (g *Governance) broadcastPendingStateChanges() {
	if g.networkModule == nil {
		return
	}
	packed, err := g.stateModule.PackOwnRequests()
	if err != nil {
		panic(err)
	}
	g.networkModule.Broadcast(packedStateChanges(packed))
}

// State allows to access embed State instance.
func (g *Governance) State() *State {
	return g.stateModule
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
		config, nodeSet := g.stateModule.Snapshot()
		g.configs = append(g.configs, config)
		g.nodeSets = append(g.nodeSets, nodeSet)
	}
}

// Clone a governance instance with replicate internal state.
func (g *Governance) Clone() *Governance {
	g.lock.RLock()
	defer g.lock.RUnlock()
	// Clone state.
	copiedState := g.stateModule.Clone()
	// Clone configs.
	copiedConfigs := []*types.Config{}
	for _, c := range g.configs {
		copiedConfigs = append(copiedConfigs, c.Clone())
	}
	// Clone node sets.
	copiedPendingChanges := make(map[uint64]map[StateChangeType]interface{})
	for round, forRound := range g.pendingConfigChanges {
		copiedForRound := make(map[StateChangeType]interface{})
		for k, v := range forRound {
			copiedForRound[k] = v
		}
		copiedPendingChanges[round] = copiedForRound
	}
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
	// Clone pending changes.
	return &Governance{
		roundShift:           g.roundShift,
		configs:              copiedConfigs,
		stateModule:          copiedState,
		nodeSets:             copiedNodeSets,
		pendingConfigChanges: copiedPendingChanges,
	}
}

// Equal checks equality between two Governance instances.
func (g *Governance) Equal(other *Governance, checkState bool) bool {
	// Check roundShift.
	if g.roundShift != other.roundShift {
		return false
	}
	// Check configs.
	if !reflect.DeepEqual(g.configs, other.configs) {
		return false
	}
	// Check node sets.
	if len(g.nodeSets) != len(other.nodeSets) {
		return false
	}
	// Check pending changes.
	if !reflect.DeepEqual(g.pendingConfigChanges, other.pendingConfigChanges) {
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
		return g.stateModule.Equal(other.stateModule) == nil
	}
	return true
}

// RegisterConfigChange tells this governance instance to request some
// configuration change at some round.
// NOTE: you can't request config change for round 0, 1, they are genesis
//       rounds.
// NOTE: this function should be called before running.
func (g *Governance) RegisterConfigChange(
	round uint64, t StateChangeType, v interface{}) (err error) {
	if t < StateChangeNumChains || t > StateChangeDKGSetSize {
		return fmt.Errorf("state changes to register is not supported: %v", t)
	}
	if round < 2 {
		return errors.New(
			"attempt to register state change for genesis rounds")
	}
	g.lock.Lock()
	defer g.lock.Unlock()
	if round < uint64(len(g.configs)) {
		return errors.New(
			"attempt to register state change for prepared rounds")
	}
	pendingChangesForRound, exists := g.pendingConfigChanges[round]
	if !exists {
		pendingChangesForRound = make(map[StateChangeType]interface{})
		g.pendingConfigChanges[round] = pendingChangesForRound
	}
	pendingChangesForRound[t] = v
	return nil
}

// SwitchToRemoteMode would switch this governance instance to remote mode,
// which means: it will broadcast all changes from its underlying state
// instance.
func (g *Governance) SwitchToRemoteMode(n *Network) {
	if g.networkModule != nil {
		panic(errors.New("not in local mode before switching"))
	}
	g.stateModule.SwitchToRemoteMode()
	g.networkModule = n
	n.addStateModule(g.stateModule)
}
