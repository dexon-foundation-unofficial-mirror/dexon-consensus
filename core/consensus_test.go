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
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

// network implements core.Network.
type network struct {
	ch   <-chan interface{}
	nID  types.NodeID
	conn *networkConnection
}

// BroadcastVote broadcasts vote to all nodes in DEXON network.
func (n *network) BroadcastVote(vote *types.Vote) {
	n.conn.broadcast(n.nID, vote)
}

// BroadcastBlock broadcasts block to all nodes in DEXON network.
func (n *network) BroadcastBlock(block *types.Block) {
	n.conn.broadcast(n.nID, block)
}

// BroadcastAgreementResult broadcasts agreement result to DKG set.
func (n *network) BroadcastAgreementResult(
	randRequest *types.AgreementResult) {
	n.conn.broadcast(n.nID, randRequest)
}

// BroadcastRandomnessResult broadcasts rand request to Notary set.
func (n *network) BroadcastRandomnessResult(
	randResult *types.BlockRandomnessResult) {
	n.conn.broadcast(n.nID, randResult)
}

// SendDKGPrivateShare sends PrivateShare to a DKG participant.
func (n *network) SendDKGPrivateShare(
	recv crypto.PublicKey, prvShare *types.DKGPrivateShare) {
	n.conn.send(types.NewNodeID(recv), prvShare)
}

// BroadcastDKGPrivateShare broadcasts PrivateShare to all DKG participants.
func (n *network) BroadcastDKGPrivateShare(
	prvShare *types.DKGPrivateShare) {
	n.conn.broadcast(n.nID, prvShare)
}

// BroadcastDKGPartialSignature broadcasts partialSignature to all
// DKG participants.
func (n *network) BroadcastDKGPartialSignature(
	psig *types.DKGPartialSignature) {
	n.conn.broadcast(n.nID, psig)
}

// ReceiveChan returns a channel to receive messages from DEXON network.
func (n *network) ReceiveChan() <-chan interface{} {
	return n.ch
}

type networkConnection struct {
	channels map[types.NodeID]chan interface{}
}

func (nc *networkConnection) broadcast(from types.NodeID, msg interface{}) {
	for pk, ch := range nc.channels {
		if pk == from {
			continue
		}
		go func(ch chan interface{}) {
			ch <- msg
		}(ch)
	}
}

func (nc *networkConnection) send(to types.NodeID, msg interface{}) {
	ch, exist := nc.channels[to]
	if !exist {
		return
	}
	go func() {
		ch <- msg
	}()
}

func (nc *networkConnection) join(pk crypto.PublicKey) *network {
	ch := make(chan interface{}, 300)
	nID := types.NewNodeID(pk)
	nc.channels[nID] = ch
	return &network{
		ch:   ch,
		nID:  nID,
		conn: nc,
	}
}

type ConsensusTestSuite struct {
	suite.Suite
	conn *networkConnection
}

func (s *ConsensusTestSuite) SetupTest() {
	s.conn = &networkConnection{
		channels: make(map[types.NodeID]chan interface{}),
	}
}

func (s *ConsensusTestSuite) prepareGenesisBlock(
	chainID uint32,
	con *Consensus) *types.Block {

	block := &types.Block{
		Position: types.Position{
			ChainID: chainID,
		},
	}
	err := con.PrepareGenesisBlock(block, time.Now().UTC())
	s.Require().NoError(err)
	return block
}

func (s *ConsensusTestSuite) prepareConsensus(
	gov *test.Governance, prvKey crypto.PrivateKey) (*test.App, *Consensus) {

	app := test.NewApp()
	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)
	con := NewConsensus(app, gov, db, s.conn.join(prvKey.PublicKey()), prvKey)
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
	// - This test case only works for Total Ordering with K=0.
	// - Byzantine Agreement layer is not taken into consideration, every
	//   block is passed to lattice module directly.
	var (
		gov, err    = test.NewGovernance(4, time.Second)
		minInterval = gov.Configuration(0).MinBlockInterval
		req         = s.Require()
		prvKeys     = gov.PrivateKeys()
		nodes       []types.NodeID
	)
	s.Require().Nil(err)
	// Setup core.Consensus and test.App.
	objs := map[types.NodeID]*struct {
		app *test.App
		con *Consensus
	}{}
	for _, key := range prvKeys {
		nID := types.NewNodeID(key.PublicKey())
		app, con := s.prepareConsensus(gov, key)
		objs[nID] = &struct {
			app *test.App
			con *Consensus
		}{app, con}
		nodes = append(nodes, nID)
	}
	// It's a helper function to emit one block
	// to all core.Consensus objects.
	broadcast := func(b *types.Block) {
		for _, obj := range objs {
			h := common.NewRandomHash()
			b.Finalization.Randomness = h[:]
			obj.con.ccModule.registerBlock(b)
			req.Nil(obj.con.processBlock(b))
		}
	}
	// Genesis blocks
	b00 := s.prepareGenesisBlock(0, objs[nodes[0]].con)
	b10 := s.prepareGenesisBlock(1, objs[nodes[1]].con)
	b20 := s.prepareGenesisBlock(2, objs[nodes[2]].con)
	b30 := s.prepareGenesisBlock(3, objs[nodes[3]].con)
	broadcast(b00)
	broadcast(b10)
	broadcast(b20)
	broadcast(b30)
	// Setup b11.
	b11 := &types.Block{
		Position: types.Position{
			ChainID: 1,
		},
	}
	req.NoError(
		objs[nodes[1]].con.prepareBlock(b11, b10.Timestamp.Add(minInterval)))
	req.Len(b11.Acks, 4)
	req.Contains(b11.Acks, b00.Hash)
	req.Contains(b11.Acks, b10.Hash)
	req.Contains(b11.Acks, b20.Hash)
	req.Contains(b11.Acks, b30.Hash)
	broadcast(b11)
	// Setup b01.
	b01 := &types.Block{
		Position: types.Position{
			ChainID: 0,
		},
	}
	req.NoError(
		objs[nodes[0]].con.prepareBlock(b01, b00.Timestamp.Add(minInterval)))
	req.Len(b01.Acks, 4)
	req.Contains(b01.Acks, b00.Hash)
	req.Contains(b01.Acks, b11.Hash)
	req.Contains(b01.Acks, b20.Hash)
	req.Contains(b01.Acks, b30.Hash)
	// Setup b21.
	b21 := &types.Block{
		Position: types.Position{
			ChainID: 2,
		},
	}
	req.NoError(
		objs[nodes[2]].con.prepareBlock(b21, b20.Timestamp.Add(minInterval)))
	req.Len(b21.Acks, 4)
	req.Contains(b21.Acks, b00.Hash)
	req.Contains(b21.Acks, b11.Hash)
	req.Contains(b21.Acks, b20.Hash)
	req.Contains(b21.Acks, b30.Hash)
	// Setup b31.
	b31 := &types.Block{
		Position: types.Position{
			ChainID: 3,
		},
	}
	req.NoError(
		objs[nodes[3]].con.prepareBlock(b31, b30.Timestamp.Add(minInterval)))
	req.Len(b31.Acks, 4)
	req.Contains(b31.Acks, b00.Hash)
	req.Contains(b31.Acks, b11.Hash)
	req.Contains(b31.Acks, b20.Hash)
	req.Contains(b31.Acks, b30.Hash)
	// Broadcast other height=1 blocks.
	broadcast(b01)
	broadcast(b21)
	broadcast(b31)
	// Setup height=2 blocks.
	// Setup b02.
	b02 := &types.Block{
		Position: types.Position{
			ChainID: 0,
		},
	}
	req.NoError(
		objs[nodes[0]].con.prepareBlock(b02, b01.Timestamp.Add(minInterval)))
	req.Len(b02.Acks, 3)
	req.Contains(b02.Acks, b01.Hash)
	req.Contains(b02.Acks, b21.Hash)
	req.Contains(b02.Acks, b31.Hash)
	// Setup b12.
	b12 := &types.Block{
		Position: types.Position{
			ChainID: 1,
		},
	}
	req.NoError(
		objs[nodes[1]].con.prepareBlock(b12, b11.Timestamp.Add(minInterval)))
	req.Len(b12.Acks, 4)
	req.Contains(b12.Acks, b01.Hash)
	req.Contains(b12.Acks, b11.Hash)
	req.Contains(b12.Acks, b21.Hash)
	req.Contains(b12.Acks, b31.Hash)
	// Setup b22.
	b22 := &types.Block{
		Position: types.Position{
			ChainID: 2,
		},
	}
	req.NoError(
		objs[nodes[2]].con.prepareBlock(b22, b21.Timestamp.Add(minInterval)))
	req.Len(b22.Acks, 3)
	req.Contains(b22.Acks, b01.Hash)
	req.Contains(b22.Acks, b21.Hash)
	req.Contains(b22.Acks, b31.Hash)
	// Setup b32.
	b32 := &types.Block{
		Position: types.Position{
			ChainID: 3,
		},
	}
	req.NoError(
		objs[nodes[3]].con.prepareBlock(b32, b31.Timestamp.Add(minInterval)))
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
		req.Len(app.TotalOrdered, 4)
		req.Equal(app.TotalOrdered[0].BlockHashes, delivered0)
		req.False(app.TotalOrdered[0].Early)
		// b11 is the sencond set delivered by total ordering.
		delivered1 := common.Hashes{b11.Hash}
		sort.Sort(delivered1)
		req.Equal(app.TotalOrdered[1].BlockHashes, delivered1)
		req.False(app.TotalOrdered[1].Early)
		// b01, b21, b31 are the third set delivered by total ordering.
		delivered2 := common.Hashes{b01.Hash, b21.Hash, b31.Hash}
		sort.Sort(delivered2)
		req.Equal(app.TotalOrdered[2].BlockHashes, delivered2)
		req.False(app.TotalOrdered[2].Early)
		// b02, b12, b22, b32 are the fourth set delivered by total ordering.
		delivered3 := common.Hashes{b02.Hash, b12.Hash, b22.Hash, b32.Hash}
		sort.Sort(delivered3)
		req.Equal(app.TotalOrdered[3].BlockHashes, delivered3)
		req.False(app.TotalOrdered[3].Early)
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
		req.NoError(err)
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
		prvKeys  = gov.PrivateKeys()
	)
	s.Require().Nil(err)
	// Setup core.Consensus and test.App.
	cons := map[types.NodeID]*Consensus{}
	for _, key := range prvKeys {
		_, con := s.prepareConsensus(gov, key)
		nID := types.NewNodeID(key.PublicKey())
		cons[nID] = con
		nodes = append(nodes, nID)
	}
	b00 := s.prepareGenesisBlock(0, cons[nodes[0]])
	b10 := s.prepareGenesisBlock(1, cons[nodes[1]])
	b20 := s.prepareGenesisBlock(2, cons[nodes[2]])
	b30 := s.prepareGenesisBlock(3, cons[nodes[3]])
	for _, con := range cons {
		req.Nil(con.processBlock(b00))
		req.Nil(con.processBlock(b10))
		req.Nil(con.processBlock(b20))
		req.Nil(con.processBlock(b30))
	}
	b11 := &types.Block{
		Position: types.Position{ChainID: b10.Position.ChainID},
	}
	interval := gov.Configuration(0).MinBlockInterval
	req.Nil(cons[nodes[1]].prepareBlock(b11, b10.Timestamp.Add(interval)))
	for _, con := range cons {
		req.Nil(con.preProcessBlock(b11))
		req.Nil(con.processBlock(b11))
	}
	b12 := &types.Block{
		Position: types.Position{ChainID: b11.Position.ChainID},
	}
	req.Nil(cons[nodes[1]].prepareBlock(b12, b11.Timestamp.Add(interval)))
	req.Len(b12.Acks, 1)
	req.Contains(b12.Acks, b11.Hash)
}

func (s *ConsensusTestSuite) TestPrepareGenesisBlock() {
	gov, err := test.NewGovernance(4, time.Second)
	s.Require().NoError(err)
	prvKey := gov.PrivateKeys()[0]
	_, con := s.prepareConsensus(gov, prvKey)
	block := &types.Block{
		Position: types.Position{ChainID: 0},
	}
	s.Require().NoError(con.PrepareGenesisBlock(block, time.Now().UTC()))
	s.True(block.IsGenesis())
	s.NoError(con.preProcessBlock(block))
}

func (s *ConsensusTestSuite) TestDKGCRS() {
	n := 21
	lambda := time.Duration(200)
	if testing.Short() {
		n = 7
		lambda = 100
	}
	gov, err := test.NewGovernance(n, lambda*time.Millisecond)
	s.Require().Nil(err)
	gov.RoundInterval = 200 * lambda * time.Millisecond
	prvKeys := gov.PrivateKeys()
	cons := map[types.NodeID]*Consensus{}
	for _, key := range prvKeys {
		_, con := s.prepareConsensus(gov, key)
		nID := types.NewNodeID(key.PublicKey())
		cons[nID] = con
		go con.processMsg(con.network.ReceiveChan())
	}
	for _, con := range cons {
		con.runDKGTSIG()
	}
	for _, con := range cons {
		func() {
			con.dkgReady.L.Lock()
			defer con.dkgReady.L.Unlock()
			for con.dkgRunning != 2 {
				con.dkgReady.Wait()
			}
		}()
	}
	crsFinish := make(chan struct{})
	for _, con := range cons {
		go func(con *Consensus) {
			con.runCRS()
			crsFinish <- struct{}{}
		}(con)
	}
	for range cons {
		<-crsFinish
	}
	s.NotNil(gov.CRS(1))
}
func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
