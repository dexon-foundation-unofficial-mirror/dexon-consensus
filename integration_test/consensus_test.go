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

package integration

import (
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

// There is no scheduler in these tests, we need to wait a long period to make
// sure these tests are ok.
type ConsensusTestSuite struct {
	suite.Suite
}

type node struct {
	con *core.Consensus
	app *test.App
	gov *test.Governance
	db  blockdb.BlockDatabase
}

func (s *ConsensusTestSuite) setupNodes(
	dMoment time.Time,
	prvKeys []crypto.PrivateKey,
	seedGov *test.Governance) map[types.NodeID]*node {
	var (
		wg sync.WaitGroup
	)
	// Setup peer server at transport layer.
	server := test.NewFakeTransportServer()
	serverChannel, err := server.Host()
	s.Require().NoError(err)
	// setup nodes.
	nodes := make(map[types.NodeID]*node)
	wg.Add(len(prvKeys))
	for _, k := range prvKeys {
		db, err := blockdb.NewMemBackedBlockDB()
		s.Require().NoError(err)
		// Prepare essential modules: app, gov, db.
		networkModule := test.NewNetwork(
			k.PublicKey(),
			&test.FixedLatencyModel{},
			test.NewDefaultMarshaller(nil),
			test.NetworkConfig{Type: test.NetworkTypeFake})
		gov := seedGov.Clone()
		gov.SwitchToRemoteMode(networkModule)
		gov.NotifyRoundHeight(0, 0)
		networkModule.AddNodeSetCache(utils.NewNodeSetCache(gov))
		app := test.NewApp(gov.State())
		// Now is the consensus module.
		con := core.NewConsensus(
			dMoment,
			app,
			gov,
			db,
			networkModule,
			k,
			&common.NullLogger{})
		nodes[con.ID] = &node{con, app, gov, db}
		go func() {
			defer wg.Done()
			s.Require().NoError(networkModule.Setup(serverChannel))
			go networkModule.Run()
		}()
	}
	// Make sure transport layer is ready.
	s.Require().NoError(server.WaitForPeers(uint32(len(prvKeys))))
	wg.Wait()
	return nodes
}

func (s *ConsensusTestSuite) verifyNodes(nodes map[types.NodeID]*node) {
	for ID, node := range nodes {
		s.Require().NoError(node.app.Verify())
		for otherID, otherNode := range nodes {
			if ID == otherID {
				continue
			}
			s.Require().NoError(node.app.Compare(otherNode.app))
		}
	}
}

func (s *ConsensusTestSuite) TestSimple() {
	// The simplest test case:
	//  - Node set is equals to DKG set and notary set for each chain in each
	//    round.
	//  - No configuration change.
	//  - 4 rounds (0, 1 are genesis rounds, round 2 would be ready when the
	//    first block delivered. Test until round 3 should be enough.
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(5)
	)
	if testing.Short() {
		untilRound = 2
	}
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	seedGov, err := test.NewGovernance(
		pubKeys, 100*time.Millisecond, core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundInterval, 50*time.Second))
	// A short round interval.
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	for _, n := range nodes {
		go n.con.Run(&types.Block{})
	}
Loop:
	for {
		<-time.After(5 * time.Second)
		s.T().Log("check latest position delivered by each node")
		for _, n := range nodes {
			latestPos := n.app.GetLatestDeliveredPosition()
			s.T().Log("latestPos", n.con.ID, &latestPos)
			if latestPos.Round < untilRound {
				continue Loop
			}
		}
		// Oh ya.
		break
	}
	s.verifyNodes(nodes)
}

func (s *ConsensusTestSuite) TestNumChainsChange() {
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(6)
	)
	if testing.Short() {
		// Short test won't test configuration change packed as payload of
		// blocks and applied when delivered.
		untilRound = 5
	}
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance.
	seedGov, err := test.NewGovernance(
		pubKeys, 100*time.Millisecond, core.ConfigRoundShift)
	req.NoError(err)
	// Setup configuration for round 0 and round 1.
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundInterval, 45*time.Second))
	seedGov.CatchUpWithRound(1)
	// Setup configuration for round 2.
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNumChains, uint32(5)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundInterval, 55*time.Second))
	seedGov.CatchUpWithRound(2)
	// Setup configuration for round 3.
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNumChains, uint32(6)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundInterval, 75*time.Second))
	seedGov.CatchUpWithRound(3)
	// Setup nodes.
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	// Pick master node, and register changes on it.
	var pickedNode *node
	for _, pickedNode = range nodes {
		break
	}
	// Register configuration changes for round 4.
	req.NoError(pickedNode.gov.RegisterConfigChange(
		4, test.StateChangeNumChains, uint32(4)))
	req.NoError(pickedNode.gov.RegisterConfigChange(
		4, test.StateChangeRoundInterval, 45*time.Second))
	// Register configuration changes for round 5.
	req.NoError(pickedNode.gov.RegisterConfigChange(
		5, test.StateChangeNumChains, uint32(5)))
	req.NoError(pickedNode.gov.RegisterConfigChange(
		5, test.StateChangeRoundInterval, 55*time.Second))
	// Run test.
	for _, n := range nodes {
		go n.con.Run(&types.Block{})
	}
Loop:
	for {
		<-time.After(5 * time.Second)
		s.T().Log("check latest position delivered by each node")
		for _, n := range nodes {
			latestPos := n.app.GetLatestDeliveredPosition()
			s.T().Log("latestPos", n.con.ID, &latestPos)
			if latestPos.Round < untilRound {
				continue Loop
			}
		}
		// Oh ya.
		break
	}
	s.verifyNodes(nodes)
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
