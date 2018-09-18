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
	"github.com/shopspring/decimal"
)

var (
	// ErrPrivateKeyNotExists means caller request private key for an
	// unknown validator ID.
	ErrPrivateKeyNotExists = fmt.Errorf("private key not exists")
)

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	Lambda             time.Duration
	Validators         map[types.ValidatorID]decimal.Decimal
	PrivateKeys        map[types.ValidatorID]crypto.PrivateKey
	DKGComplaint       map[uint64][]*types.DKGComplaint
	DKGMasterPublicKey map[uint64][]*types.DKGMasterPublicKey
}

// NewGovernance constructs a Governance instance.
func NewGovernance(validatorCount int, lambda time.Duration) (
	g *Governance, err error) {
	g = &Governance{
		Lambda:             lambda,
		Validators:         make(map[types.ValidatorID]decimal.Decimal),
		PrivateKeys:        make(map[types.ValidatorID]crypto.PrivateKey),
		DKGComplaint:       make(map[uint64][]*types.DKGComplaint),
		DKGMasterPublicKey: make(map[uint64][]*types.DKGMasterPublicKey),
	}
	for i := 0; i < validatorCount; i++ {
		prv, err := eth.NewPrivateKey()
		if err != nil {
			return nil, err
		}
		vID := types.NewValidatorID(prv.PublicKey())
		g.Validators[vID] = decimal.NewFromFloat(0)
		g.PrivateKeys[vID] = prv
	}
	return
}

// GetValidatorSet implements Governance interface to return current
// validator set.
func (g *Governance) GetValidatorSet() map[types.ValidatorID]decimal.Decimal {
	return g.Validators
}

// GetTotalOrderingK returns K.
func (g *Governance) GetTotalOrderingK() int {
	return 0
}

// GetPhiRatio returns phi ratio.
func (g *Governance) GetPhiRatio() float32 {
	return 0.667
}

// GetNumShards returns the number of shards.
func (g *Governance) GetNumShards() uint32 {
	return 1
}

// GetNumChains returns the number of chains.
func (g *Governance) GetNumChains() uint32 {
	return uint32(len(g.Validators))
}

// GetConfigurationChangeEvent Get configuration change events after a certain
// epoch.
func (g *Governance) GetConfigurationChangeEvent(
	epoch int) []types.ConfigurationChangeEvent {
	return nil
}

// GetGenesisCRS returns the CRS string.
func (g *Governance) GetGenesisCRS() string {
	return "🆕 DEXON"
}

// GetLambda returns lambda for BA.
func (g *Governance) GetLambda() time.Duration {
	return g.Lambda
}

// GetPrivateKey return the private key for that validator, this function
// is a test utility and not a general Governance interface.
func (g *Governance) GetPrivateKey(
	vID types.ValidatorID) (key crypto.PrivateKey, err error) {

	key, exists := g.PrivateKeys[vID]
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
