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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// Validator represents a validator in DexCon.
type Validator struct {
	network Network
	app     *SimApp

	config     config.Validator
	msgChannel chan interface{}
	isFinished chan struct{}

	ID              types.ValidatorID
	lattice         *core.BlockLattice
	compactionChain *core.BlockChain

	genesis *types.Block
	current *types.Block
}

// NewValidator returns a new empty validator.
func NewValidator(
	id types.ValidatorID,
	config config.Validator,
	network Network) *Validator {
	app := NewSimApp(id, network)
	db, err := blockdb.NewMemBackedBlockDB()
	if err != nil {
		panic(err)
	}
	lattice := core.NewBlockLattice(db, app)
	return &Validator{
		ID:         id,
		config:     config,
		network:    network,
		app:        app,
		lattice:    lattice,
		isFinished: make(chan struct{}),
	}
}

// GetID returns the ID of validator.
func (v *Validator) GetID() types.ValidatorID {
	return v.ID
}

// Run starts the validator.
func (v *Validator) Run() {
	v.msgChannel = v.network.Join(v)

	isStopped := make(chan struct{})
	isShutdown := make(chan struct{})

	v.BroadcastGenesisBlock()
	go v.MsgServer()
	go v.CheckServerInfo(isShutdown)
	go v.BlockProposer(isStopped, isShutdown)

	// Blocks forever.
	<-isStopped
	v.network.NotifyServer(Message{
		Type: shutdownAck,
	})
	v.isFinished <- struct{}{}
}

// Wait for the validator to stop (if peerServer told it to).
func (v *Validator) Wait() {
	<-v.isFinished
}

// CheckServerInfo will check the info from the peerServer and update
// validator's status if needed.
func (v *Validator) CheckServerInfo(isShutdown chan struct{}) {
	for {
		infoMsg := v.network.GetServerInfo()
		if infoMsg.Status == shutdown {
			isShutdown <- struct{}{}
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// MsgServer listen to the network channel for message and handle it.
func (v *Validator) MsgServer() {
	for {
		msg := <-v.msgChannel

		switch val := msg.(type) {
		case *types.Block:
			//if val.ProposerID.Equal(v.ID) {
			//	continue
			//}
			v.lattice.ProcessBlock(val, true)
		}
	}
}

// BroadcastGenesisBlock broadcasts genesis block to all peers.
func (v *Validator) BroadcastGenesisBlock() {
	// Wait until all peer joined the network.
	for v.network.NumPeers() != v.config.Num {
		time.Sleep(time.Second)
	}

	if v.genesis == nil {
		hash := common.NewRandomHash()
		b := &types.Block{
			ProposerID: v.ID,
			ParentHash: hash,
			Hash:       hash,
			Height:     0,
			Acks:       map[common.Hash]struct{}{},
		}
		v.genesis = b
		v.current = b

		v.lattice.AddValidator(v.ID, b)
		v.lattice.SetOwner(v.ID)

		v.lattice.PrepareBlock(b)
		v.network.BroadcastBlock(b)
	}
}

// BlockProposer propose blocks to be send to the DEXON network.
func (v *Validator) BlockProposer(isStopped, isShutdown chan struct{}) {
	// Wait until all peer knows each other.
	for len(v.lattice.ValidatorSet) != v.config.Num {
		time.Sleep(time.Second)
	}

	model := &NormalNetwork{
		Sigma: v.config.ProposeIntervalSigma,
		Mean:  v.config.ProposeIntervalMean,
	}
ProposingBlockLoop:
	for {
		time.Sleep(model.Delay())

		block := &types.Block{
			ProposerID: v.ID,
			ParentHash: v.current.Hash,
			Hash:       common.NewRandomHash(),
			Height:     v.current.Height + 1.,
			Acks:       map[common.Hash]struct{}{},
		}
		v.current = block
		v.lattice.PrepareBlock(block)
		v.network.BroadcastBlock(block)
		select {
		case <-isShutdown:
			isStopped <- struct{}{}
			break ProposingBlockLoop
		default:
			break
		}
	}
}
