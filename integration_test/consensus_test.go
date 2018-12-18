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
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/syncer"
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
	ID      types.NodeID
	con     *core.Consensus
	app     *test.App
	gov     *test.Governance
	db      db.Database
	network *test.Network
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
		dbInst, err := db.NewMemBackedDB()
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
			dbInst,
			networkModule,
			k,
			&common.NullLogger{},
		)
		nodes[con.ID] = &node{con.ID, con, app, gov, dbInst, networkModule}
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

func (s *ConsensusTestSuite) syncBlocksWithSomeNode(
	sourceNode, syncNode *node,
	syncerObj *syncer.Consensus,
	nextSyncHeight uint64) (
	syncedCon *core.Consensus, syncerHeight uint64, err error) {

	syncerHeight = nextSyncHeight
	// Setup revealer.
	DBAll, err := sourceNode.db.GetAllBlocks()
	if err != nil {
		return
	}
	r, err := test.NewCompactionChainBlockRevealer(DBAll, nextSyncHeight)
	if err != nil {
		return
	}
	// Load all blocks from revealer and dump them into syncer.
	var compactionChainBlocks []*types.Block
	syncBlocks := func() (done bool) {
		// Apply txs in blocks to make sure our governance instance is ready.
		// This action should be performed by fullnode in production mode.
		for _, b := range compactionChainBlocks {
			if err = syncNode.gov.State().Apply(b.Payload); err != nil {
				if err != test.ErrDuplicatedChange {
					return
				}
				err = nil
			}
			syncNode.gov.CatchUpWithRound(
				b.Position.Round + core.ConfigRoundShift)
		}
		var synced bool
		synced, err = syncerObj.SyncBlocks(compactionChainBlocks, true)
		if err != nil {
			done = true
		}
		if synced {
			syncedCon, err = syncerObj.GetSyncedConsensus()
			done = true
		}
		compactionChainBlocks = nil
		return
	}
	for {
		var b types.Block
		b, err = r.NextBlock()
		if err != nil {
			if err == db.ErrIterationFinished {
				err = nil
				if syncBlocks() {
					break
				}
			}
			break
		}
		syncerHeight = b.Finalization.Height + 1
		compactionChainBlocks = append(compactionChainBlocks, &b)
		if len(compactionChainBlocks) >= 100 {
			if syncBlocks() {
				break
			}
		}
	}
	return
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
		test.NewState(
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundInterval, 50*time.Second))
	// A short round interval.
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	for _, n := range nodes {
		go n.con.Run()
	}
Loop:
	for {
		<-time.After(5 * time.Second)
		fmt.Println("check latest position delivered by each node")
		for _, n := range nodes {
			latestPos := n.app.GetLatestDeliveredPosition()
			fmt.Println("latestPos", n.ID, &latestPos)
			if latestPos.Round < untilRound {
				continue Loop
			}
		}
		// Oh ya.
		break
	}
	s.verifyNodes(nodes)
	// TODO(haoping) stop consensus.
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
		test.NewState(
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
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
		go n.con.Run()
	}
Loop:
	for {
		<-time.After(5 * time.Second)
		fmt.Println("check latest position delivered by each node")
		for _, n := range nodes {
			latestPos := n.app.GetLatestDeliveredPosition()
			fmt.Println("latestPos", n.ID, &latestPos)
			if latestPos.Round < untilRound {
				continue Loop
			}
		}
		// Oh ya.
		break
	}
	s.verifyNodes(nodes)
}

func (s *ConsensusTestSuite) TestSync() {
	// The sync test case:
	// - No configuration change.
	// - One node does not run when all others starts until aliveRound exceeded.
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(5)
		stopRound  = uint64(3)
		aliveRound = uint64(1)
		errChan    = make(chan error, 100)
	)
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	seedGov, err := test.NewGovernance(
		test.NewState(
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundInterval, 50*time.Second))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNumChains, uint32(5)))
	seedGov.CatchUpWithRound(0)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNumChains, uint32(4)))
	seedGov.CatchUpWithRound(1)
	// A short round interval.
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	// Choose the first node as "syncNode" that its consensus' Run() is called
	// later.
	syncNode := nodes[types.NewNodeID(pubKeys[0])]
	syncNode.con = nil
	// Pick a node to stop when synced.
	stoppedNode := nodes[types.NewNodeID(pubKeys[1])]
	for _, n := range nodes {
		if n.ID != syncNode.ID {
			go n.con.Run()
		}
	}
	// Clean syncNode's network receive channel, or it might exceed the limit
	// and block other go routines.
	dummyReceiverCtx, dummyReceiverCtxCancel := context.WithCancel(
		context.Background())
	go func() {
	Loop:
		for {
			select {
			case <-syncNode.network.ReceiveChan():
			case <-dummyReceiverCtx.Done():
				break Loop
			}
		}
	}()
	// Print status every 5 seconds so CI won't fail.
	monitorCtx, monitorCancel := context.WithCancel(
		context.Background())
	defer monitorCancel()
	go func() {
		for {
			select {
			case <-time.After(5 * time.Second):
				for _, n := range nodes {
					pos := n.app.GetLatestDeliveredPosition()
					fmt.Println("latestPos", n.ID, &pos)
					break
				}
			case <-monitorCtx.Done():
				return
			}
		}
	}()
ReachAlive:
	for {
		// If all nodes excepts syncNode have reached aliveRound, call syncNode's
		// Run() and send it all blocks in one of normal node's compaction chain.
		for id, n := range nodes {
			if id == syncNode.ID {
				continue
			}
			if n.app.GetLatestDeliveredPosition().Round < aliveRound {
				continue ReachAlive
			}
			// Check if any error happened or sleep for a period of time.
			select {
			case err := <-errChan:
				req.NoError(err)
			case <-time.After(5 * time.Second):
			}
		}
		dummyReceiverCtxCancel()
		break
	}
	// Initiate Syncer.
	runnerCtx, runnerCtxCancel := context.WithCancel(context.Background())
	defer runnerCtxCancel()
	syncerObj := syncer.NewConsensus(
		dMoment,
		syncNode.app,
		syncNode.gov,
		syncNode.db,
		syncNode.network,
		prvKeys[0],
		&common.NullLogger{},
	)
	// Initialize communication channel, it's not recommended to assertion in
	// another go routine.
	go func() {
		var (
			syncedHeight uint64
			err          error
			syncedCon    *core.Consensus
		)
	SyncLoop:
		for {
			syncedCon, syncedHeight, err = s.syncBlocksWithSomeNode(
				stoppedNode, syncNode, syncerObj, syncedHeight)
			if syncedCon != nil {
				syncNode.con = syncedCon
				go syncNode.con.Run()
				break SyncLoop
			}
			if err != nil {
				errChan <- err
				break SyncLoop
			}
			select {
			case <-runnerCtx.Done():
				break SyncLoop
			case <-time.After(2 * time.Second):
			}
		}
	}()
	// Wait until all nodes reach 'untilRound'.
	go func() {
	ReachFinished:
		for {
			time.Sleep(5 * time.Second)
			for _, n := range nodes {
				if n.ID == stoppedNode.ID {
					if n.con == nil {
						continue
					}
					if n.app.GetLatestDeliveredPosition().Round < stopRound {
						continue
					}
					// Stop a node, we should still be able to proceed.
					stoppedNode.con.Stop()
					stoppedNode.con = nil
					fmt.Println("one node stopped", stoppedNode.ID)
					// Initiate a dummy routine to consume the receive channel.
					go func() {
						for {
							select {
							case <-runnerCtx.Done():
								return
							case <-stoppedNode.network.ReceiveChan():
							}
						}
					}()
					continue
				}
				if n.app.GetLatestDeliveredPosition().Round < untilRound {
					continue ReachFinished
				}
			}
			break
		}
		runnerCtxCancel()
	}()
	// Block until any reasonable testing milestone reached.
	select {
	case err := <-errChan:
		req.NoError(err)
	case <-runnerCtx.Done():
		// This test passed.
		// TODO(haoping) stop consensus.
	}
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
