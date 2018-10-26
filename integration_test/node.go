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

package integration

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type consensusEventType int

const (
	evtProposeBlock consensusEventType = iota
	evtReceiveBlock
)

type consensusEventPayload struct {
	Type      consensusEventType
	PiggyBack interface{}
}

// NewProposeBlockEvent constructs an test.Event that would trigger
// block proposing.
func NewProposeBlockEvent(nID types.NodeID, when time.Time) *test.Event {
	return test.NewEvent(nID, when, &consensusEventPayload{
		Type: evtProposeBlock,
	})
}

// NewReceiveBlockEvent constructs an test.Event that would trigger
// block received.
func NewReceiveBlockEvent(
	nID types.NodeID, when time.Time, block *types.Block) *test.Event {

	return test.NewEvent(nID, when, &consensusEventPayload{
		Type:      evtReceiveBlock,
		PiggyBack: block,
	})
}

// Node is designed to work with test.Scheduler.
type Node struct {
	ID               types.NodeID
	chainNum         uint32
	chainID          uint32
	lattice          *core.Lattice
	app              *test.App
	db               blockdb.BlockDatabase
	broadcastTargets map[types.NodeID]struct{}
	networkLatency   test.LatencyModel
	proposingLatency test.LatencyModel
	prevFinalHeight  uint64
}

// NewNode constructs an instance of Node.
func NewNode(
	app *test.App,
	gov core.Governance,
	db blockdb.BlockDatabase,
	privateKey crypto.PrivateKey,
	dMoment time.Time,
	networkLatency test.LatencyModel,
	proposingLatency test.LatencyModel) *Node {

	var (
		chainID          = uint32(math.MaxUint32)
		governanceConfig = gov.Configuration(0)
		nodeSetKeys      = gov.NodeSet(0)
		nodeID           = types.NewNodeID(privateKey.PublicKey())
	)
	broadcastTargets := make(map[types.NodeID]struct{})
	for _, k := range nodeSetKeys {
		broadcastTargets[types.NewNodeID(k)] = struct{}{}
	}
	hashes := common.Hashes{}
	for nID := range broadcastTargets {
		hashes = append(hashes, nID.Hash)
	}
	sort.Sort(hashes)
	for i, h := range hashes {
		if h == nodeID.Hash {
			chainID = uint32(i)
		}
	}
	delete(broadcastTargets, nodeID)
	return &Node{
		ID:               nodeID,
		chainID:          chainID,
		chainNum:         governanceConfig.NumChains,
		broadcastTargets: broadcastTargets,
		networkLatency:   networkLatency,
		proposingLatency: proposingLatency,
		app:              app,
		db:               db,
		lattice: core.NewLattice(
			dMoment,
			governanceConfig,
			core.NewAuthenticator(privateKey),
			app,
			app,
			db,
			&common.NullLogger{}),
	}
}

// Handle implements test.EventHandler interface.
func (n *Node) Handle(e *test.Event) (events []*test.Event) {
	payload := e.Payload.(*consensusEventPayload)
	switch payload.Type {
	case evtProposeBlock:
		events, e.ExecError = n.handleProposeBlock(e.Time, payload.PiggyBack)
	case evtReceiveBlock:
		events, e.ExecError = n.handleReceiveBlock(payload.PiggyBack)
	default:
		panic(fmt.Errorf("unknown consensus event type: %v", payload.Type))
	}
	return
}

func (n *Node) handleProposeBlock(when time.Time, _ interface{}) (
	events []*test.Event, err error) {

	b, err := n.prepareBlock(when)
	if err != nil {
		return
	}
	if err = n.processBlock(b); err != nil {
		return
	}
	// Create 'block received' event for each other nodes.
	for nID := range n.broadcastTargets {
		events = append(events, NewReceiveBlockEvent(
			nID, when.Add(n.networkLatency.Delay()), b.Clone()))
	}
	// Create next 'block proposing' event for this nodes.
	events = append(events, NewProposeBlockEvent(
		n.ID, when.Add(n.proposingLatency.Delay())))
	return
}

func (n *Node) handleReceiveBlock(piggyback interface{}) (
	events []*test.Event, err error) {

	err = n.processBlock(piggyback.(*types.Block))
	if err != nil {
		panic(err)
	}
	return
}

func (n *Node) prepareBlock(when time.Time) (b *types.Block, err error) {
	b = &types.Block{
		Position: types.Position{
			ChainID: n.chainID,
		}}
	err = n.lattice.PrepareBlock(b, when)
	return
}

func (n *Node) processBlock(b *types.Block) (err error) {
	// TODO(mission): this segment of code is identical to testLatticeMgr in
	//                core/lattice_test.go, except the compaction-chain part.
	var (
		delivered []*types.Block
	)
	if err = n.lattice.SanityCheck(b); err != nil {
		if err == core.ErrRetrySanityCheckLater {
			err = nil
		} else {
			return
		}
	}
	if delivered, err = n.lattice.ProcessBlock(b); err != nil {
		return
	}
	// Deliver blocks.
	for _, b = range delivered {
		if err = n.db.Put(*b); err != nil {
			return
		}
		b.Finalization.Height = n.prevFinalHeight + 1
		n.app.BlockDelivered(b.Hash, b.Finalization)
		n.prevFinalHeight++
	}
	if err = n.lattice.PurgeBlocks(delivered); err != nil {
		return
	}
	return
}
