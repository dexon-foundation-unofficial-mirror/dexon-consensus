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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
)

var (
	// ErrPrivateKeyNotExists means caller request private key for an
	// unknown validator ID.
	ErrPrivateKeyNotExists = fmt.Errorf("private key not exists")
)

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	lambda             time.Duration
	notarySet          map[types.ValidatorID]struct{}
	privateKeys        map[types.ValidatorID]crypto.PrivateKey
	DKGComplaint       map[uint64][]*types.DKGComplaint
	DKGMasterPublicKey map[uint64][]*types.DKGMasterPublicKey
}

// NewGovernance constructs a Governance instance.
func NewGovernance(validatorCount int, lambda time.Duration) (
	g *Governance, err error) {
	g = &Governance{
		lambda:             lambda,
		notarySet:          make(map[types.ValidatorID]struct{}),
		privateKeys:        make(map[types.ValidatorID]crypto.PrivateKey),
		DKGComplaint:       make(map[uint64][]*types.DKGComplaint),
		DKGMasterPublicKey: make(map[uint64][]*types.DKGMasterPublicKey),
	}
	for i := 0; i < validatorCount; i++ {
		prv, err := eth.NewPrivateKey()
		if err != nil {
			return nil, err
		}
		vID := types.NewValidatorID(prv.PublicKey())
		g.notarySet[vID] = struct{}{}
		g.privateKeys[vID] = prv
	}
	return
}

// GetNotarySet implements Governance interface to return current
// notary set.
func (g *Governance) GetNotarySet() map[types.ValidatorID]struct{} {
	return g.notarySet
}

// GetConfiguration returns the configuration at a given block height.
func (g *Governance) GetConfiguration(blockHeight uint64) *types.Config {
	return &types.Config{
		NumShards:  1,
		NumChains:  uint32(len(g.notarySet)),
		GenesisCRS: "__ DEXON",
		Lambda:     g.lambda,
		K:          0,
		PhiRatio:   0.667,
	}
}

// GetPrivateKey return the private key for that validator, this function
// is a test utility and not a general Governance interface.
func (g *Governance) GetPrivateKey(
	vID types.ValidatorID) (key crypto.PrivateKey, err error) {

	key, exists := g.privateKeys[vID]
	if !exists {
		err = ErrPrivateKeyNotExists
		return
	}
	return
}

// AddDKGComplaint add a DKGComplaint.
func (g *Governance) AddDKGComplaint(complaint *types.DKGComplaint) {
	g.DKGComplaint[complaint.Round] = append(g.DKGComplaint[complaint.Round], complaint)
}

// DKGComplaints returns the DKGComplaints of round.
func (g *Governance) DKGComplaints(round uint64) []*types.DKGComplaint {
	complaints, exist := g.DKGComplaint[round]
	if !exist {
		return []*types.DKGComplaint{}
	}
	return complaints
}

// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
func (g *Governance) AddDKGMasterPublicKey(
	masterPublicKey *types.DKGMasterPublicKey) {
	g.DKGMasterPublicKey[masterPublicKey.Round] = append(
		g.DKGMasterPublicKey[masterPublicKey.Round], masterPublicKey)
}

// DKGMasterPublicKeys returns the DKGMasterPublicKeys of round.
func (g *Governance) DKGMasterPublicKeys(
	round uint64) []*types.DKGMasterPublicKey {
	masterPublicKeys, exist := g.DKGMasterPublicKey[round]
	if !exist {
		return []*types.DKGMasterPublicKey{}
	}
	return masterPublicKeys
}
