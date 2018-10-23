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
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
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
	crs                []common.Hash
	tsig               map[uint64]crypto.Signature
	DKGComplaint       map[uint64][]*types.DKGComplaint
	DKGMasterPublicKey map[uint64][]*types.DKGMasterPublicKey
	DKGFinal           map[uint64]map[types.NodeID]struct{}
	RoundInterval      time.Duration
	MinBlockInterval   time.Duration
	MaxBlockInterval   time.Duration
	lock               sync.RWMutex
}

// NewGovernance constructs a Governance instance.
func NewGovernance(nodeCount int, lambda time.Duration) (
	g *Governance, err error) {
	hashCRS := crypto.Keccak256Hash([]byte("__ DEXON"))
	g = &Governance{
		lambdaBA:           lambda,
		lambdaDKG:          lambda * 10,
		privateKeys:        make(map[types.NodeID]crypto.PrivateKey),
		crs:                []common.Hash{hashCRS},
		tsig:               make(map[uint64]crypto.Signature),
		DKGComplaint:       make(map[uint64][]*types.DKGComplaint),
		DKGMasterPublicKey: make(map[uint64][]*types.DKGMasterPublicKey),
		DKGFinal:           make(map[uint64]map[types.NodeID]struct{}),
		RoundInterval:      365 * 86400 * time.Second,
		MinBlockInterval:   1 * time.Millisecond,
		MaxBlockInterval:   lambda * 8,
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

// NodeSet implements Governance interface to return current
// notary set.
func (g *Governance) NodeSet(_ uint64) (
	ret []crypto.PublicKey) {
	for _, key := range g.privateKeys {
		ret = append(ret, key.PublicKey())
	}
	return
}

// Configuration returns the configuration at a given block height.
func (g *Governance) Configuration(_ uint64) *types.Config {
	return &types.Config{
		NumChains:        uint32(len(g.privateKeys)),
		LambdaBA:         g.lambdaBA,
		LambdaDKG:        g.lambdaDKG,
		K:                0,
		PhiRatio:         0.667,
		NotarySetSize:    uint32(len(g.privateKeys)),
		DKGSetSize:       uint32(len(g.privateKeys)),
		RoundInterval:    g.RoundInterval,
		MinBlockInterval: g.MinBlockInterval,
		MaxBlockInterval: g.MaxBlockInterval,
	}
}

// CRS returns the CRS for a given round.
func (g *Governance) CRS(round uint64) common.Hash {
	g.lock.RLock()
	defer g.lock.RUnlock()
	if round >= uint64(len(g.crs)) {
		return common.Hash{}
	}
	return g.crs[round]
}

// NotifyRoundHeight notifies governace contract to snapshot config.
func (g *Governance) NotifyRoundHeight(round, height uint64) {
}

// ProposeCRS propose a CRS.
func (g *Governance) ProposeCRS(round uint64, signedCRS []byte) {
	g.lock.Lock()
	defer g.lock.Unlock()
	crs := crypto.Keccak256Hash(signedCRS)
	if g.crs[len(g.crs)-1].Equal(crs) {
		return
	}
	g.crs = append(g.crs, crs)
}

// PrivateKeys return the private key for that node, this function
// is a test utility and not a general Governance interface.
func (g *Governance) PrivateKeys() (keys []crypto.PrivateKey) {
	for _, k := range g.privateKeys {
		keys = append(keys, k)
	}
	return
}

// AddDKGComplaint add a DKGComplaint.
func (g *Governance) AddDKGComplaint(
	round uint64, complaint *types.DKGComplaint) {
	if round != complaint.Round {
		return
	}
	if g.IsDKGFinal(complaint.Round) {
		return
	}
	g.lock.Lock()
	defer g.lock.Unlock()
	if _, exist := g.DKGFinal[complaint.Round][complaint.ProposerID]; exist {
		return
	}
	for _, comp := range g.DKGComplaint[complaint.Round] {
		if comp == complaint {
			return
		}
	}
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
	round uint64, masterPublicKey *types.DKGMasterPublicKey) {
	if round != masterPublicKey.Round {
		return
	}
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
	mpks := make([]*types.DKGMasterPublicKey, 0, len(masterPublicKeys))
	for _, mpk := range masterPublicKeys {
		bytes, _ := json.Marshal(mpk)
		mpkCopy := types.NewDKGMasterPublicKey()
		json.Unmarshal(bytes, mpkCopy)
		mpks = append(mpks, mpkCopy)
	}
	return mpks
}

// AddDKGFinalize adds a DKG finalize message.
func (g *Governance) AddDKGFinalize(round uint64, final *types.DKGFinalize) {
	if round != final.Round {
		return
	}
	g.lock.Lock()
	defer g.lock.Unlock()
	if _, exist := g.DKGFinal[final.Round]; !exist {
		g.DKGFinal[final.Round] = make(map[types.NodeID]struct{})
	}
	g.DKGFinal[final.Round][final.ProposerID] = struct{}{}
}

// IsDKGFinal checks if DKG is final.
func (g *Governance) IsDKGFinal(round uint64) bool {
	g.lock.RLock()
	defer g.lock.RUnlock()
	return len(g.DKGFinal[round]) > int(g.Configuration(round).DKGSetSize)/3*2
}
