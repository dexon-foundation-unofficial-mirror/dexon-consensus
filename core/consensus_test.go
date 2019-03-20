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
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
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

// PullRandomness tries to pull randomness from the DEXON network.
func (n *network) PullRandomness(common.Hashes) {
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
	ch, exist := nc.cons[to]
	if !exist {
		return
	}
	msgCopy := msg
	// Clone msg if necessary.
	switch val := msg.(type) {
	case *types.Block:
		msgCopy = val.Clone()
	case *typesDKG.PrivateShare:
		// Use Marshal/Unmarshal to do deep copy.
		data, err := json.Marshal(val)
		if err != nil {
			panic(err)
		}
		valCopy := &typesDKG.PrivateShare{}
		if err := json.Unmarshal(data, valCopy); err != nil {
			panic(err)
		}
		msgCopy = valCopy
	}
	ch <- msgCopy
}

type networkConnection struct {
	s    *ConsensusTestSuite
	cons map[types.NodeID]chan interface{}
}

func (nc *networkConnection) newNetwork(nID types.NodeID) *network {
	return &network{
		nID:  nID,
		conn: nc,
	}
}

func (nc *networkConnection) setCon(nID types.NodeID, con *Consensus) {
	ch := make(chan interface{}, 1000)
	go func() {
		for {
			msg := <-ch
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
				err = con.ProcessBlockRandomnessResult(val, true)
			case *typesDKG.PrivateShare:
				err = con.cfgModule.processPrivateShare(val)
			case *typesDKG.PartialSignature:
				err = con.cfgModule.processPartialSignature(val)
			}
			if err != nil {
				panic(err)
			}
		}
	}()
	nc.cons[nID] = ch
}

type ConsensusTestSuite struct {
	suite.Suite
	conn *networkConnection
}

func (s *ConsensusTestSuite) newNetworkConnection() *networkConnection {
	return &networkConnection{
		s:    s,
		cons: make(map[types.NodeID]chan interface{}),
	}
}

func (s *ConsensusTestSuite) prepareConsensus(
	dMoment time.Time,
	gov *test.Governance,
	prvKey crypto.PrivateKey,
	conn *networkConnection) (
	*test.App, *Consensus) {

	app := test.NewApp(0, nil, nil)
	dbInst, err := db.NewMemBackedDB()
	s.Require().NoError(err)
	nID := types.NewNodeID(prvKey.PublicKey())
	network := conn.newNetwork(nID)
	con := NewConsensus(
		dMoment, app, gov, dbInst, network, prvKey, &common.NullLogger{})
	conn.setCon(nID, con)
	return app, con
}

func (s *ConsensusTestSuite) prepareConsensusWithDB(
	dMoment time.Time,
	gov *test.Governance,
	prvKey crypto.PrivateKey,
	conn *networkConnection,
	dbInst db.Database) (
	*test.App, *Consensus) {

	app := test.NewApp(0, nil, nil)
	nID := types.NewNodeID(prvKey.PublicKey())
	network := conn.newNetwork(nID)
	con := NewConsensus(
		dMoment, app, gov, dbInst, network, prvKey, &common.NullLogger{})
	conn.setCon(nID, con)
	return app, con
}

func (s *ConsensusTestSuite) TestRegisteredDKGRecover() {
	conn := s.newNetworkConnection()
	prvKeys, pubKeys, err := test.NewKeys(1)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, time.Second, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)
	gov.State().RequestChange(test.StateChangeRoundLength, uint64(200))
	dMoment := time.Now().UTC()
	dbInst, err := db.NewMemBackedDB()
	s.Require().NoError(err)
	_, con := s.prepareConsensusWithDB(dMoment, gov, prvKeys[0], conn, dbInst)

	s.Require().Nil(con.cfgModule.dkg)

	con.cfgModule.registerDKG(0, 0, 10)

	_, newCon := s.prepareConsensusWithDB(dMoment, gov, prvKeys[0], conn, dbInst)

	newCon.cfgModule.registerDKG(0, 0, 10)

	s.Require().NotNil(newCon.cfgModule.dkg)
	s.Require().True(newCon.cfgModule.dkg.prvShares.Equal(con.cfgModule.dkg.prvShares))
}

func (s *ConsensusTestSuite) TestDKGCRS() {
	n := 21
	lambda := 200 * time.Millisecond
	if testing.Short() {
		n = 7
		lambda = 100 * time.Millisecond
	}
	if isTravisCI() {
		lambda *= 5
	}
	conn := s.newNetworkConnection()
	prvKeys, pubKeys, err := test.NewKeys(n)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, lambda, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)
	gov.State().RequestChange(test.StateChangeRoundLength, uint64(200))
	cons := map[types.NodeID]*Consensus{}
	dMoment := time.Now().UTC()
	for _, key := range prvKeys {
		_, con := s.prepareConsensus(dMoment, gov, key, conn)
		nID := types.NewNodeID(key.PublicKey())
		cons[nID] = con
	}
	time.Sleep(gov.Configuration(0).MinBlockInterval * 4)
	for _, con := range cons {
		go con.runDKG(0, 0, gov.Configuration(0))
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
			con.runCRS(0, gov.CRS(0), false)
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
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, lambdaBA, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)
	prvKey := prvKeys[0]
	_, con := s.prepareConsensus(time.Now().UTC(), gov, prvKey, conn)
	go con.Run()
	defer con.Stop()
	hash := common.NewRandomHash()
	signers := make([]*utils.Signer, 0, len(prvKeys))
	for _, prvKey := range prvKeys {
		signers = append(signers, utils.NewSigner(prvKey))
	}
	pos := types.Position{
		Round:  0,
		Height: 20,
	}
	baResult := &types.AgreementResult{
		BlockHash: hash,
		Position:  pos,
	}
	for _, signer := range signers {
		vote := types.NewVote(types.VoteCom, hash, 0)
		vote.Position = pos
		s.Require().NoError(signer.SignVote(vote))
		baResult.Votes = append(baResult.Votes, *vote)
	}
	// Make sure each agreement module is running. ProcessAgreementResult only
	// works properly when agreement module is running:
	//  - the bias for round begin time would be 4 * lambda.
	//  - the ticker is 1 lambdaa.
	time.Sleep(5 * lambdaBA)
	s.Require().NoError(con.ProcessAgreementResult(baResult))
	aID := con.baMgr.baModule.agreementID()
	s.Equal(pos, aID)

	// Negative cases are moved to TestVerifyAgreementResult in utils_test.go.
}

func TestConsensus(t *testing.T) {
	suite.Run(t, new(ConsensusTestSuite))
}
