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
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/simulation/config"
)

type infoStatus string

const (
	statusInit     infoStatus = "init"
	statusNormal   infoStatus = "normal"
	statusShutdown infoStatus = "shutdown"
)

type messageType string

const (
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
	db        blockdb.BlockDatabase
	gov       *test.Governance
	netModule *test.Network
	ID        types.NodeID
	prvKey    crypto.PrivateKey
	consensus *core.Consensus
}

// newNode returns a new empty node.
func newNode(
	prvKey crypto.PrivateKey,
	config config.Config) *node {
	pubKey := prvKey.PublicKey()
	netModule := test.NewNetwork(
		pubKey,
		&test.NormalLatencyModel{
			Mean:  config.Networking.Mean,
			Sigma: config.Networking.Sigma,
		},
		test.NewDefaultMarshaller(&jsonMarshaller{}),
		test.NetworkConfig{
			Type:       config.Networking.Type,
			PeerServer: config.Networking.PeerServer,
			PeerPort:   peerPort,
		})
	id := types.NewNodeID(pubKey)
	db, err := blockdb.NewMemBackedBlockDB(id.String() + ".blockdb")
	if err != nil {
		panic(err)
	}
	// Sync config to state in governance.
	cConfig := config.Node.Consensus
	gov, err := test.NewGovernance([]crypto.PublicKey{pubKey}, time.Millisecond)
	if err != nil {
		panic(err)
	}
	gov.State().RequestChange(test.StateChangeK, cConfig.K)
	gov.State().RequestChange(test.StateChangePhiRatio, cConfig.PhiRatio)
	gov.State().RequestChange(test.StateChangeNumChains, cConfig.ChainNum)
	gov.State().RequestChange(
		test.StateChangeNotarySetSize, cConfig.NotarySetSize)
	gov.State().RequestChange(test.StateChangeDKGSetSize, cConfig.DKGSetSize)
	gov.State().RequestChange(test.StateChangeLambdaBA, time.Duration(
		cConfig.LambdaBA)*time.Millisecond)
	gov.State().RequestChange(test.StateChangeLambdaDKG, time.Duration(
		cConfig.LambdaDKG)*time.Millisecond)
	gov.State().RequestChange(test.StateChangeRoundInterval, time.Duration(
		cConfig.RoundInterval)*time.Millisecond)
	gov.State().RequestChange(
		test.StateChangeMinBlockInterval,
		3*time.Duration(cConfig.LambdaBA)*time.Millisecond)
	gov.State().ProposeCRS(0, crypto.Keccak256Hash([]byte(cConfig.GenesisCRS)))
	return &node{
		ID:        id,
		prvKey:    prvKey,
		app:       newSimApp(id, netModule, gov.State()),
		gov:       gov,
		db:        db,
		netModule: netModule,
	}
}

// GetID returns the ID of node.
func (n *node) GetID() types.NodeID {
	return n.ID
}

// run starts the node.
func (n *node) run(serverEndpoint interface{}, dMoment time.Time) {
	// Run network.
	if err := n.netModule.Setup(serverEndpoint); err != nil {
		panic(err)
	}
	msgChannel := n.netModule.ReceiveChanForNode()
	peers := n.netModule.Peers()
	go n.netModule.Run()
	// Run consensus.
	hashes := make(common.Hashes, 0, len(peers))
	for _, pubKey := range peers {
		nID := types.NewNodeID(pubKey)
		n.gov.State().RequestChange(test.StateAddNode, pubKey)
		hashes = append(hashes, nID.Hash)
	}
	// Setup of governance is ready, can be switched to remote mode.
	n.gov.SwitchToRemoteMode(n.netModule)
	// Setup Consensus.
	n.consensus = core.NewConsensus(
		dMoment,
		n.app,
		n.gov,
		n.db,
		n.netModule,
		n.prvKey,
		&common.SimpleLogger{})
	go n.consensus.Run(&types.Block{})

	// Blocks forever.
MainLoop:
	for {
		msg := <-msgChannel
		switch val := msg.(type) {
		case infoStatus:
			if val == statusShutdown {
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
	n.netModule.Report(&message{
		Type: shutdownAck,
	})
	// TODO(mission): once we have a way to know if consensus is stopped, stop
	//                the network module.
	return
}
