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

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// Validator represents a validator in DexCon.
type Validator struct {
	network core.Network
	app     *SimApp

	config     config.Validator
	db         *leveldb.DB
	msgChannel chan interface{}

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
	network core.Network,
	db *leveldb.DB) *Validator {

	hash := common.NewRandomHash()
	genesis := &types.Block{
		ProposerID: id,
		ParentHash: hash,
		Hash:       hash,
		Height:     0,
		Acks:       map[common.Hash]struct{}{},
	}

	app := NewSimApp(id)
	lattice := core.NewBlockLattice(blockdb.NewMemBackedBlockDB(), network, app)

	return &Validator{
		ID:      id,
		config:  config,
		network: network,
		app:     app,
		db:      db,
		lattice: lattice,
		genesis: genesis,
		current: genesis,
	}
}

// GetID returns the ID of validator.
func (v *Validator) GetID() types.ValidatorID {
	return v.ID
}

// Bootstrap bootstraps a validator.
func (v *Validator) Bootstrap(vs []*Validator) {
	for _, x := range vs {
		v.lattice.AddValidator(x.ID, x.genesis)
	}
	v.lattice.SetOwner(v.ID)
}

// Run starts the validator.
func (v *Validator) Run() {
	v.msgChannel = v.network.Join(v)

	go v.MsgServer()
	go v.BlockProposer()

	// Blocks forever.
	select {}
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

// BlockProposer propose blocks to be send to the DEXON network.
func (v *Validator) BlockProposer() {
	model := &NormalNetwork{
		Sigma: v.config.ProposeIntervalSigma,
		Mean:  v.config.ProposeIntervalMean,
	}

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
		v.lattice.ProposeBlock(block)
	}
}
