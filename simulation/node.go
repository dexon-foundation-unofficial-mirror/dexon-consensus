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

package simulation

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/simulation/config"
)

type serverNotification string

const (
	ntfShutdown         serverNotification = "shutdown"
	ntfSelectedAsMaster serverNotification = "as_master"
	ntfReady            serverNotification = "ready"
)

type messageType string

const (
	setupOK        messageType = "setupOK"
	shutdownAck    messageType = "shutdownAck"
	blockTimestamp messageType = "blockTimestamps"
)

// message is a struct for peer sending message to server.
type message struct {
	Type    messageType     `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// node represents a node in DexCon.
type node struct {
	app       core.Application
	db        db.Database
	gov       *test.Governance
	netModule *test.Network
	ID        types.NodeID
	prvKey    crypto.PrivateKey
	logger    common.Logger
	consensus *core.Consensus
	cfg       *config.Config
}

// newNode returns a new empty node.
func newNode(prvKey crypto.PrivateKey, logger common.Logger,
	cfg config.Config) *node {
	pubKey := prvKey.PublicKey()
	netModule := test.NewNetwork(pubKey, test.NetworkConfig{
		Type:       cfg.Networking.Type,
		PeerServer: cfg.Networking.PeerServer,
		PeerPort:   peerPort,
		DirectLatency: &test.NormalLatencyModel{
			Mean:  cfg.Networking.Direct.Mean,
			Sigma: cfg.Networking.Direct.Sigma,
		},
		GossipLatency: &test.NormalLatencyModel{
			Mean:  cfg.Networking.Gossip.Mean,
			Sigma: cfg.Networking.Gossip.Sigma,
		},
		Marshaller: test.NewDefaultMarshaller(&jsonMarshaller{})})
	id := types.NewNodeID(pubKey)
	dbInst, err := db.NewMemBackedDB(id.String() + ".db")
	if err != nil {
		panic(err)
	}
	// Sync config to state in governance.
	gov, err := test.NewGovernance(
		test.NewState(core.DKGDelayRound,
			[]crypto.PublicKey{pubKey}, time.Millisecond, logger, true),
		core.ConfigRoundShift)
	if err != nil {
		panic(err)
	}
	return &node{
		ID:        id,
		prvKey:    prvKey,
		logger:    logger,
		app:       newSimApp(id, netModule, gov.State()),
		gov:       gov,
		db:        dbInst,
		netModule: netModule,
		cfg:       &cfg,
	}
}

// GetID returns the ID of node.
func (n *node) GetID() types.NodeID {
	return n.ID
}

// run starts the node.
func (n *node) run(
	serverEndpoint interface{}) {
	// Run network.
	if err := n.netModule.Setup(serverEndpoint); err != nil {
		panic(err)
	}
	msgChannel := n.netModule.ReceiveChanForNode()
	peers := n.netModule.Peers()
	dMoment := n.netModule.DMoment()
	n.logger.Info("Simulation DMoment", "dMoment", dMoment)
	go n.netModule.Run()
	// Run consensus.
	hashes := make(common.Hashes, 0, len(peers))
	for _, pubKey := range peers {
		nID := types.NewNodeID(pubKey)
		if err :=
			n.gov.State().RequestChange(test.StateAddNode, pubKey); err != nil {
			panic(err)
		}
		hashes = append(hashes, nID.Hash)
	}
	n.prepareConfigs()
	if err := n.netModule.Report(&message{Type: setupOK}); err != nil {
		panic(err)
	}
	// Wait for a "ready" server notification.
readyLoop:
	for {
		msg := <-msgChannel
		ntf := msg.(serverNotification)
		switch ntf {
		case ntfReady:
			break readyLoop
		case ntfSelectedAsMaster:
			n.logger.Info(
				"receive 'selected-as-master' notification from server")
			for _, c := range n.cfg.Node.Changes {
				if c.Round <= core.ConfigRoundShift+1 {
					continue
				}
				n.logger.Info("register config change", "change", c)
				if err := c.RegisterChange(n.gov); err != nil {
					panic(err)
				}
			}
		default:
			panic(fmt.Errorf("receive unexpected server notification: %v", ntf))
		}
	}
	// Setup Consensus.
	n.consensus = core.NewConsensusForSimulation(
		dMoment,
		n.app,
		n.gov,
		n.db,
		n.netModule,
		n.prvKey,
		n.logger)
	go n.consensus.Run()

	// Blocks forever.
MainLoop:
	for {
		msg := <-msgChannel
		switch val := msg.(type) {
		case serverNotification:
			if val == ntfShutdown {
				n.logger.Info("receive shutdown notification from server")
				break MainLoop
			}
		default:
			panic(fmt.Errorf("unexpected message from server: %v", val))
		}
	}
	// Cleanup.
	n.consensus.Stop()
	if err := n.db.Close(); err != nil {
		fmt.Println(err)
	}
	if err := n.netModule.Report(&message{Type: shutdownAck}); err != nil {
		panic(err)
	}
	// TODO(mission): once we have a way to know if consensus is stopped, stop
	//                the network module.
	return
}

func (n *node) prepareConfigs() {
	// Prepare configurations.
	cConfig := n.cfg.Node.Consensus
	n.gov.State().RequestChange(
		test.StateChangeNotarySetSize, cConfig.NotarySetSize) // #nosec G104
	n.gov.State().RequestChange(test.StateChangeDKGSetSize, cConfig.DKGSetSize) // #nosec G104
	n.gov.State().RequestChange(test.StateChangeLambdaBA, time.Duration(
		cConfig.LambdaBA)*time.Millisecond) // #nosec G104
	n.gov.State().RequestChange(test.StateChangeLambdaDKG, time.Duration(
		cConfig.LambdaDKG)*time.Millisecond) // #nosec G104
	n.gov.State().RequestChange(test.StateChangeRoundLength, cConfig.RoundLength) // #nosec G104
	n.gov.State().RequestChange(test.StateChangeMinBlockInterval, time.Duration(
		cConfig.MinBlockInterval)*time.Millisecond) // #nosec G104
	n.gov.State().ProposeCRS(0, crypto.Keccak256Hash([]byte(cConfig.GenesisCRS))) // #nosec G104
	// These rounds are not safe to be registered as pending state change
	// requests.
	for i := uint64(0); i <= core.ConfigRoundShift+1; i++ {
		n.logger.Info("prepare config", "round", i)
		prepareConfigs(i, n.cfg.Node.Changes, n.gov)
	}
	// This notification is implictly called in full node.
	n.gov.NotifyRound(0, 0)
	// Setup of configuration is ready, can be switched to remote mode.
	n.gov.SwitchToRemoteMode(n.netModule)
}
