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

// validator represents a validator in DexCon.
type validator struct {
	app *simApp
	gov *simGovernance
	db  blockdb.BlockDatabase

	config    config.Validator
	netModule *network

	ID        types.ValidatorID
	chainID   uint64
	prvKey    crypto.PrivateKey
	sigToPub  core.SigToPubFn
	consensus *core.Consensus
}

// newValidator returns a new empty validator.
func newValidator(
	prvKey crypto.PrivateKey,
	sigToPub core.SigToPubFn,
	config config.Config) *validator {

	id := types.NewValidatorID(prvKey.PublicKey())
	netModule := newNetwork(id, config.Networking)
	db, err := blockdb.NewMemBackedBlockDB(
		id.String() + ".blockdb")
	if err != nil {
		panic(err)
	}
	gov := newSimGovernance(config.Validator.Num, config.Validator.Consensus)
	return &validator{
		ID:        id,
		prvKey:    prvKey,
		sigToPub:  sigToPub,
		config:    config.Validator,
		app:       newSimApp(id, netModule),
		gov:       gov,
		db:        db,
		netModule: netModule,
	}
}

// GetID returns the ID of validator.
func (v *validator) GetID() types.ValidatorID {
	return v.ID
}

// run starts the validator.
func (v *validator) run(serverEndpoint interface{}, legacy bool) {
	// Run network.
	if err := v.netModule.setup(serverEndpoint); err != nil {
		panic(err)
	}
	msgChannel := v.netModule.receiveChanForValidator()
	peers := v.netModule.peers()
	go v.netModule.run()
	// Run consensus.
	hashes := make(common.Hashes, 0, len(peers))
	for vID := range peers {
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
	v.consensus = core.NewConsensus(
		v.app, v.gov, v.db, v.netModule, v.prvKey, v.sigToPub)
	if legacy {
		go v.consensus.RunLegacy()
	} else {
		go v.consensus.Run()
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
		default:
			panic(fmt.Errorf("unexpected message from server: %v", val))
		}
	}
	// Cleanup.
	v.consensus.Stop()
	if err := v.db.Close(); err != nil {
		fmt.Println(err)
	}
	v.netModule.report(&message{
		Type: shutdownAck,
	})
	// TODO(mission): once we have a way to know if consensus is stopped, stop
	//                the network module.
	return
}
