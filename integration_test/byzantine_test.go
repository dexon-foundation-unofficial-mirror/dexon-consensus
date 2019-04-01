// Copyright 2019 The dexon-consensus Authors
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
	"context"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

// There is no scheduler in these tests, we need to wait a long period to make
// sure these tests are ok.
type ByzantineTestSuite struct {
	suite.Suite

	directLatencyModel map[types.NodeID]test.LatencyModel
}

func (s *ByzantineTestSuite) SetupTest() {
	s.directLatencyModel = make(map[types.NodeID]test.LatencyModel)
}

func (s *ByzantineTestSuite) setupNodes(
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
	for i, k := range prvKeys {
		dbInst, err := db.NewMemBackedDB()
		s.Require().NoError(err)
		nID := types.NewNodeID(k.PublicKey())
		// Prepare essential modules: app, gov, db.
		var directLatencyModel test.LatencyModel
		if model, exist := s.directLatencyModel[nID]; exist {
			directLatencyModel = model
		} else {
			directLatencyModel = &test.FixedLatencyModel{}
		}
		networkModule := test.NewNetwork(k.PublicKey(), test.NetworkConfig{
			Type:          test.NetworkTypeFake,
			DirectLatency: directLatencyModel,
			GossipLatency: &test.FixedLatencyModel{},
			Marshaller:    test.NewDefaultMarshaller(nil)},
		)
		gov := seedGov.Clone()
		gov.SwitchToRemoteMode(networkModule)
		gov.NotifyRound(0, types.GenesisHeight)
		networkModule.AttachNodeSetCache(utils.NewNodeSetCache(gov))
		f, err := os.Create(fmt.Sprintf("log.%d.log", i))
		if err != nil {
			panic(err)
		}
		logger := common.NewCustomLogger(log.New(f, "", log.LstdFlags|log.Lmicroseconds))
		app := test.NewApp(1, gov, nil)
		nodes[nID] = &node{
			ID:      nID,
			app:     app,
			gov:     gov,
			db:      dbInst,
			network: networkModule,
			logger:  logger,
		}
		go func() {
			defer wg.Done()
			s.Require().NoError(networkModule.Setup(serverChannel))
			go networkModule.Run()
		}()
	}
	// Make sure transport layer is ready.
	s.Require().NoError(server.WaitForPeers(uint32(len(prvKeys))))
	wg.Wait()
	for _, k := range prvKeys {
		node := nodes[types.NewNodeID(k.PublicKey())]
		// Now is the consensus module.
		node.con = core.NewConsensus(
			dMoment,
			node.app,
			node.gov,
			node.db,
			node.network,
			k,
			node.logger,
		)
	}
	return nodes
}

func (s *ByzantineTestSuite) verifyNodes(nodes map[types.NodeID]*node) {
	for ID, node := range nodes {
		s.Require().NoError(test.VerifyDB(node.db))
		s.Require().NoError(node.app.Verify())
		for otherID, otherNode := range nodes {
			if ID == otherID {
				continue
			}
			s.Require().NoError(node.app.Compare(otherNode.app))
		}
	}
}

func (s *ByzantineTestSuite) TestOneSlowNodeOneDeadNode() {
	// 4 nodes setup with one slow node and one dead node.
	// The network of slow node is very slow.
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(3)
	)
	if testing.Short() {
		untilRound = 1
	}
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	lambda := 100 * time.Millisecond
	seedGov, err := test.NewGovernance(
		test.NewState(core.DKGDelayRound,
			pubKeys, lambda, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	slowNodeID := types.NewNodeID(pubKeys[0])
	deadNodeID := types.NewNodeID(pubKeys[1])
	s.directLatencyModel[slowNodeID] = &test.FixedLatencyModel{
		Latency: lambda.Seconds() * 1000 * 2,
	}
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	for _, n := range nodes {
		if n.ID == deadNodeID {
			continue
		}
		go n.con.Run()
		defer n.con.Stop()
	}
	// Clean deadNode's network receive channel, or it might exceed the limit
	// and block other go routines.
	dummyReceiverCtxCancel, _ := utils.LaunchDummyReceiver(
		context.Background(), nodes[deadNodeID].network.ReceiveChan(), nil)
	defer dummyReceiverCtxCancel()
Loop:
	for {
		<-time.After(5 * time.Second)
		fmt.Println("check latest position delivered by each node")
		for _, n := range nodes {
			if n.ID == deadNodeID {
				continue
			}
			latestPos := n.app.GetLatestDeliveredPosition()
			fmt.Println("latestPos", n.ID, &latestPos)
			if latestPos.Round < untilRound {
				continue Loop
			}
		}
		// Oh ya.
		break
	}
	delete(nodes, deadNodeID)
	s.verifyNodes(nodes)
}

type voteCensor struct{}

func (vc *voteCensor) Censor(msg interface{}) bool {
	_, ok := msg.(*types.Vote)
	return ok
}

func (s *ByzantineTestSuite) TestOneNodeWithoutVote() {
	// 4 nodes setup with one node's votes been censored.
	// so it will always do syncing BA.
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(3)
		tolerence  = uint64(2)
	)
	if testing.Short() {
		untilRound = 2
	}
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	lambda := 100 * time.Millisecond
	seedGov, err := test.NewGovernance(
		test.NewState(core.DKGDelayRound,
			pubKeys, lambda, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	votelessNodeID := types.NewNodeID(pubKeys[0])
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	votelessNode := nodes[votelessNodeID]
	votelessNode.network.SetCensor(&voteCensor{}, &voteCensor{})
	for _, n := range nodes {
		go n.con.Run()
		defer n.con.Stop()
	}
Loop:
	for {
		<-time.After(5 * time.Second)
		fmt.Println("check latest position delivered by voteless node")
		latestPos := votelessNode.app.GetLatestDeliveredPosition()
		fmt.Println("latestPos", votelessNode.ID, &latestPos)
		for _, n := range nodes {
			if n.ID == votelessNodeID {
				continue
			}
			otherPos := n.app.GetLatestDeliveredPosition()
			if otherPos.Newer(latestPos) {
				fmt.Println("otherPos", n.ID, &otherPos)
				s.Require().True(
					otherPos.Height-latestPos.Height <= tolerence)
			}
		}
		if latestPos.Round < untilRound {
			continue Loop
		}
		// Oh ya.
		break
	}
	s.verifyNodes(nodes)
}

func TestByzantine(t *testing.T) {
	suite.Run(t, new(ByzantineTestSuite))
}
