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
	"log"
	"os"
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

// A round event handler to purge utils.NodeSetCache in test.Network.
func purgeHandlerGen(n *test.Network) func([]utils.RoundEventParam) {
	return func(evts []utils.RoundEventParam) {
		for _, e := range evts {
			if e.Reset == 0 {
				continue
			}
			n.PurgeNodeSetCache(e.Round + 1)
		}
	}
}

func govHandlerGen(
	round, reset uint64,
	g *test.Governance,
	doer func(*test.Governance)) func([]utils.RoundEventParam) {
	return func(evts []utils.RoundEventParam) {
		for _, e := range evts {
			if e.Round == round && e.Reset == reset {
				doer(g)
			}
		}
	}

}

type node struct {
	ID      types.NodeID
	con     *core.Consensus
	app     *test.App
	gov     *test.Governance
	rEvt    *utils.RoundEvent
	db      db.Database
	network *test.Network
	logger  common.Logger
}

func prohibitDKG(gov *test.Governance) {
	gov.Prohibit(test.StateAddDKGMasterPublicKey)
	gov.Prohibit(test.StateAddDKGFinal)
	gov.Prohibit(test.StateAddDKGComplaint)
}

func prohibitDKGExceptFinalize(gov *test.Governance) {
	gov.Prohibit(test.StateAddDKGMasterPublicKey)
	gov.Prohibit(test.StateAddDKGComplaint)
}

func unprohibitDKG(gov *test.Governance) {
	gov.Unprohibit(test.StateAddDKGMasterPublicKey)
	gov.Unprohibit(test.StateAddDKGFinal)
	gov.Unprohibit(test.StateAddDKGComplaint)
}

func (s *ConsensusTestSuite) setupNodes(
	dMoment time.Time,
	prvKeys []crypto.PrivateKey,
	seedGov *test.Governance) map[types.NodeID]*node {
	var (
		wg        sync.WaitGroup
		initRound uint64
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
		// Prepare essential modules: app, gov, db.
		networkModule := test.NewNetwork(k.PublicKey(), test.NetworkConfig{
			Type:          test.NetworkTypeFake,
			DirectLatency: &test.FixedLatencyModel{},
			GossipLatency: &test.FixedLatencyModel{},
			Marshaller:    test.NewDefaultMarshaller(nil)},
		)
		gov := seedGov.Clone()
		gov.SwitchToRemoteMode(networkModule)
		gov.NotifyRound(initRound, types.GenesisHeight)
		networkModule.AttachNodeSetCache(utils.NewNodeSetCache(gov))
		f, err := os.Create(fmt.Sprintf("log.%d.log", i))
		if err != nil {
			panic(err)
		}
		logger := common.NewCustomLogger(log.New(f, "", log.LstdFlags|log.Lmicroseconds))
		rEvt, err := utils.NewRoundEvent(context.Background(), gov, logger,
			types.Position{Height: types.GenesisHeight}, core.ConfigRoundShift)
		s.Require().NoError(err)
		nID := types.NewNodeID(k.PublicKey())
		nodes[nID] = &node{
			ID:      nID,
			app:     test.NewApp(initRound+1, gov, rEvt),
			gov:     gov,
			db:      dbInst,
			logger:  logger,
			rEvt:    rEvt,
			network: networkModule,
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
	r, err := test.NewBlockRevealerByPosition(DBAll, nextSyncHeight)
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
			// Sync app.
			syncNode.app.BlockConfirmed(*b)
			syncNode.app.BlockDelivered(b.Hash, b.Position, b.Randomness)
			// Sync gov.
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
		syncerHeight = b.Position.Height + 1
		compactionChainBlocks = append(compactionChainBlocks, &b)
		if len(compactionChainBlocks) >= 20 {
			if syncBlocks() {
				break
			}
		}
	}
	return
}

func (s *ConsensusTestSuite) TestSimple() {
	if testing.Short() {
		// All other tests will cover this basic case. To speed up CI process,
		// ignore this test in short mode.
		return
	}
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
		test.NewState(core.DKGDelayRound,
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	// A short round interval.
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	for _, n := range nodes {
		go n.con.Run()
		defer n.con.Stop()
	}
Loop:
	for {
		<-time.After(5 * time.Second)
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

func (s *ConsensusTestSuite) TestSetSizeChange() {
	var (
		req        = s.Require()
		peerCount  = 7
		dMoment    = time.Now().UTC()
		untilRound = uint64(5)
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
		test.NewState(core.DKGDelayRound, pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNotarySetSize, uint32(4)))
	seedGov.CatchUpWithRound(0)
	// Setup configuration for round 0 and round 1.
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNotarySetSize, uint32(5)))
	seedGov.CatchUpWithRound(1)
	// Setup configuration for round 2.
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNotarySetSize, uint32(6)))
	seedGov.CatchUpWithRound(2)
	// Setup configuration for round 3.
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNotarySetSize, uint32(4)))
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
		4, test.StateChangeRoundLength, uint64(100)))
	req.NoError(pickedNode.gov.RegisterConfigChange(
		4, test.StateChangeNotarySetSize, uint32(5)))
	// Register configuration changes for round 5.
	req.NoError(pickedNode.gov.RegisterConfigChange(
		5, test.StateChangeRoundLength, uint64(60)))
	req.NoError(pickedNode.gov.RegisterConfigChange(
		5, test.StateChangeNotarySetSize, uint32(4)))
	// Run test.
	for _, n := range nodes {
		go n.con.Run()
		defer n.con.Stop()
	}
Loop:
	for {
		<-time.After(5 * time.Second)
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
	// - One DKG reset happened before syncing.
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(6)
		stopRound  = uint64(4)
		// aliveRound should be large enough to test round event handling in
		// syncer.
		aliveRound = uint64(2)
		errChan    = make(chan error, 100)
	)
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	seedGov, err := test.NewGovernance(
		test.NewState(core.DKGDelayRound,
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	seedGov.CatchUpWithRound(0)
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
		n.rEvt.Register(purgeHandlerGen(n.network))
		// Round Height reference table:
		// - Round:1 Reset:0 -- 100
		// - Round:1 Reset:1 -- 200
		// - Round:2 Reset:0 -- 300
		n.rEvt.Register(govHandlerGen(1, 0, n.gov, prohibitDKG))
		n.rEvt.Register(govHandlerGen(1, 1, n.gov, unprohibitDKG))
		if n.ID != syncNode.ID {
			go n.con.Run()
			if n.ID != stoppedNode.ID {
				defer n.con.Stop()
			}
		}
	}
	// Clean syncNode's network receive channel, or it might exceed the limit
	// and block other go routines.
	dummyReceiverCtxCancel, dummyFinished := utils.LaunchDummyReceiver(
		context.Background(), syncNode.network.ReceiveChan(), nil)
ReachAlive:
	for {
		// Check if any error happened or sleep for a period of time.
		select {
		case err := <-errChan:
			req.NoError(err)
		case <-time.After(5 * time.Second):
		}
		// If all nodes excepts syncNode have reached aliveRound, call syncNode's
		// Run() and send it all blocks in one of normal node's compaction chain.
		for id, n := range nodes {
			if id == syncNode.ID {
				continue
			}
			pos := n.app.GetLatestDeliveredPosition()
			if pos.Round < aliveRound {
				fmt.Println("latestPos", n.ID, &pos)
				continue ReachAlive
			}
		}
		dummyReceiverCtxCancel()
		<-dummyFinished
		break
	}
	// Initiate Syncer.
	runnerCtx, runnerCtxCancel := context.WithCancel(context.Background())
	defer runnerCtxCancel()
	f, err := os.Create("log.sync.log")
	if err != nil {
		panic(err)
	}
	logger := common.NewCustomLogger(log.New(f, "", log.LstdFlags|log.Lmicroseconds))
	syncerObj := syncer.NewConsensus(
		0,
		dMoment,
		syncNode.app,
		syncNode.gov,
		syncNode.db,
		syncNode.network,
		prvKeys[0],
		logger,
	)
	// Initialize communication channel, it's not recommended to assertion in
	// another go routine.
	go func() {
		var (
			syncedHeight uint64 = 1
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
				go func() {
					<-runnerCtx.Done()
					syncNode.con.Stop()
				}()
				break SyncLoop
			}
			if err != nil {
				errChan <- err
				break SyncLoop
			}
			select {
			case <-runnerCtx.Done():
				break SyncLoop
			case <-time.After(4 * time.Second):
			}
		}
	}()
	// Wait until all nodes reach 'untilRound'.
	var stoppedRound uint64
	go func() {
		n, pos := stoppedNode, stoppedNode.app.GetLatestDeliveredPosition()
	ReachFinished:
		for {
			fmt.Println("latestPos", n.ID, &pos)
			time.Sleep(5 * time.Second)
			if stoppedNode.con != nil {
				pos = n.app.GetLatestDeliveredPosition()
				if pos.Round >= stopRound {
					// Stop a node, we should still be able to proceed.
					stoppedNode.con.Stop()
					stoppedNode.con = nil
					stoppedRound = pos.Round
					fmt.Println("one node stopped", stoppedNode.ID)
					utils.LaunchDummyReceiver(
						runnerCtx, stoppedNode.network.ReceiveChan(), nil)
				}
			}
			for _, n = range nodes {
				if n.ID == stoppedNode.ID {
					continue
				}
				pos = n.app.GetLatestDeliveredPosition()
				if pos.Round < untilRound {
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
	}
	s.Require().Equal(stoppedRound, stopRound)
}

func (s *ConsensusTestSuite) TestForceSync() {
	// The sync test case:
	// - No configuration change.
	// - One node does not run when all others starts until aliveRound exceeded.
	var (
		req        = s.Require()
		peerCount  = 4
		dMoment    = time.Now().UTC()
		untilRound = uint64(3)
		stopRound  = uint64(1)
		errChan    = make(chan error, 100)
	)
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	seedGov, err := test.NewGovernance(
		test.NewState(core.DKGDelayRound,
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	seedGov.CatchUpWithRound(0)
	seedGov.CatchUpWithRound(1)
	// A short round interval.
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	for _, n := range nodes {
		go n.con.Run()
	}
ReachStop:
	for {
		// Check if any error happened or sleep for a period of time.
		select {
		case err := <-errChan:
			req.NoError(err)
		case <-time.After(5 * time.Second):
		}
		// If one of the nodes have reached stopRound, stop all nodes to simulate
		// crash.
		for _, n := range nodes {
			pos := n.app.GetLatestDeliveredPosition()
			if pos.Round >= stopRound {
				break ReachStop
			} else {
				fmt.Println("latestPos", n.ID, &pos)
			}
		}
	}

	var latestHeight uint64
	var latestNodeID types.NodeID
	for _, n := range nodes {
		n.con.Stop()
		time.Sleep(1 * time.Second)
	}
	for nID, n := range nodes {
		_, height := n.db.GetCompactionChainTipInfo()
		if height > latestHeight {
			fmt.Println("Newer height", nID, height)
			latestNodeID = nID
			latestHeight = height
		}
	}
	fmt.Println("Latest node", latestNodeID, latestHeight)
	for nID, node := range nodes {
		if nID == latestNodeID {
			continue
		}
		fmt.Printf("[%p] Clearing %s %s\n", node.app, nID, node.app.GetLatestDeliveredPosition())
		node.app.ClearUndeliveredBlocks()
	}
	syncerCon := make(map[types.NodeID]*syncer.Consensus, len(nodes))
	for i, prvKey := range prvKeys {
		f, err := os.Create(fmt.Sprintf("log.sync.%d.log", i))
		if err != nil {
			panic(err)
		}
		logger := common.NewCustomLogger(log.New(f, "", log.LstdFlags|log.Lmicroseconds))
		nID := types.NewNodeID(prvKey.PublicKey())
		node := nodes[nID]
		syncerCon[nID] = syncer.NewConsensus(
			latestHeight,
			dMoment,
			node.app,
			node.gov,
			node.db,
			node.network,
			prvKey,
			logger,
		)
	}
	targetNode := nodes[latestNodeID]
	for nID, node := range nodes {
		if nID == latestNodeID {
			continue
		}
		syncedHeight := node.app.GetLatestDeliveredPosition().Height
		syncedHeight++
		var err error
		for {
			fmt.Println("Syncing", nID, syncedHeight)
			if syncedHeight >= latestHeight {
				break
			}
			_, syncedHeight, err = s.syncBlocksWithSomeNode(
				targetNode, node, syncerCon[nID], syncedHeight)
			if err != nil {
				panic(err)
			}
			fmt.Println("Syncing after", nID, syncedHeight)
		}
		fmt.Println("Synced", nID, syncedHeight)
	}
	// Make sure all nodes are synced in db and app.
	_, latestHeight = targetNode.db.GetCompactionChainTipInfo()
	latestPos := targetNode.app.GetLatestDeliveredPosition()
	for _, node := range nodes {
		_, height := node.db.GetCompactionChainTipInfo()
		s.Require().Equal(height, latestHeight)
		pos := node.app.GetLatestDeliveredPosition()
		s.Require().Equal(latestPos, pos)
	}
	for _, con := range syncerCon {
		con.ForceSync(latestPos, true)
	}
	for nID := range nodes {
		con, err := syncerCon[nID].GetSyncedConsensus()
		s.Require().NoError(err)
		nodes[nID].con = con
	}
	for _, node := range nodes {
		go node.con.Run()
		defer node.con.Stop()
	}

Loop:
	for {
		<-time.After(5 * time.Second)
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

func (s *ConsensusTestSuite) TestResetDKG() {
	var (
		req        = s.Require()
		peerCount  = 5
		dMoment    = time.Now().UTC()
		untilRound = uint64(3)
	)
	prvKeys, pubKeys, err := test.NewKeys(peerCount)
	req.NoError(err)
	// Setup seed governance instance. Give a short latency to make this test
	// run faster.
	seedGov, err := test.NewGovernance(
		test.NewState(core.DKGDelayRound,
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeRoundLength, uint64(100)))
	req.NoError(seedGov.State().RequestChange(
		test.StateChangeNotarySetSize, uint32(4)))
	nodes := s.setupNodes(dMoment, prvKeys, seedGov)
	for _, n := range nodes {
		n.rEvt.Register(purgeHandlerGen(n.network))
		// Round Height reference table:
		// - Round:1 Reset:0 -- 100
		// - Round:1 Reset:1 -- 200
		// - Round:1 Reset:2 -- 300
		// - Round:2 Reset:0 -- 400
		// - Round:2 Reset:1 -- 500
		// - Round:3 Reset:0 -- 600
		n.rEvt.Register(govHandlerGen(1, 0, n.gov, prohibitDKG))
		n.rEvt.Register(govHandlerGen(1, 2, n.gov, unprohibitDKG))
		n.rEvt.Register(govHandlerGen(2, 0, n.gov, prohibitDKGExceptFinalize))
		n.rEvt.Register(govHandlerGen(2, 1, n.gov, unprohibitDKG))
		go n.con.Run()
	}
Loop:
	for {
		<-time.After(5 * time.Second)
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
	for _, n := range nodes {
		n.con.Stop()
		req.Equal(n.gov.DKGResetCount(2), uint64(2))
		req.Equal(n.gov.DKGResetCount(3), uint64(1))
	}
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
