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
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus-core/core/types/dkg"
)

var (
	// ErrPrivateKeyNotExists means caller request private key for an
	// unknown node ID.
	ErrPrivateKeyNotExists = fmt.Errorf("private key not exists")
)

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	privateKeys map[types.NodeID]crypto.PrivateKey
	configs     []*types.Config
	nodeSets    [][]crypto.PublicKey
	state       *State
	lock        sync.RWMutex
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
