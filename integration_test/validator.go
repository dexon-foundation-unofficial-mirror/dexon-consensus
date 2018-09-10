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

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
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
func NewProposeBlockEvent(vID types.ValidatorID, when time.Time) *test.Event {
	return test.NewEvent(vID, when, &consensusEventPayload{
		Type: evtProposeBlock,
	})
}

// NewReceiveBlockEvent constructs an test.Event that would trigger
// block received.
func NewReceiveBlockEvent(
	vID types.ValidatorID, when time.Time, block *types.Block) *test.Event {

	return test.NewEvent(vID, when, &consensusEventPayload{
		Type:      evtReceiveBlock,
		PiggyBack: block,
	})
}

// Validator is designed to work with test.Scheduler.
type Validator struct {
	ID               types.ValidatorID
	chainID          uint32
	cons             *core.Consensus
	gov              core.Governance
	networkLatency   test.LatencyModel
	proposingLatency test.LatencyModel
}

// NewValidator constructs an instance of Validator.
func NewValidator(
	app core.Application,
	gov core.Governance,
	db blockdb.BlockDatabase,
	privateKey crypto.PrivateKey,
	vID types.ValidatorID,
	networkLatency test.LatencyModel,
	proposingLatency test.LatencyModel) *Validator {

	hashes := make(common.Hashes, 0)
	for vID := range gov.GetValidatorSet() {
		hashes = append(hashes, vID.Hash)
	}
	sort.Sort(hashes)
	chainID := uint32(0)
	for i, hash := range hashes {
		if hash == vID.Hash {
			chainID = uint32(i)
			break
		}
	}

	return &Validator{
		ID:               vID,
		chainID:          chainID,
		gov:              gov,
		networkLatency:   networkLatency,
		proposingLatency: proposingLatency,
		cons: core.NewConsensus(
			app, gov,
			db, &Network{}, time.NewTicker(1), privateKey, eth.SigToPub),
	}
}

// Handle implements test.EventHandler interface.
func (v *Validator) Handle(e *test.Event) (events []*test.Event) {
	payload := e.Payload.(*consensusEventPayload)
	switch payload.Type {
	case evtProposeBlock:
		events, e.ExecError = v.handleProposeBlock(e.Time, payload.PiggyBack)
	case evtReceiveBlock:
		events, e.ExecError = v.handleReceiveBlock(payload.PiggyBack)
	default:
		panic(fmt.Errorf("unknown consensus event type: %v", payload.Type))
	}
	return
}

func (v *Validator) handleProposeBlock(when time.Time, piggyback interface{}) (
	events []*test.Event, err error) {

	b := &types.Block{
		ProposerID: v.ID,
		Position: types.Position{
			ChainID: v.chainID,
		},
	}
	defer types.RecycleBlock(b)
	if err = v.cons.PrepareBlock(b, when); err != nil {
		return
	}
	if err = v.cons.ProcessBlock(b); err != nil {
		return
	}
	// Create 'block received' event for each other validators.
	for vID := range v.gov.GetValidatorSet() {
		if vID == v.ID {
			continue
		}
		events = append(events, NewReceiveBlockEvent(
			vID, when.Add(v.networkLatency.Delay()), b.Clone()))
	}
	// Create next 'block proposing' event for this validators.
	events = append(events, NewProposeBlockEvent(
		v.ID, when.Add(v.proposingLatency.Delay())))
	return
}

func (v *Validator) handleReceiveBlock(piggyback interface{}) (
	events []*test.Event, err error) {

	err = v.cons.ProcessBlock(piggyback.(*types.Block))
	if err != nil {
		panic(err)
	}
	return
}
