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

package simulation

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/dexon-foundation/dexon/log"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/simulation/config"
)

// Run starts the simulation.
func Run(cfg *config.Config, logPrefix string) {
	var (
		networkType = cfg.Networking.Type
		server      *PeerServer
		wg          sync.WaitGroup
		err         error
	)

	if cfg.Node.Consensus.NotarySetSize > cfg.Node.Num {
		panic(fmt.Errorf("NotarySetSize should not be larger the node num"))
	}

	if cfg.Node.Consensus.DKGSetSize > cfg.Node.Num {
		panic(fmt.Errorf("DKGSetSze should not be larger the node num"))
	}

	newLogger := func(logPrefix string) common.Logger {
		mw := io.Writer(os.Stderr)
		if logPrefix != "" {
			f, err := os.Create(logPrefix + ".log")
			if err != nil {
				panic(err)
			}
			mw = io.MultiWriter(os.Stderr, f)
		}
		logger := log.New()
		logger.SetHandler(log.StreamHandler(mw, log.TerminalFormat(false)))
		return logger
	}

	// init is a function to init a node.
	init := func(serverEndpoint interface{}, logger common.Logger) {
		prv, err := ecdsa.NewPrivateKey()
		if err != nil {
			panic(err)
		}
		v := newNode(prv, logger, *cfg)
		wg.Add(1)
		go func() {
			defer wg.Done()
			v.run(serverEndpoint)
		}()
	}

	switch networkType {
	case test.NetworkTypeTCP:
		// Intialized a simulation on multiple remotely peers.
		// The peer-server would be initialized with another command.
		init(nil, newLogger(logPrefix))
	case test.NetworkTypeTCPLocal, test.NetworkTypeFake:
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
		for i := uint32(0); i < cfg.Node.Num; i++ {
			prefix := fmt.Sprintf("%s.%d", logPrefix, i)
			if logPrefix == "" {
				prefix = ""
			}
			init(serverEndpoint, newLogger(prefix))
		}
	}
	wg.Wait()

	// Do not exit when we are in TCP node, since k8s will restart the pod and
	// cause confusions.
	if networkType == test.NetworkTypeTCP {
		select {}
	}
}
