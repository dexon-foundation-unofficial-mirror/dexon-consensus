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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type StopperTestSuite struct {
	suite.Suite
}

func (s *StopperTestSuite) deliver(
	blocks []*types.Block, app *App, db blockdb.BlockDatabase) {
	hashes := common.Hashes{}
	for _, b := range blocks {
		hashes = append(hashes, b.Hash)
		s.Require().NoError(db.Put(*b))
	}
	for _, h := range hashes {
		app.BlockConfirmed(types.Block{Hash: h})
	}
	app.TotalOrderingDelivered(hashes, core.TotalOrderingModeNormal)
	for _, h := range hashes {
		app.BlockDelivered(h, types.Position{}, types.FinalizationResult{
			Timestamp: time.Time{},
		})
	}
}

func (s *StopperTestSuite) deliverToAllNodes(
	blocks []*types.Block,
	apps map[types.NodeID]*App,
	dbs map[types.NodeID]blockdb.BlockDatabase) {
	for nID := range apps {
		s.deliver(blocks, apps[nID], dbs[nID])
	}
}

func (s *StopperTestSuite) TestStopByConfirmedBlocks() {
	// This test case makes sure this stopper would stop when
	// all nodes confirmed at least 'x' count of blocks produced
	// by themselves.
	var (
		req   = s.Require()
		apps  = make(map[types.NodeID]*App)
		dbs   = make(map[types.NodeID]blockdb.BlockDatabase)
		nodes = GenerateRandomNodeIDs(2)
	)
	for _, nID := range nodes {
		apps[nID] = NewApp(nil)
		db, err := blockdb.NewMemBackedBlockDB()
		req.NoError(err)
		dbs[nID] = db
	}
	stopper := NewStopByConfirmedBlocks(2, apps, dbs)
	b00 := &types.Block{
		ProposerID: nodes[0],
		Hash:       common.NewRandomHash(),
	}
	s.deliverToAllNodes([]*types.Block{b00}, apps, dbs)
	b10 := &types.Block{
		ProposerID: nodes[1],
		Hash:       common.NewRandomHash(),
	}
	b11 := &types.Block{
		ProposerID: nodes[1],
		ParentHash: b10.Hash,
		Hash:       common.NewRandomHash(),
	}
	s.deliverToAllNodes([]*types.Block{b10, b11}, apps, dbs)
	req.False(stopper.ShouldStop(nodes[1]))
	b12 := &types.Block{
		ProposerID: nodes[1],
		ParentHash: b11.Hash,
		Hash:       common.NewRandomHash(),
	}
	s.deliverToAllNodes([]*types.Block{b12}, apps, dbs)
	req.False(stopper.ShouldStop(nodes[1]))
	b01 := &types.Block{
		ProposerID: nodes[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
	}
	s.deliverToAllNodes([]*types.Block{b01}, apps, dbs)
	req.True(stopper.ShouldStop(nodes[0]))
}

func (s *StopperTestSuite) TestStopByRound() {
	// This test case make sure at least one block from round R
	// is delivered by each node.
	var (
		req   = s.Require()
		apps  = make(map[types.NodeID]*App)
		dbs   = make(map[types.NodeID]blockdb.BlockDatabase)
		nodes = GenerateRandomNodeIDs(2)
	)
	for _, nID := range nodes {
		apps[nID] = NewApp(nil)
		db, err := blockdb.NewMemBackedBlockDB()
		req.NoError(err)
		dbs[nID] = db
	}
	stopper := NewStopByRound(10, apps, dbs)
	b00 := &types.Block{
		ProposerID: nodes[0],
		Position: types.Position{
			Round:   0,
			ChainID: 0,
			Height:  0,
		},
		Hash: common.NewRandomHash(),
	}
	s.deliverToAllNodes([]*types.Block{b00}, apps, dbs)
	b10 := &types.Block{
		ProposerID: nodes[1],
		Position: types.Position{
			Round:   0,
			ChainID: 1,
			Height:  0,
		},
		Hash: common.NewRandomHash(),
	}
	b11 := &types.Block{
		ProposerID: nodes[1],
		ParentHash: b10.Hash,
		Position: types.Position{
			Round:   0,
			ChainID: 1,
			Height:  1,
		},
		Hash: common.NewRandomHash(),
	}
	s.deliverToAllNodes([]*types.Block{b10, b11}, apps, dbs)
	req.False(stopper.ShouldStop(nodes[0]))
	req.False(stopper.ShouldStop(nodes[1]))
	// Deliver one block at round 10 to node0
	b12 := &types.Block{
		ProposerID: nodes[1],
		ParentHash: b11.Hash,
		Position: types.Position{
			Round:   10,
			ChainID: 1,
			Height:  2,
		},
		Hash: common.NewRandomHash(),
	}
	// None should stop when only one node reach that round.
	s.deliver([]*types.Block{b12}, apps[nodes[0]], dbs[nodes[0]])
	req.False(stopper.ShouldStop(nodes[0]))
	req.False(stopper.ShouldStop(nodes[1]))
	// Everyone should stop now.
	s.deliver([]*types.Block{b12}, apps[nodes[1]], dbs[nodes[1]])
	req.True(stopper.ShouldStop(nodes[1]))
	req.True(stopper.ShouldStop(nodes[0]))
}

func TestStopper(t *testing.T) {
	suite.Run(t, new(StopperTestSuite))
}
