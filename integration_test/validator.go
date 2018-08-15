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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
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

func newProposeBlockEvent(vID types.ValidatorID, when time.Time) *test.Event {
	return test.NewEvent(vID, when, &consensusEventPayload{
		Type: evtProposeBlock,
	})
}

func newReceiveBlockEvent(
	vID types.ValidatorID, when time.Time, block *types.Block) *test.Event {

	return test.NewEvent(vID, when, &consensusEventPayload{
		Type:      evtReceiveBlock,
		PiggyBack: block,
	})
}

type validator struct {
	ID               types.ValidatorID
	cons             *core.Consensus
	gov              core.Governance
	networkLatency   LatencyModel
	proposingLatency LatencyModel
}

func newValidator(
	app core.Application,
	gov core.Governance,
	db blockdb.BlockDatabase,
	privateKey crypto.PrivateKey,
	vID types.ValidatorID,
	networkLatency LatencyModel,
	proposingLatency LatencyModel) *validator {

	return &validator{
		ID:               vID,
		gov:              gov,
		networkLatency:   networkLatency,
		proposingLatency: proposingLatency,
		cons: core.NewConsensus(
			app, gov, db, privateKey, eth.SigToPub),
	}
}

func (v *validator) Handle(e *test.Event) (events []*test.Event) {
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

func (v *validator) handleProposeBlock(when time.Time, piggyback interface{}) (
	events []*test.Event, err error) {

	b := &types.Block{ProposerID: v.ID}
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
		events = append(events, newReceiveBlockEvent(
			vID, when.Add(v.networkLatency.Delay()), b.Clone()))
	}
	// Create next 'block proposing' event for this validators.
	events = append(events, newProposeBlockEvent(
		v.ID, when.Add(v.proposingLatency.Delay())))
	return
}

func (v *validator) handleReceiveBlock(piggyback interface{}) (
	events []*test.Event, err error) {

	err = v.cons.ProcessBlock(piggyback.(*types.Block))
	if err != nil {
		panic(err)
	}
	return
}
