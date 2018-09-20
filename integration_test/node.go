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
	"sort"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
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
	chainID          uint32
	cons             *core.Consensus
	gov              core.Governance
	networkLatency   test.LatencyModel
	proposingLatency test.LatencyModel
}

// NewNode constructs an instance of Node.
func NewNode(
	app core.Application,
	gov core.Governance,
	db blockdb.BlockDatabase,
	privateKey crypto.PrivateKey,
	nID types.NodeID,
	networkLatency test.LatencyModel,
	proposingLatency test.LatencyModel) *Node {

	hashes := make(common.Hashes, 0)
	for nID := range gov.GetNotarySet() {
		hashes = append(hashes, nID.Hash)
	}
	sort.Sort(hashes)
	chainID := uint32(0)
	for i, hash := range hashes {
		if hash == nID.Hash {
			chainID = uint32(i)
			break
		}
	}

	return &Node{
		ID:               nID,
		chainID:          chainID,
		gov:              gov,
		networkLatency:   networkLatency,
		proposingLatency: proposingLatency,
		cons: core.NewConsensus(
			app, gov, db, &Network{}, privateKey, eth.SigToPub),
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

func (n *Node) handleProposeBlock(when time.Time, piggyback interface{}) (
	events []*test.Event, err error) {

	b := &types.Block{
		ProposerID: n.ID,
		Position: types.Position{
			ChainID: n.chainID,
		},
	}
	defer types.RecycleBlock(b)
	if err = n.cons.PrepareBlock(b, when); err != nil {
		return
	}
	if err = n.cons.ProcessBlock(b); err != nil {
		return
	}
	// Create 'block received' event for each other nodes.
	for nID := range n.gov.GetNotarySet() {
		if nID == n.ID {
			continue
		}
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

	err = n.cons.ProcessBlock(piggyback.(*types.Block))
	if err != nil {
		panic(err)
	}
	return
}
