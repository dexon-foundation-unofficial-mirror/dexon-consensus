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

package core

import (
	"errors"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

var (
	// ErrRoundNotReady means we got nil config from governance contract.
	ErrRoundNotReady = errors.New("round is not ready")
)

// NodeSetCache caches node set information from governance contract.
type NodeSetCache struct {
	lock    sync.RWMutex
	gov     Governance
	rounds  map[uint64]*types.NodeSet
	keyPool map[types.NodeID]*struct {
		pubKey crypto.PublicKey
		refCnt int
	}
}

// NewNodeSetCache constructs an NodeSetCache instance.
func NewNodeSetCache(gov Governance) *NodeSetCache {
	return &NodeSetCache{
		gov:    gov,
		rounds: make(map[uint64]*types.NodeSet),
		keyPool: make(map[types.NodeID]*struct {
			pubKey crypto.PublicKey
			refCnt int
		}),
	}
}

// Exists checks if a node is in node set of that round.
func (cache *NodeSetCache) Exists(
	round uint64, nodeID types.NodeID) (exists bool, err error) {

	nIDs, exists := cache.get(round)
	if !exists {
		if nIDs, err = cache.update(round); err != nil {
			return
		}
	}
	_, exists = nIDs.IDs[nodeID]
	return
}

// GetPublicKey return public key for that node:
func (cache *NodeSetCache) GetPublicKey(
	nodeID types.NodeID) (key crypto.PublicKey, exists bool) {

	cache.lock.RLock()
	defer cache.lock.RUnlock()

	rec, exists := cache.keyPool[nodeID]
	if exists {
		key = rec.pubKey
	}
	return
}

// GetNodeSet returns IDs of nodes set of this round as map.
func (cache *NodeSetCache) GetNodeSet(
	round uint64) (nIDs *types.NodeSet, err error) {

	IDs, exists := cache.get(round)
	if !exists {
		if IDs, err = cache.update(round); err != nil {
			return
		}
	}
	nIDs = IDs.Clone()
	return
}

// update node set for that round.
//
// This cache would maintain 10 rounds before the updated round and purge
// rounds not in this range.
func (cache *NodeSetCache) update(
	round uint64) (nIDs *types.NodeSet, err error) {

	cache.lock.Lock()
	defer cache.lock.Unlock()

	// Get the requested round from governance contract.
	keySet := cache.gov.NodeSet(round)
	if keySet == nil {
		// That round is not ready yet.
		err = ErrRoundNotReady
		return
	}
	// Cache new round.
	nIDs = types.NewNodeSet()
	for _, key := range keySet {
		nID := types.NewNodeID(key)
		nIDs.Add(nID)
		if rec, exists := cache.keyPool[nID]; exists {
			rec.refCnt++
		} else {
			cache.keyPool[nID] = &struct {
				pubKey crypto.PublicKey
				refCnt int
			}{key, 1}
		}
	}
	cache.rounds[round] = nIDs
	// Purge older rounds.
	for rID, nIDs := range cache.rounds {
		if round-rID <= 5 {
			continue
		}
		for nID := range nIDs.IDs {
			rec := cache.keyPool[nID]
			if rec.refCnt--; rec.refCnt == 0 {
				delete(cache.keyPool, nID)
			}
		}
		delete(cache.rounds, rID)
	}
	return
}

func (cache *NodeSetCache) get(
	round uint64) (nIDs *types.NodeSet, exists bool) {

	cache.lock.RLock()
	defer cache.lock.RUnlock()

	nIDs, exists = cache.rounds[round]
	return
}
