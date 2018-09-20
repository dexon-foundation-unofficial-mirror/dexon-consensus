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

package simulation

import (
	"fmt"
	"sort"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// node represents a node in DexCon.
type node struct {
	app *simApp
	gov *simGovernance
	db  blockdb.BlockDatabase

	config    config.Node
	netModule *network

	ID        types.NodeID
	chainID   uint64
	prvKey    crypto.PrivateKey
	sigToPub  core.SigToPubFn
	consensus *core.Consensus
}

// newNode returns a new empty node.
func newNode(
	prvKey crypto.PrivateKey,
	sigToPub core.SigToPubFn,
	config config.Config) *node {

	id := types.NewNodeID(prvKey.PublicKey())
	netModule := newNetwork(id, config.Networking)
	db, err := blockdb.NewMemBackedBlockDB(
		id.String() + ".blockdb")
	if err != nil {
		panic(err)
	}
	gov := newSimGovernance(id, config.Node.Num, config.Node.Consensus)
	return &node{
		ID:        id,
		prvKey:    prvKey,
		sigToPub:  sigToPub,
		config:    config.Node,
		app:       newSimApp(id, netModule),
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
func (n *node) run(serverEndpoint interface{}, legacy bool) {
	// Run network.
	if err := n.netModule.setup(serverEndpoint); err != nil {
		panic(err)
	}
	msgChannel := n.netModule.receiveChanForNode()
	peers := n.netModule.peers()
	go n.netModule.run()
	n.gov.setNetwork(n.netModule)
	// Run consensus.
	hashes := make(common.Hashes, 0, len(peers))
	for nID := range peers {
		n.gov.addNode(nID)
		hashes = append(hashes, nID.Hash)
	}
	sort.Sort(hashes)
	for i, hash := range hashes {
		if hash == n.ID.Hash {
			n.chainID = uint64(i)
			break
		}
	}
	n.consensus = core.NewConsensus(
		n.app, n.gov, n.db, n.netModule, n.prvKey, n.sigToPub)
	if legacy {
		go n.consensus.RunLegacy()
	} else {
		go n.consensus.Run()
	}

	// Blocks forever.
MainLoop:
	for {
		msg := <-msgChannel
		switch val := msg.(type) {
		case infoStatus:
			if val == statusShutdown {
				break MainLoop
			}
		case *types.DKGComplaint:
			n.gov.AddDKGComplaint(val)
		case *types.DKGMasterPublicKey:
			n.gov.AddDKGMasterPublicKey(val)
		default:
			panic(fmt.Errorf("unexpected message from server: %v", val))
		}
	}
	// Cleanup.
	n.consensus.Stop()
	if err := n.db.Close(); err != nil {
		fmt.Println(err)
	}
	n.netModule.report(&message{
		Type: shutdownAck,
	})
	// TODO(mission): once we have a way to know if consensus is stopped, stop
	//                the network module.
	return
}
