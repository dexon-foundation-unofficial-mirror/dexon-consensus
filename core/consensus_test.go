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

package core

import (
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/stretchr/testify/suite"
)

// network implements core.Network.
type network struct {
	nID  types.NodeID
	conn *networkConnection
}

// PullBlocks tries to pull blocks from the DEXON network.
func (n *network) PullBlocks(common.Hashes) {
}

// PullVotes tries to pull votes from the DEXON network.
func (n *network) PullVotes(types.Position) {
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
	recv crypto.PublicKey, prvShare *typesDKG.PrivateShare) {
	n.conn.send(types.NewNodeID(recv), prvShare)
}

// BroadcastDKGPrivateShare broadcasts PrivateShare to all DKG participants.
func (n *network) BroadcastDKGPrivateShare(
	prvShare *typesDKG.PrivateShare) {
	n.conn.broadcast(n.nID, prvShare)
}

// BroadcastDKGPartialSignature broadcasts partialSignature to all
// DKG participants.
func (n *network) BroadcastDKGPartialSignature(
	psig *typesDKG.PartialSignature) {
	n.conn.broadcast(n.nID, psig)
}

// ReceiveChan returns a channel to receive messages from DEXON network.
func (n *network) ReceiveChan() <-chan interface{} {
	return make(chan interface{})
}

func (nc *networkConnection) broadcast(from types.NodeID, msg interface{}) {
	for nID := range nc.cons {
		if nID == from {
			continue
		}
		nc.send(nID, msg)
	}
}

func (nc *networkConnection) send(to types.NodeID, msg interface{}) {
	con, exist := nc.cons[to]
	if !exist {
		return
	}
	go func() {
		var err error
		// Testify package does not support concurrent call.
		// Use panic() to detact error.
		switch val := msg.(type) {
		case *types.Block:
			err = con.preProcessBlock(val)
		case *types.Vote:
			err = con.ProcessVote(val)
		case *types.AgreementResult:
			err = con.ProcessAgreementResult(val)
		case *types.BlockRandomnessResult:
			err = con.ProcessBlockRandomnessResult(val)
		case *typesDKG.PrivateShare:
			err = con.cfgModule.processPrivateShare(val)
		case *typesDKG.PartialSignature:
			err = con.cfgModule.processPartialSignature(val)
		}
		if err != nil {
			panic(err)
		}
	}()
}

type networkConnection struct {
	s    *ConsensusTestSuite
	cons map[types.NodeID]*Consensus
}

func (nc *networkConnection) newNetwork(nID types.NodeID) *network {
	return &network{
		nID:  nID,
		conn: nc,
	}
}

func (nc *networkConnection) setCon(nID types.NodeID, con *Consensus) {
	nc.cons[nID] = con
}

type ConsensusTestSuite struct {
	suite.Suite
	conn *networkConnection
}

func (s *ConsensusTestSuite) newNetworkConnection() *networkConnection {
	return &networkConnection{
		s:    s,
		cons: make(map[types.NodeID]*Consensus),
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
	dMoment time.Time,
	gov *test.Governance,
	prvKey crypto.PrivateKey,
	conn *networkConnection) (
	*test.App, *Consensus) {

	app := test.NewApp(nil)
	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().NoError(err)
	nID := types.NewNodeID(prvKey.PublicKey())
	network := conn.newNetwork(nID)
	con := NewConsensus(
		dMoment, app, gov, db, network, prvKey, &common.NullLogger{})
	con.ccModule.init(&types.Block{})
	conn.setCon(nID, con)
	return app, con
}

func (s *ConsensusTestSuite) prepareAgreementMgrWithoutRunning(
	con *Consensus, numChains uint32) {
	// This is a workaround to setup agreementMgr.
	con.baMgr.appendConfig(0, &types.Config{
		NumChains:     numChains,
		RoundInterval: time.Hour,
	}, common.NewRandomHash())
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
		req   = s.Require()
		nodes []types.NodeID
		conn  = s.newNetworkConnection()
	)
	prvKeys, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(pubKeys, time.Second, ConfigRoundShift)
	s.Require().NoError(err)
	minInterval := gov.Configuration(0).MinBlockInterval
	// Setup core.Consensus and test.App.
	objs := map[types.NodeID]*struct {
		app *test.App
		con *Consensus
	}{}
	dMoment := time.Now().UTC()
	for _, key := range prvKeys {
		nID := types.NewNodeID(key.PublicKey())
		app, con := s.prepareConsensus(dMoment, gov, key, conn)
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
		req.Contains(app.Confirmed, b00.Hash)
		req.Contains(app.Confirmed, b10.Hash)
		req.Contains(app.Confirmed, b20.Hash)
		req.Contains(app.Confirmed, b30.Hash)
		req.Contains(app.Confirmed, b01.Hash)
		req.Contains(app.Confirmed, b11.Hash)
		req.Contains(app.Confirmed, b21.Hash)
		req.Contains(app.Confirmed, b31.Hash)
		// Genesis blocks are delivered by total ordering as a set.
		delivered0 := common.Hashes{b00.Hash, b10.Hash, b20.Hash, b30.Hash}
		sort.Sort(delivered0)
		req.Len(app.TotalOrdered, 4)
		req.Equal(app.TotalOrdered[0].BlockHashes, delivered0)
		req.Equal(app.TotalOrdered[0].Mode, TotalOrderingModeNormal)
		// b11 is the sencond set delivered by total ordering.
		delivered1 := common.Hashes{b11.Hash}
		sort.Sort(delivered1)
		req.Equal(app.TotalOrdered[1].BlockHashes, delivered1)
		req.Equal(app.TotalOrdered[1].Mode, TotalOrderingModeNormal)
		// b01, b21, b31 are the third set delivered by total ordering.
		delivered2 := common.Hashes{b01.Hash, b21.Hash, b31.Hash}
		sort.Sort(delivered2)
		req.Equal(app.TotalOrdered[2].BlockHashes, delivered2)
		req.Equal(app.TotalOrdered[2].Mode, TotalOrderingModeNormal)
		// b02, b12, b22, b32 are the fourth set delivered by total ordering.
		delivered3 := common.Hashes{b02.Hash, b12.Hash, b22.Hash, b32.Hash}
		sort.Sort(delivered3)
		req.Equal(app.TotalOrdered[3].BlockHashes, delivered3)
		req.Equal(app.TotalOrdered[3].Mode, TotalOrderingModeNormal)
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
		req   = s.Require()
		nodes []types.NodeID
		conn  = s.newNetworkConnection()
	)
	prvKeys, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(pubKeys, time.Second, ConfigRoundShift)
	s.Require().NoError(err)
	dMoment := time.Now().UTC()
	// Setup core.Consensus and test.App.
	cons := map[types.NodeID]*Consensus{}
	for _, key := range prvKeys {
		_, con := s.prepareConsensus(dMoment, gov, key, conn)
		s.prepareAgreementMgrWithoutRunning(con, 4)
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
	conn := s.newNetworkConnection()
	prvKeys, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(pubKeys, time.Second, ConfigRoundShift)
	s.Require().NoError(err)
	prvKey := prvKeys[0]
	_, con := s.prepareConsensus(time.Now().UTC(), gov, prvKey, conn)
	s.prepareAgreementMgrWithoutRunning(con, 4)
	block := &types.Block{
		Position: types.Position{ChainID: 0},
	}
	s.Require().NoError(con.PrepareGenesisBlock(block, time.Now().UTC()))
	s.True(block.IsGenesis())
	s.NoError(con.preProcessBlock(block))
}

func (s *ConsensusTestSuite) TestDKGCRS() {
	n := 21
	lambda := 200 * time.Millisecond
	if testing.Short() {
		n = 7
		lambda = 100 * time.Millisecond
	}
	conn := s.newNetworkConnection()
	prvKeys, pubKeys, err := test.NewKeys(n)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(pubKeys, lambda, ConfigRoundShift)
	s.Require().NoError(err)
	gov.State().RequestChange(test.StateChangeRoundInterval, 200*lambda)
	cons := map[types.NodeID]*Consensus{}
	dMoment := time.Now().UTC()
	for _, key := range prvKeys {
		_, con := s.prepareConsensus(dMoment, gov, key, conn)
		nID := types.NewNodeID(key.PublicKey())
		cons[nID] = con
		con.cfgModule.registerDKG(uint64(0), n/3+1)
	}
	for _, con := range cons {
		con.runDKG(0, gov.Configuration(0))
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
			con.runCRS(0)
			crsFinish <- struct{}{}
		}(con)
	}
	for range cons {
		<-crsFinish
	}
	s.NotNil(gov.CRS(1))
}

func (s *ConsensusTestSuite) TestSyncBA() {
	lambdaBA := time.Second
	conn := s.newNetworkConnection()
	prvKeys, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(pubKeys, lambdaBA, ConfigRoundShift)
	s.Require().NoError(err)
	prvKey := prvKeys[0]
	_, con := s.prepareConsensus(time.Now().UTC(), gov, prvKey, conn)
	go con.Run(&types.Block{})
	hash := common.NewRandomHash()
	auths := make([]*Authenticator, 0, len(prvKeys))
	for _, prvKey := range prvKeys {
		auths = append(auths, NewAuthenticator(prvKey))
	}
	pos := types.Position{
		Round:   0,
		ChainID: 0,
		Height:  20,
	}
	baResult := &types.AgreementResult{
		BlockHash: hash,
		Position:  pos,
	}
	for _, auth := range auths {
		vote := types.NewVote(types.VoteCom, hash, 0)
		vote.Position = pos
		s.Require().NoError(auth.SignVote(vote))
		baResult.Votes = append(baResult.Votes, *vote)
	}
	// Make sure each agreement module is running. ProcessAgreementResult only
	// works properly when agreement module is running:
	//  - the bias for round begin time would be 4 * lambda.
	//  - the ticker is 1 lambdaa.
	time.Sleep(5 * lambdaBA)
	s.Require().NoError(con.ProcessAgreementResult(baResult))
	aID := con.baMgr.baModules[0].agreementID()
	s.Equal(pos, aID)

	// Test negative case.
	baResult.BlockHash = common.NewRandomHash()
	s.Equal(ErrIncorrectVoteBlockHash, con.ProcessAgreementResult(baResult))
	baResult.BlockHash = hash

	baResult.Position.Height++
	s.Equal(ErrIncorrectVotePosition, con.ProcessAgreementResult(baResult))
	baResult.Position = pos

	baResult.Votes[0].Type = types.VotePreCom
	s.Equal(ErrIncorrectVoteType, con.ProcessAgreementResult(baResult))
	baResult.Votes[0].Type = types.VoteCom

	baResult.Votes[0].ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	s.Equal(ErrIncorrectVoteProposer, con.ProcessAgreementResult(baResult))
	baResult.Votes[0].ProposerID = types.NewNodeID(pubKeys[0])

	baResult.Votes[0].Signature, err = prvKeys[0].Sign(common.NewRandomHash())
	s.Require().NoError(err)
	s.Equal(ErrIncorrectVoteSignature, con.ProcessAgreementResult(baResult))
	s.Require().NoError(auths[0].SignVote(&baResult.Votes[0]))

	for _, auth := range auths {
		vote := types.NewVote(types.VoteCom, hash, 0)
		vote.Position = pos
		s.Require().NoError(auth.SignVote(vote))
		baResult.Votes = append(baResult.Votes, *vote)
	}
	s.Equal(ErrIncorrectVoteProposer, con.ProcessAgreementResult(baResult))

	baResult.Votes = baResult.Votes[:1]
	s.Equal(ErrNotEnoughVotes, con.ProcessAgreementResult(baResult))
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
