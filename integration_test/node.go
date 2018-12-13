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
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
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

// newProposeBlockEvent constructs an test.Event that would trigger
// block proposing.
func newProposeBlockEvent(nID types.NodeID,
	roundID uint64, chainID uint32, when time.Time) *test.Event {
	return test.NewEvent(nID, when, &consensusEventPayload{
		Type: evtProposeBlock,
		PiggyBack: &struct {
			round uint64
			chain uint32
		}{roundID, chainID},
	})
}

// newReceiveBlockEvent constructs an test.Event that would trigger
// block received.
func newReceiveBlockEvent(
	nID types.NodeID, when time.Time, block *types.Block) *test.Event {

	return test.NewEvent(nID, when, &consensusEventPayload{
		Type:      evtReceiveBlock,
		PiggyBack: block,
	})
}

// Node is designed to work with test.Scheduler.
type Node struct {
	ID               types.NodeID
	ownChains        []uint32
	roundEndTimes    []time.Time
	roundToNotify    uint64
	lattice          *core.Lattice
	appModule        *test.App
	stateModule      *test.State
	govModule        *test.Governance
	dbModule         db.Database
	broadcastTargets map[types.NodeID]struct{}
	networkLatency   test.LatencyModel
	proposingLatency test.LatencyModel
	prevFinalHeight  uint64
	pendings         []*types.Block
	prevHash         common.Hash
	// This variable caches the maximum NumChains seen by this node when
	// it's notified for round switching.
	latticeMaxNumChains uint32
}

// newNode constructs an instance of Node.
func newNode(
	gov *test.Governance,
	privateKey crypto.PrivateKey,
	dMoment time.Time,
	ownChains []uint32,
	networkLatency test.LatencyModel,
	proposingLatency test.LatencyModel) (*Node, error) {
	// Load all configs prepared in core.Governance into core.Lattice.
	copiedGov := gov.Clone()
	configs := loadAllConfigs(copiedGov)
	// Setup db.
	dbInst, err := db.NewMemBackedDB()
	if err != nil {
		return nil, err
	}
	// Setup test.App
	app := test.NewApp(copiedGov.State())
	// Setup lattice instance.
	lattice := core.NewLattice(
		dMoment,
		0,
		configs[0],
		core.NewAuthenticator(privateKey),
		app,
		app,
		dbInst,
		&common.NullLogger{})
	n := &Node{
		ID:                  types.NewNodeID(privateKey.PublicKey()),
		ownChains:           ownChains,
		roundEndTimes:       genRoundEndTimes(configs, dMoment),
		roundToNotify:       2,
		networkLatency:      networkLatency,
		proposingLatency:    proposingLatency,
		appModule:           app,
		stateModule:         copiedGov.State(),
		dbModule:            dbInst,
		govModule:           copiedGov,
		lattice:             lattice,
		latticeMaxNumChains: configs[0].NumChains,
	}
	for idx, config := range configs[1:] {
		if err := lattice.AppendConfig(uint64(idx+1), config); err != nil {
			return nil, err
		}
		if config.NumChains > n.latticeMaxNumChains {
			n.latticeMaxNumChains = config.NumChains
		}
	}
	return n, nil
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

func (n *Node) handleProposeBlock(when time.Time, payload interface{}) (
	events []*test.Event, err error) {
	pos := payload.(*struct {
		round uint64
		chain uint32
	})
	b, err := n.prepareBlock(pos.round, pos.chain, when)
	if err != nil {
		if err == utils.ErrInvalidChainID {
			// This chain is not included in this round, retry in next round.
			events = append(events, newProposeBlockEvent(
				n.ID, b.Position.Round+1, b.Position.ChainID,
				n.roundEndTimes[b.Position.Round]))
		}
		return
	}
	if events, err = n.processBlock(b); err != nil {
		// It's shouldn't be error when prepared.
		panic(err)
	}
	// Create 'block received' event for each other nodes.
	for nID := range n.broadcastTargets {
		events = append(events, newReceiveBlockEvent(
			nID, when.Add(n.networkLatency.Delay()), b.Clone()))
	}
	// Create next 'block proposing' event for this nodes.
	events = append(events, newProposeBlockEvent(n.ID,
		b.Position.Round,
		b.Position.ChainID,
		when.Add(n.proposingLatency.Delay())))
	return
}

func (n *Node) handleReceiveBlock(piggyback interface{}) (
	events []*test.Event, err error) {
	events, err = n.processBlock(piggyback.(*types.Block))
	if err != nil {
		panic(err)
	}
	return
}

func (n *Node) prepareBlock(
	round uint64, chainID uint32, when time.Time) (b *types.Block, err error) {
	b = &types.Block{
		Position: types.Position{
			Round:   round,
			ChainID: chainID,
		}}
	if err = n.lattice.PrepareBlock(b, when); err != nil {
		if err == core.ErrRoundNotSwitch {
			b.Position.Round++
			err = n.lattice.PrepareBlock(b, when)
		}
	}
	return
}

func (n *Node) processBlock(b *types.Block) (events []*test.Event, err error) {
	// TODO(mission): this segment of code is identical to testLatticeMgr in
	//                core/lattice_test.go, except the compaction-chain part.
	var (
		delivered []*types.Block
	)
	n.pendings = append([]*types.Block{b}, n.pendings...)
	for {
		var (
			newPendings  []*types.Block
			tmpDelivered []*types.Block
			tmpErr       error
		)
		updated := false
		for _, p := range n.pendings {
			if tmpErr = n.lattice.SanityCheck(p); tmpErr != nil {
				if tmpErr == core.ErrRetrySanityCheckLater {
					newPendings = append(newPendings, p)
				} else {
					// Those blocks are prepared by lattice module, they should
					// not be wrong.
					panic(tmpErr)
				}
				continue
			}
			if tmpDelivered, tmpErr =
				n.lattice.ProcessBlock(p); tmpErr != nil {
				// It's not allowed that sanity checked block failed to
				// be added to lattice.
				panic(tmpErr)
			}
			delivered = append(delivered, tmpDelivered...)
			updated = true
		}
		n.pendings = newPendings
		if !updated {
			break
		}
	}
	// Deliver blocks.
	for _, b = range delivered {
		if err = n.dbModule.PutBlock(*b); err != nil {
			panic(err)
		}
		b.Finalization.Height = n.prevFinalHeight + 1
		b.Finalization.ParentHash = n.prevHash
		n.appModule.BlockDelivered(b.Hash, b.Position, b.Finalization)
		n.prevFinalHeight++
		n.prevHash = b.Hash
		events = append(events, n.checkRoundSwitch(b)...)
	}
	if err = n.lattice.PurgeBlocks(delivered); err != nil {
		panic(err)
	}
	return
}

func (n *Node) checkRoundSwitch(b *types.Block) (evts []*test.Event) {
	if !b.Timestamp.After(n.roundEndTimes[b.Position.Round]) {
		return
	}
	if b.Position.Round+2 != n.roundToNotify {
		return
	}
	// Handle round switching logic.
	n.govModule.NotifyRoundHeight(n.roundToNotify, b.Finalization.Height)
	if n.roundToNotify == uint64(len(n.roundEndTimes)) {
		config := n.govModule.Configuration(n.roundToNotify)
		if config == nil {
			panic(fmt.Errorf(
				"config is not ready for round: %v", n.roundToNotify-1))
		}
		// Cache round ended time for each round.
		n.roundEndTimes = append(n.roundEndTimes,
			n.roundEndTimes[len(n.roundEndTimes)-1].Add(
				config.RoundInterval))
		// Add new config to lattice module.
		if err := n.lattice.AppendConfig(n.roundToNotify, config); err != nil {
			panic(err)
		}
		if config.NumChains > n.latticeMaxNumChains {
			// We can be sure that lattice module can support this number of
			// chains.
			for _, chainID := range n.ownChains {
				if chainID < n.latticeMaxNumChains {
					continue
				}
				if chainID >= config.NumChains {
					continue
				}
				// For newly added chains, add block proposing seed event.
				evts = append(evts, newProposeBlockEvent(n.ID, n.roundToNotify,
					chainID, n.roundEndTimes[n.roundToNotify-1]))
			}
			n.latticeMaxNumChains = config.NumChains
		}
	} else if n.roundToNotify > uint64(len(n.roundEndTimes)) {
		panic(fmt.Errorf(
			"config notification not incremental: %v, cached configs: %v",
			n.roundToNotify, len(n.roundEndTimes)))
	}
	n.roundToNotify++
	return
}

// Bootstrap this node with block proposing event.
func (n *Node) Bootstrap(sch *test.Scheduler, now time.Time) (err error) {
	sch.RegisterEventHandler(n.ID, n)
	for _, chainID := range n.ownChains {
		if chainID >= n.latticeMaxNumChains {
			continue
		}
		err = sch.Seed(newProposeBlockEvent(n.ID, 0, chainID, now))
		if err != nil {
			return
		}
	}
	return
}

func (n *Node) setBroadcastTargets(targets map[types.NodeID]struct{}) {
	// Clone targets, except self.
	targetsCopy := make(map[types.NodeID]struct{})
	for nID := range targets {
		if nID == n.ID {
			continue
		}
		targetsCopy[nID] = struct{}{}
	}
	n.broadcastTargets = targetsCopy
}

func (n *Node) app() *test.App {
	return n.appModule
}

func (n *Node) db() db.Database {
	return n.dbModule
}

func (n *Node) gov() *test.Governance {
	return n.govModule
}
