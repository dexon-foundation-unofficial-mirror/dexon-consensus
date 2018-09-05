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
	msgChannel <-chan interface{}
	isFinished chan struct{}

	ID              types.ValidatorID
	chainID         uint64
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
func (v *Validator) Run(legacy bool) {
	v.network.Join(v)
	v.msgChannel = v.network.ReceiveChan()

	hashes := make(common.Hashes, 0, v.network.NumPeers())
	for _, vID := range v.network.Endpoints() {
		v.gov.addValidator(vID)
		hashes = append(hashes, vID.Hash)
	}
	sort.Sort(hashes)
	for i, hash := range hashes {
		if hash == v.ID.Hash {
			v.chainID = uint64(i)
			break
		}
	}
	if legacy {
		v.consensus = core.NewConsensus(
			v.app, v.gov, v.db, v.network,
			time.NewTicker(
				time.Duration(v.config.Legacy.ProposeIntervalMean)*time.Millisecond),
			v.prvKey, v.sigToPub)

		go v.consensus.RunLegacy()
	} else {
		v.consensus = core.NewConsensus(
			v.app, v.gov, v.db, v.network,
			time.NewTicker(
				time.Duration(v.config.Lambda)*time.Millisecond),
			v.prvKey, v.sigToPub)

		go v.consensus.Run()
	}

	isShutdown := make(chan struct{})

	go v.CheckServerInfo(isShutdown)

	// Blocks forever.
	<-isShutdown
	v.consensus.Stop()
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
