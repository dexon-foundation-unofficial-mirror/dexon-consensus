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
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// PeerServer is the main object for maintaining peer list.
type PeerServer struct {
	peers            map[types.ValidatorID]string
	peersMu          sync.Mutex
	peerTotalOrder   PeerTotalOrder
	peerTotalOrderMu sync.Mutex
}

// NewPeerServer returns a new peer server.
func NewPeerServer() *PeerServer {
	return &PeerServer{
		peers:          make(map[types.ValidatorID]string),
		peerTotalOrder: make(PeerTotalOrder),
	}
}

// Run starts the peer server.
func (p *PeerServer) Run(configPath string) {
	cfg, err := config.Read(configPath)
	if err != nil {
		panic(err)
	}

	resetHandler := func(w http.ResponseWriter, r *http.Request) {
		p.peersMu.Lock()
		defer p.peersMu.Unlock()

		p.peers = make(map[types.ValidatorID]string)
		log.Printf("Peer server has been reset.")
	}

	joinHandler := func(w http.ResponseWriter, r *http.Request) {
		idString := r.Header.Get("ID")
		portString := r.Header.Get("PORT")

		id := types.ValidatorID{}
		id.UnmarshalText([]byte(idString))

		p.peersMu.Lock()
		defer p.peersMu.Unlock()

		host, _, _ := net.SplitHostPort(r.RemoteAddr)
		p.peers[id] = net.JoinHostPort(host, portString)
		p.peerTotalOrder[id] = NewTotalOrderResult()
		log.Printf("Peer %s joined from %s", id, p.peers[id])
	}

	peersHandler := func(w http.ResponseWriter, r *http.Request) {
		p.peersMu.Lock()
		defer p.peersMu.Unlock()

		if len(p.peers) != cfg.Validator.Num {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		jsonText, err := json.Marshal(p.peers)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonText)
	}

	infoHandler := func(w http.ResponseWriter, r *http.Request) {
		p.peersMu.Lock()
		defer p.peersMu.Unlock()

		jsonText, err := json.Marshal(p.peers)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(jsonText)
	}

	deliveryHandler := func(w http.ResponseWriter, r *http.Request) {
		idString := r.Header.Get("ID")

		defer r.Body.Close()
		body, err := ioutil.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		m := BlockList{}
		if err := json.Unmarshal(body, &m); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		id := types.ValidatorID{}
		id.UnmarshalText([]byte(idString))

		p.peerTotalOrderMu.Lock()
		defer p.peerTotalOrderMu.Unlock()

		readyForVerify := p.peerTotalOrder[id].PushBlocks(m)
		if !readyForVerify {
			return
		}

		// Verify the total order result.
		go func(id types.ValidatorID) {
			p.peerTotalOrderMu.Lock()
			defer p.peerTotalOrderMu.Unlock()
			var correct bool
			p.peerTotalOrder, correct = VerifyTotalOrder(id, p.peerTotalOrder)
			if !correct {
				log.Printf("The result of Total Ordering Algorithm has error.\n")
			}
		}(id)
	}

	http.HandleFunc("/reset", resetHandler)
	http.HandleFunc("/join", joinHandler)
	http.HandleFunc("/peers", peersHandler)
	http.HandleFunc("/info", infoHandler)
	http.HandleFunc("/delivery", deliveryHandler)

	addr := fmt.Sprintf("0.0.0.0:%d", peerPort)
	log.Printf("Peer server started at %s", addr)

	http.ListenAndServe(addr, nil)
}
