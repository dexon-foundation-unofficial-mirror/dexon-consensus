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
	roundBeginHeights    []uint64
	stateModule          *State
	networkModule        *Network
	pendingConfigChanges map[uint64]map[StateChangeType]interface{}
	prohibitedTypes      map[StateChangeType]struct{}
	lock                 sync.RWMutex
}

// NewGovernance constructs a Governance instance.
func NewGovernance(state *State, roundShift uint64) (g *Governance, err error) {
	// Setup a State instance.
	g = &Governance{
		roundShift:           roundShift,
		pendingConfigChanges: make(map[uint64]map[StateChangeType]interface{}),
		stateModule:          state,
		prohibitedTypes:      make(map[StateChangeType]struct{}),
		roundBeginHeights:    []uint64{types.GenesisHeight},
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

// GetRoundHeight returns the begin height of a round.
func (g *Governance) GetRoundHeight(round uint64) uint64 {
	// This is a workaround to fit fullnode's behavior, their 0 is reserved for
	// a genesis block unseen to core.
	if round == 0 {
		return 0
	}
	g.lock.RLock()
	defer g.lock.RUnlock()
	if round >= uint64(len(g.roundBeginHeights)) {
		panic(fmt.Errorf("round begin height is not ready: %d %d",
			round, len(g.roundBeginHeights)))
	}
	return g.roundBeginHeights[round]
}

// CRS returns the CRS for a given round.
func (g *Governance) CRS(round uint64) common.Hash {
	return g.stateModule.CRS(round)
}

// NotifyRound notifies governace contract to snapshot config, and broadcast
// pending state change requests for next round if any.
func (g *Governance) NotifyRound(round, beginHeight uint64) {
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
		if round == uint64(len(g.roundBeginHeights)) {
			g.roundBeginHeights = append(g.roundBeginHeights, beginHeight)
		} else if round < uint64(len(g.roundBeginHeights)) {
			if beginHeight != g.roundBeginHeights[round] {
				panic(fmt.Errorf("mismatched round begin height: %d %d %d",
					round, beginHeight, g.roundBeginHeights[round]))
			}
		} else {
			panic(fmt.Errorf("discontinuous round begin height: %d %d %d",
				round, beginHeight, len(g.roundBeginHeights)))
		}
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
func (g *Governance) AddDKGComplaint(complaint *typesDKG.Complaint) {
	if g.isProhibited(StateAddDKGComplaint) {
		return
	}
	if g.IsDKGFinal(complaint.Round) {
		return
	}
	if err := g.stateModule.RequestChange(
		StateAddDKGComplaint, complaint); err != nil {
		if err != ErrChangeWontApply {
			panic(err)
		}
	}
	g.broadcastPendingStateChanges()
}

// DKGComplaints returns the DKGComplaints of round.
func (g *Governance) DKGComplaints(round uint64) []*typesDKG.Complaint {
	return g.stateModule.DKGComplaints(round)
}

// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
func (g *Governance) AddDKGMasterPublicKey(masterPublicKey *typesDKG.MasterPublicKey) {
	if g.isProhibited(StateAddDKGMasterPublicKey) {
		return
	}
	if g.IsDKGMPKReady(masterPublicKey.Round) {
		return
	}
	if err := g.stateModule.RequestChange(
		StateAddDKGMasterPublicKey, masterPublicKey); err != nil {
		if err != ErrChangeWontApply {
			panic(err)
		}
	}
	g.broadcastPendingStateChanges()
}

// DKGMasterPublicKeys returns the DKGMasterPublicKeys of round.
func (g *Governance) DKGMasterPublicKeys(
	round uint64) []*typesDKG.MasterPublicKey {
	return g.stateModule.DKGMasterPublicKeys(round)
}

// AddDKGMPKReady adds a DKG ready message.
func (g *Governance) AddDKGMPKReady(ready *typesDKG.MPKReady) {
	if err := g.stateModule.RequestChange(
		StateAddDKGMPKReady, ready); err != nil {
		if err != ErrChangeWontApply {
			panic(err)
		}
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
	return g.stateModule.IsDKGMPKReady(round, int(g.configs[round].NotarySetSize)*2/3+1)
}

// AddDKGFinalize adds a DKG finalize message.
func (g *Governance) AddDKGFinalize(final *typesDKG.Finalize) {
	if g.isProhibited(StateAddDKGFinal) {
		return
	}
	if err := g.stateModule.RequestChange(StateAddDKGFinal, final); err != nil {
		if err != ErrChangeWontApply {
			panic(err)
		}
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
	return g.stateModule.IsDKGFinal(round, int(g.configs[round].NotarySetSize)*2/3+1)
}

// AddDKGSuccess adds a DKG success message.
func (g *Governance) AddDKGSuccess(success *typesDKG.Success) {
	if g.isProhibited(StateAddDKGSuccess) {
		return
	}
	if err := g.stateModule.RequestChange(StateAddDKGSuccess, success); err != nil {
		if err != ErrChangeWontApply {
			panic(err)
		}
	}
	g.broadcastPendingStateChanges()
}

// IsDKGSuccess checks if DKG is success.
func (g *Governance) IsDKGSuccess(round uint64) bool {
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
	return g.stateModule.IsDKGFinal(round, int(g.configs[round].NotarySetSize)*5/6)
}

// ReportForkVote reports a node for forking votes.
func (g *Governance) ReportForkVote(vote1, vote2 *types.Vote) {
}

// ReportForkBlock reports a node for forking blocks.
func (g *Governance) ReportForkBlock(block1, block2 *types.Block) {
}

// ResetDKG resets latest DKG data and propose new CRS.
func (g *Governance) ResetDKG(newSignedCRS []byte) {
	g.lock.Lock()
	defer g.lock.Unlock()
	crs := crypto.Keccak256Hash(newSignedCRS)
	if err := g.stateModule.RequestChange(StateResetDKG, crs); err != nil {
		// ResetDKG can be proposed multiple times, other errors are not
		// accepted.
		if err != ErrDuplicatedChange {
			panic(err)
		}
		return
	}
	g.broadcastPendingStateChanges()
}

// DKGResetCount returns the reset count for DKG of given round.
func (g *Governance) DKGResetCount(round uint64) uint64 {
	return g.stateModule.DKGResetCount(round)
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
	if err := g.networkModule.Broadcast(packedStateChanges(packed)); err != nil {
		panic(err)
	}
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
	if round >= 1 && len(g.roundBeginHeights) == 1 {
		// begin height of round 0 and round 1 should be ready, they won't be
		// afected by DKG reset mechanism.
		g.roundBeginHeights = append(g.roundBeginHeights,
			g.configs[0].RoundLength+g.roundBeginHeights[0])
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
	// Clone prohibited flag.
	copiedProhibitedTypes := make(map[StateChangeType]struct{})
	for t := range g.prohibitedTypes {
		copiedProhibitedTypes[t] = struct{}{}
	}
	// Clone pending changes.
	return &Governance{
		roundShift:           g.roundShift,
		configs:              copiedConfigs,
		stateModule:          copiedState,
		nodeSets:             copiedNodeSets,
		pendingConfigChanges: copiedPendingChanges,
		prohibitedTypes:      copiedProhibitedTypes,
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
	// Check prohibited types.
	if !reflect.DeepEqual(g.prohibitedTypes, other.prohibitedTypes) {
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
	if t < StateAddCRS || t > StateChangeNotarySetSize {
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

// Prohibit would prohibit DKG related state change requests.
//
// Note this method only prevents local modification, state changes related to
// DKG from others won't be prohibited.
func (g *Governance) Prohibit(t StateChangeType) {
	g.lock.Lock()
	defer g.lock.Unlock()
	switch t {
	case StateAddDKGMasterPublicKey, StateAddDKGFinal, StateAddDKGComplaint:
		g.prohibitedTypes[t] = struct{}{}
	default:
		panic(fmt.Errorf("unsupported state change type to prohibit: %s", t))
	}
}

// Unprohibit would unprohibit DKG related state change requests.
func (g *Governance) Unprohibit(t StateChangeType) {
	g.lock.Lock()
	defer g.lock.Unlock()
	delete(g.prohibitedTypes, t)
}

// isProhibited checks if a state change request is prohibited or not.
func (g *Governance) isProhibited(t StateChangeType) (prohibited bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	_, prohibited = g.prohibitedTypes[t]
	return
}
