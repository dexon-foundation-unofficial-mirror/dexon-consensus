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

package utils

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type nsIntf struct {
	s       *NodeSetCacheTestSuite
	crs     common.Hash
	curKeys []crypto.PublicKey
}

func (g *nsIntf) Configuration(round uint64) (cfg *types.Config) {
	return &types.Config{
		NotarySetSize:    7,
		RoundLength:      60,
		LambdaBA:         250 * time.Millisecond,
		MinBlockInterval: 1 * time.Second,
	}
}
func (g *nsIntf) CRS(round uint64) (b common.Hash) { return g.crs }
func (g *nsIntf) NodeSet(round uint64) []crypto.PublicKey {
	// Randomly generating keys, and check them for verification.
	g.curKeys = []crypto.PublicKey{}
	for i := 0; i < 10; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		g.s.Require().NoError(err)
		g.curKeys = append(g.curKeys, prvKey.PublicKey())
	}
	return g.curKeys
}

type NodeSetCacheTestSuite struct {
	suite.Suite
}

func (s *NodeSetCacheTestSuite) TestBasicUsage() {
	var (
		nsIntf = &nsIntf{
			s:   s,
			crs: common.NewRandomHash(),
		}
		cache = NewNodeSetCache(nsIntf)
		req   = s.Require()
	)

	chk := func(
		cache *NodeSetCache, round uint64, nodeSet map[types.NodeID]struct{}) {

		for nID := range nodeSet {
			// It should exists.
			exists, err := cache.Exists(round, nID)
			req.NoError(err)
			req.True(exists)
			// We could get keys.
			key, exists := cache.GetPublicKey(nID)
			req.NotNil(key)
			req.True(exists)
		}
	}

	// Try to get round 0.
	nodeSet0, err := cache.GetNodeSet(0)
	req.NoError(err)
	chk(cache, 0, nodeSet0.IDs)
	notarySet, err := cache.GetNotarySet(0)
	req.NoError(err)
	chk(cache, 0, notarySet)
	// Try to get round 1.
	nodeSet1, err := cache.GetNodeSet(1)
	req.NoError(err)
	chk(cache, 0, nodeSet0.IDs)
	chk(cache, 1, nodeSet1.IDs)
	// Try to get round 6, round 0 should be purged.
	nodeSet6, err := cache.GetNodeSet(6)
	req.NoError(err)
	chk(cache, 1, nodeSet1.IDs)
	chk(cache, 6, nodeSet6.IDs)
	for nID := range nodeSet0.IDs {
		_, exists := cache.GetPublicKey(nID)
		req.False(exists)
	}
}

func (s *NodeSetCacheTestSuite) TestTouch() {
	var (
		nsIntf = &nsIntf{
			s:   s,
			crs: common.NewRandomHash(),
		}
		cache = NewNodeSetCache(nsIntf)
		req   = s.Require()
	)

	_, exists := cache.get(1)
	req.False(exists)

	err := cache.Touch(1)
	req.NoError(err)

	_, exists = cache.get(1)
	req.True(exists)
}

func (s *NodeSetCacheTestSuite) TestPurge() {
	var (
		nsIntf = &nsIntf{
			s:   s,
			crs: common.NewRandomHash(),
		}
		cache = NewNodeSetCache(nsIntf)
		req   = s.Require()
	)
	err := cache.Touch(1)
	req.NoError(err)
	_, exist := cache.get(1)
	req.True(exist)
	cache.Purge(1)
	_, exist = cache.get(1)
	req.False(exist)
}

func TestNodeSetCache(t *testing.T) {
	suite.Run(t, new(NodeSetCacheTestSuite))
}
