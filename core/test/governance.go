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

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var (
	// ErrPrivateKeyNotExists means caller request private key for an
	// unknown node ID.
	ErrPrivateKeyNotExists = fmt.Errorf("private key not exists")
)

// Governance is an implementation of Goverance for testing purpose.
type Governance struct {
	lambdaBA           time.Duration
	lambdaDKG          time.Duration
	privateKeys        map[types.NodeID]crypto.PrivateKey
	tsig               map[uint64]crypto.Signature
	DKGComplaint       map[uint64][]*types.DKGComplaint
	DKGMasterPublicKey map[uint64][]*types.DKGMasterPublicKey
	lock               sync.RWMutex
}

// NewGovernance constructs a Governance instance.
func NewGovernance(nodeCount int, lambda time.Duration) (
	g *Governance, err error) {
	g = &Governance{
		lambdaBA:           lambda,
		lambdaDKG:          lambda * 10,
		privateKeys:        make(map[types.NodeID]crypto.PrivateKey),
		tsig:               make(map[uint64]crypto.Signature),
		DKGComplaint:       make(map[uint64][]*types.DKGComplaint),
		DKGMasterPublicKey: make(map[uint64][]*types.DKGMasterPublicKey),
	}
	for i := 0; i < nodeCount; i++ {
		prv, err := ecdsa.NewPrivateKey()
		if err != nil {
			return nil, err
		}
		nID := types.NewNodeID(prv.PublicKey())
		g.privateKeys[nID] = prv
	}
	return
}

// GetNodeSet implements Governance interface to return current
// notary set.
func (g *Governance) GetNodeSet(_ uint64) (
	ret []crypto.PublicKey) {
	for _, key := range g.privateKeys {
		ret = append(ret, key.PublicKey())
	}
	return
}

// GetConfiguration returns the configuration at a given block height.
func (g *Governance) GetConfiguration(_ uint64) *types.Config {
	return &types.Config{
		NumShards: 1,
		NumChains: uint32(len(g.privateKeys)),
		LambdaBA:  g.lambdaBA,
		LambdaDKG: g.lambdaDKG,
		K:         0,
		PhiRatio:  0.667,
	}
}

// GetCRS returns the CRS for a given round.
func (g *Governance) GetCRS(round uint64) []byte {
	return []byte("__ DEXON")
}

// GetPrivateKeys return the private key for that node, this function
// is a test utility and not a general Governance interface.
func (g *Governance) GetPrivateKeys() (keys []crypto.PrivateKey) {
	for _, k := range g.privateKeys {
		keys = append(keys, k)
	}
	return
}

// ProposeThresholdSignature porposes a ThresholdSignature of round.
func (g *Governance) ProposeThresholdSignature(
	round uint64, signature crypto.Signature) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.tsig[round] = signature
}

// GetThresholdSignature gets a ThresholdSignature of round.
func (g *Governance) GetThresholdSignature(round uint64) (
	sig crypto.Signature, exist bool) {
	g.lock.RLock()
	defer g.lock.RUnlock()
	sig, exist = g.tsig[round]
	return
}

// AddDKGComplaint add a DKGComplaint.
func (g *Governance) AddDKGComplaint(complaint *types.DKGComplaint) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.DKGComplaint[complaint.Round] = append(g.DKGComplaint[complaint.Round],
		complaint)
}

// DKGComplaints returns the DKGComplaints of round.
func (g *Governance) DKGComplaints(round uint64) []*types.DKGComplaint {
	g.lock.RLock()
	defer g.lock.RUnlock()
	complaints, exist := g.DKGComplaint[round]
	if !exist {
		return []*types.DKGComplaint{}
	}
	return complaints
}

// AddDKGMasterPublicKey adds a DKGMasterPublicKey.
func (g *Governance) AddDKGMasterPublicKey(
	masterPublicKey *types.DKGMasterPublicKey) {
	g.lock.Lock()
	defer g.lock.Unlock()
	g.DKGMasterPublicKey[masterPublicKey.Round] = append(
		g.DKGMasterPublicKey[masterPublicKey.Round], masterPublicKey)
}

// DKGMasterPublicKeys returns the DKGMasterPublicKeys of round.
func (g *Governance) DKGMasterPublicKeys(
	round uint64) []*types.DKGMasterPublicKey {
	g.lock.RLock()
	defer g.lock.RUnlock()
	masterPublicKeys, exist := g.DKGMasterPublicKey[round]
	if !exist {
		return []*types.DKGMasterPublicKey{}
	}
	return masterPublicKeys
}
