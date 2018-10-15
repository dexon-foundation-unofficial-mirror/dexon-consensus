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
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// Run starts the simulation.
func Run(cfg *config.Config) {
	var (
		networkType = cfg.Networking.Type
		server      *PeerServer
		wg          sync.WaitGroup
		err         error
	)

	dMoment := time.Now().UTC().Add(1 * time.Second)

	// init is a function to init a node.
	init := func(serverEndpoint interface{}) {
		prv, err := ecdsa.NewPrivateKey()
		if err != nil {
			panic(err)
		}
		v := newNode(prv, *cfg)
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.run(serverEndpoint, dMoment)
		}()
	}

	switch networkType {
	case config.NetworkTypeTCP:
		// Intialized a simulation on multiple remotely peers.
		// The peer-server would be initialized with another command.
		init(nil)
	case config.NetworkTypeTCPLocal, config.NetworkTypeFake:
		// Initialize a local simulation with a peer server.
		var serverEndpoint interface{}
		server = NewPeerServer()
		if serverEndpoint, err = server.Setup(cfg); err != nil {
			panic(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			server.Run()
		}()
		// Initialize all nodes.
		for i := 0; i < cfg.Node.Num; i++ {
			init(serverEndpoint)
		}
	}
	wg.Wait()

	// Do not exit when we are in TCP node, since k8s will restart the pod and
	// cause confusions.
	if networkType == config.NetworkTypeTCP {
		select {}
	}
}
