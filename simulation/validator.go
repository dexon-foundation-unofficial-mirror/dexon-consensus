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
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// Validator represents a validator in DexCon.
type Validator struct {
	network Network
	app     *simApp
	gov     *simGovernance
	db      blockdb.BlockDatabase

	config     config.Validator
	msgChannel chan interface{}
	isFinished chan struct{}

	ID              types.ValidatorID
	prvKey          crypto.PrivateKey
	sigToPub        core.SigToPubFn
	consensus       *core.Consensus
	compactionChain *core.BlockChain
}

// NewValidator returns a new empty validator.
func NewValidator(
	prvKey crypto.PrivateKey,
	sigToPub core.SigToPubFn,
	config config.Validator,
	network Network) *Validator {

	id := types.NewValidatorID(prvKey.PublicKey())

	db, err := blockdb.NewMemBackedBlockDB(
		id.String() + ".blockdb")
	if err != nil {
		panic(err)
	}
	gov := newSimGovernance(config.Num, config.Consensus)
	return &Validator{
		ID:         id,
		prvKey:     prvKey,
		sigToPub:   sigToPub,
		config:     config,
		network:    network,
		app:        newSimApp(id, network),
		gov:        gov,
		db:         db,
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

	for _, vID := range v.network.Endpoints() {
		v.gov.addValidator(vID)
	}
	v.consensus = core.NewConsensus(
		v.app, v.gov, v.db, v.prvKey, v.sigToPub)

	genesisBlock := &types.Block{
		ProposerID: v.ID,
		ParentHash: common.Hash{},
		Hash:       common.NewRandomHash(),
		Height:     0,
		Acks:       map[common.Hash]struct{}{},
		Timestamps: map[types.ValidatorID]time.Time{
			v.ID: time.Now().UTC(),
		},
	}
	isStopped := make(chan struct{}, 2)
	isShutdown := make(chan struct{})

	v.app.addBlock(genesisBlock)
	v.consensus.ProcessBlock(genesisBlock)
	v.BroadcastGenesisBlock(genesisBlock)
	go v.MsgServer(isStopped)
	go v.CheckServerInfo(isShutdown)
	go v.BlockProposer(isStopped, isShutdown)

	// Blocks forever.
	<-isStopped
	if err := v.db.Close(); err != nil {
		fmt.Println(err)
	}
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
		if infoMsg.Status == statusShutdown {
			isShutdown <- struct{}{}
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// MsgServer listen to the network channel for message and handle it.
func (v *Validator) MsgServer(
	isStopped chan struct{}) {

	for {
		var msg interface{}
		select {
		case msg = <-v.msgChannel:
		case <-isStopped:
			return
		}

		switch val := msg.(type) {
		case *types.Block:
			v.app.addBlock(val)
			if err := v.consensus.ProcessBlock(val); err != nil {
				fmt.Println(err)
				//panic(err)
			}
		}
	}
}

// BroadcastGenesisBlock broadcasts genesis block to all peers.
func (v *Validator) BroadcastGenesisBlock(genesisBlock *types.Block) {
	// Wait until all peer joined the network.
	for v.network.NumPeers() != v.config.Num {
		time.Sleep(time.Second)
	}
	v.network.BroadcastBlock(genesisBlock)
}

// BlockProposer propose blocks to be send to the DEXON network.
func (v *Validator) BlockProposer(isStopped, isShutdown chan struct{}) {
	model := &NormalNetwork{
		Sigma: v.config.ProposeIntervalSigma,
		Mean:  v.config.ProposeIntervalMean,
	}
ProposingBlockLoop:
	for {
		time.Sleep(model.Delay())

		block := &types.Block{
			ProposerID: v.ID,
			Hash:       common.NewRandomHash(),
		}
		if err := v.consensus.PrepareBlock(block, time.Now().UTC()); err != nil {
			panic(err)
		}
		v.app.addBlock(block)
		if err := v.consensus.ProcessBlock(block); err != nil {
			fmt.Println(err)
			//panic(err)
		}
		v.network.BroadcastBlock(block)
		select {
		case <-isShutdown:
			isStopped <- struct{}{}
			isStopped <- struct{}{}
			break ProposingBlockLoop
		default:
			break
		}
	}
}
