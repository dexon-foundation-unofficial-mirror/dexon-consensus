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
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

// network implements core.Network.
type network struct {
}

// BroadcastVote broadcasts vote to all nodes in DEXON network.
func (n *network) BroadcastVote(vote *types.Vote) {}

// BroadcastBlock broadcasts block to all nodes in DEXON network.
func (n *network) BroadcastBlock(block *types.Block) {
}

// BroadcastWitnessAck broadcasts witnessAck to all nodes in DEXON network.
func (n *network) BroadcastWitnessAck(witnessAck *types.WitnessAck) {
}

// SendDKGPrivateShare sends PrivateShare to a DKG participant.
func (n *network) SendDKGPrivateShare(
	recv types.NodeID, prvShare *types.DKGPrivateShare) {
}

// BroadcastDKGPrivateShare broadcasts PrivateShare to all DKG participants.
func (n *network) BroadcastDKGPrivateShare(
	prvShare *types.DKGPrivateShare) {
}

// ReceiveChan returns a channel to receive messages from DEXON network.
func (n *network) ReceiveChan() <-chan interface{} {
	return make(chan interface{})
}

type ConsensusTestSuite struct {
	suite.Suite
}

func (s *ConsensusTestSuite) prepareGenesisBlock(
	proposerID types.NodeID,
	chainID uint32,
	con *Consensus) *types.Block {

	block := &types.Block{
		ProposerID: proposerID,
		Position: types.Position{
			ChainID: chainID,
		},
	}
	err := con.PrepareGenesisBlock(block, time.Now().UTC())
	s.Require().Nil(err)
	return block
}

func (s *ConsensusTestSuite) prepareConsensus(
	gov *test.Governance, nID types.NodeID) (*test.App, *Consensus) {

	app := test.NewApp()
	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)
	prv, exist := gov.GetPrivateKey(nID)
	s.Require().Nil(exist)
	con := NewConsensus(app, gov, db, &network{}, prv, eth.SigToPub)
	return app, con
}

func (s *ConsensusTestSuite) TestSimpleDeliverBlock() {
	// This test scenario:
	// o o o o <- this layer makes older blocks strongly acked.
	// |x|x|x| <- lots of acks.
	// o | o o <- this layer would be sent to total ordering.
	// |\|/|-|
	// | o | | <- the only block which is acked by all other blocks
	// |/|\|\|    at the same height.
	// o o o o <- genesis blocks
	// 0 1 2 3 <- index of node ID
	//
	// This test case only works for Total Ordering with K=0.
	var (
		minInterval = 50 * time.Millisecond
		gov, err    = test.NewGovernance(4, time.Second)
		req         = s.Require()
		nodes       []types.NodeID
	)
	s.Require().Nil(err)

	for nID := range gov.GetNotarySet() {
		nodes = append(nodes, nID)
	}

	// Setup core.Consensus and test.App.
	objs := map[types.NodeID]*struct {
		app *test.App
		con *Consensus
	}{}
	for _, nID := range nodes {
		app, con := s.prepareConsensus(gov, nID)
		objs[nID] = &struct {
			app *test.App
			con *Consensus
		}{app, con}
	}
	// It's a helper function to emit one block
	// to all core.Consensus objects.
	broadcast := func(b *types.Block) {
		for _, obj := range objs {
			req.Nil(obj.con.processBlock(b))
		}
	}
	// Genesis blocks
	b00 := s.prepareGenesisBlock(nodes[0], 0, objs[nodes[0]].con)
	time.Sleep(minInterval)
	b10 := s.prepareGenesisBlock(nodes[1], 1, objs[nodes[1]].con)
	time.Sleep(minInterval)
	b20 := s.prepareGenesisBlock(nodes[2], 2, objs[nodes[2]].con)
	time.Sleep(minInterval)
	b30 := s.prepareGenesisBlock(nodes[3], 3, objs[nodes[3]].con)
	broadcast(b00)
	broadcast(b10)
	broadcast(b20)
	broadcast(b30)
	// Setup b11.
	time.Sleep(minInterval)
	b11 := &types.Block{
		ProposerID: nodes[1],
		Position: types.Position{
			ChainID: 1,
		},
	}
	b11.Hash, err = hashBlock(b11)
	s.Require().Nil(err)
	req.Nil(objs[nodes[1]].con.prepareBlock(b11, time.Now().UTC()))
	req.Len(b11.Acks, 4)
	req.Contains(b11.Acks, b00.Hash)
	req.Contains(b11.Acks, b10.Hash)
	req.Contains(b11.Acks, b20.Hash)
	req.Contains(b11.Acks, b30.Hash)
	broadcast(b11)
	// Setup b01.
	time.Sleep(minInterval)
	b01 := &types.Block{
		ProposerID: nodes[0],
		Position: types.Position{
			ChainID: 0,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[0]].con.prepareBlock(b01, time.Now().UTC()))
	req.Len(b01.Acks, 4)
	req.Contains(b01.Acks, b11.Hash)
	// Setup b21.
	time.Sleep(minInterval)
	b21 := &types.Block{
		ProposerID: nodes[2],
		Position: types.Position{
			ChainID: 2,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[2]].con.prepareBlock(b21, time.Now().UTC()))
	req.Len(b21.Acks, 4)
	req.Contains(b21.Acks, b11.Hash)
	// Setup b31.
	time.Sleep(minInterval)
	b31 := &types.Block{
		ProposerID: nodes[3],
		Position: types.Position{
			ChainID: 3,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[3]].con.prepareBlock(b31, time.Now().UTC()))
	req.Len(b31.Acks, 4)
	req.Contains(b31.Acks, b11.Hash)
	// Broadcast other height=1 blocks.
	broadcast(b01)
	broadcast(b21)
	broadcast(b31)
	// Setup height=2 blocks.
	// Setup b02.
	time.Sleep(minInterval)
	b02 := &types.Block{
		ProposerID: nodes[0],
		Position: types.Position{
			ChainID: 0,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[0]].con.prepareBlock(b02, time.Now().UTC()))
	req.Len(b02.Acks, 3)
	req.Contains(b02.Acks, b01.Hash)
	req.Contains(b02.Acks, b21.Hash)
	req.Contains(b02.Acks, b31.Hash)
	// Setup b12.
	time.Sleep(minInterval)
	b12 := &types.Block{
		ProposerID: nodes[1],
		Position: types.Position{
			ChainID: 1,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[1]].con.prepareBlock(b12, time.Now().UTC()))
	req.Len(b12.Acks, 4)
	req.Contains(b12.Acks, b01.Hash)
	req.Contains(b12.Acks, b11.Hash)
	req.Contains(b12.Acks, b21.Hash)
	req.Contains(b12.Acks, b31.Hash)
	// Setup b22.
	time.Sleep(minInterval)
	b22 := &types.Block{
		ProposerID: nodes[2],
		Position: types.Position{
			ChainID: 2,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[2]].con.prepareBlock(b22, time.Now().UTC()))
	req.Len(b22.Acks, 3)
	req.Contains(b22.Acks, b01.Hash)
	req.Contains(b22.Acks, b21.Hash)
	req.Contains(b22.Acks, b31.Hash)
	// Setup b32.
	time.Sleep(minInterval)
	b32 := &types.Block{
		ProposerID: nodes[3],
		Position: types.Position{
			ChainID: 3,
		},
		Hash: common.NewRandomHash(),
	}
	req.Nil(objs[nodes[3]].con.prepareBlock(b32, time.Now().UTC()))
	req.Len(b32.Acks, 3)
	req.Contains(b32.Acks, b01.Hash)
	req.Contains(b32.Acks, b21.Hash)
	req.Contains(b32.Acks, b31.Hash)
	// Broadcast blocks at height=2.
	broadcast(b02)
	broadcast(b12)
	broadcast(b22)
	broadcast(b32)

	// Verify the cached status of each app.
	verify := func(app *test.App) {
		// Check blocks that are strongly acked.
		req.Contains(app.Acked, b00.Hash)
		req.Contains(app.Acked, b10.Hash)
		req.Contains(app.Acked, b20.Hash)
		req.Contains(app.Acked, b30.Hash)
		req.Contains(app.Acked, b01.Hash)
		req.Contains(app.Acked, b11.Hash)
		req.Contains(app.Acked, b21.Hash)
		req.Contains(app.Acked, b31.Hash)
		// Genesis blocks are delivered by total ordering as a set.
		delivered0 := common.Hashes{b00.Hash, b10.Hash, b20.Hash, b30.Hash}
		sort.Sort(delivered0)
		req.Len(app.TotalOrdered, 2)
		req.Equal(app.TotalOrdered[0].BlockHashes, delivered0)
		req.False(app.TotalOrdered[0].Early)
		// b11 is the sencond set delivered by total ordering.
		delivered1 := common.Hashes{b11.Hash}
		sort.Sort(delivered1)
		req.Equal(app.TotalOrdered[1].BlockHashes, delivered1)
		req.False(app.TotalOrdered[1].Early)
		// Check generated timestamps.
		req.Contains(app.Delivered, b00.Hash)
		req.Contains(app.Delivered, b10.Hash)
		req.Contains(app.Delivered, b20.Hash)
		req.Contains(app.Delivered, b30.Hash)
		req.Contains(app.Delivered, b11.Hash)
		// Check timestamps, there is no direct way to know which block is
		// selected as main chain, we can only detect it by making sure
		// its ConsensusTimestamp is not interpolated.
		timestamps := make([]time.Time, 4)
		timestamps[0] = b00.Timestamp
		timestamps[1] = b10.Timestamp
		timestamps[2] = b20.Timestamp
		timestamps[3] = b30.Timestamp
		t, err := getMedianTime(timestamps)
		req.Nil(err)
		req.Equal(t, app.Delivered[b11.Hash].ConsensusTime)
	}
	for _, obj := range objs {
		obj.con.nbModule.wait()
		verify(obj.app)
	}
}

func (s *ConsensusTestSuite) TestPrepareBlock() {
	// This test case would test these steps:
	//  - Add all genesis blocks into lattice.
	//  - Make sure Consensus.prepareBlock would attempt to ack
	//    all genesis blocks.
	//  - Add the prepared block into lattice.
	//  - Make sure Consensus.prepareBlock would only attempt to
	//    ack the prepared block.
	var (
		gov, err = test.NewGovernance(4, time.Second)
		req      = s.Require()
		nodes    []types.NodeID
	)
	s.Require().Nil(err)
	for nID := range gov.GetNotarySet() {
		nodes = append(nodes, nID)
	}
	// Setup core.Consensus and test.App.
	cons := map[types.NodeID]*Consensus{}
	for _, nID := range nodes {
		_, con := s.prepareConsensus(gov, nID)
		cons[nID] = con
	}
	b00 := s.prepareGenesisBlock(nodes[0], 0, cons[nodes[0]])
	b10 := s.prepareGenesisBlock(nodes[1], 1, cons[nodes[1]])
	b20 := s.prepareGenesisBlock(nodes[2], 2, cons[nodes[2]])
	b30 := s.prepareGenesisBlock(nodes[3], 3, cons[nodes[3]])
	for _, con := range cons {
		req.Nil(con.processBlock(b00))
		req.Nil(con.processBlock(b10))
		req.Nil(con.processBlock(b20))
		req.Nil(con.processBlock(b30))
	}
	b11 := &types.Block{
		ProposerID: nodes[1],
	}
	// Sleep to make sure 'now' is slower than b10's timestamp.
	time.Sleep(100 * time.Millisecond)
	req.Nil(cons[nodes[1]].prepareBlock(b11, time.Now().UTC()))
	// Make sure we would assign 'now' to the timestamp belongs to
	// the proposer.
	req.True(
		b11.Timestamp.Sub(
			b10.Timestamp) > 100*time.Millisecond)
	for _, con := range cons {
		req.Nil(con.processBlock(b11))
	}
	b12 := &types.Block{
		ProposerID: nodes[1],
	}
	req.Nil(cons[nodes[1]].prepareBlock(b12, time.Now().UTC()))
	req.Len(b12.Acks, 1)
	req.Contains(b12.Acks, b11.Hash)
}

func (s *ConsensusTestSuite) TestPrepareGenesisBlock() {
	var (
		gov, err = test.NewGovernance(4, time.Second)
		nodes    []types.NodeID
	)
	s.Require().Nil(err)
	for nID := range gov.GetNotarySet() {
		nodes = append(nodes, nID)
	}
	_, con := s.prepareConsensus(gov, nodes[0])
	block := &types.Block{
		ProposerID: nodes[0],
	}
	con.PrepareGenesisBlock(block, time.Now().UTC())
	s.True(block.IsGenesis())
	s.Nil(con.sanityCheck(block))
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
