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
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sync"

	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/simulation/config"
)

// PeerServer is the main object to collect results and monitor simulation.
type PeerServer struct {
	peers            map[types.NodeID]struct{}
	msgChannel       chan *test.TransportEnvelope
	trans            test.TransportServer
	peerTotalOrder   PeerTotalOrder
	peerTotalOrderMu sync.Mutex
	verifiedLen      uint64
	cfg              *config.Config
	ctx              context.Context
	ctxCancel        context.CancelFunc
}

// NewPeerServer returns a new PeerServer instance.
func NewPeerServer() *PeerServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &PeerServer{
		peers:          make(map[types.NodeID]struct{}),
		peerTotalOrder: make(PeerTotalOrder),
		ctx:            ctx,
		ctxCancel:      cancel,
	}
}

// isNode checks if nID is in p.peers. If peer server restarts but
// nodes are not, it will cause panic if nodes send message.
func (p *PeerServer) isNode(nID types.NodeID) bool {
	_, exist := p.peers[nID]
	return exist
}

// handleBlockList is the handler for messages with BlockList as payload.
func (p *PeerServer) handleBlockList(id types.NodeID, blocks *BlockList) {
	p.peerTotalOrderMu.Lock()
	defer p.peerTotalOrderMu.Unlock()

	readyForVerify := p.peerTotalOrder[id].PushBlocks(*blocks)
	if !readyForVerify {
		return
	}
	// Verify the total order result.
	go func(id types.NodeID) {
		p.peerTotalOrderMu.Lock()
		defer p.peerTotalOrderMu.Unlock()

		var correct bool
		var length int
		p.peerTotalOrder, correct, length = VerifyTotalOrder(id, p.peerTotalOrder)
		if !correct {
			log.Printf("The result of Total Ordering Algorithm has error.\n")
		}
		p.verifiedLen += uint64(length)
		if p.verifiedLen >= p.cfg.Node.MaxBlock {
			if err := p.trans.Broadcast(statusShutdown); err != nil {
				panic(err)
			}
		}
	}(id)
}

// handleMessage is the handler for messages with Message as payload.
func (p *PeerServer) handleMessage(id types.NodeID, m *message) {
	switch m.Type {
	case shutdownAck:
		delete(p.peers, id)
		log.Printf("%v shutdown, %d remains.\n", id, len(p.peers))
		if len(p.peers) == 0 {
			p.ctxCancel()
		}
	case blockTimestamp:
		msgs := []timestampMessage{}
		if err := json.Unmarshal(m.Payload, &msgs); err != nil {
			panic(err)
		}
		for _, msg := range msgs {
			if ok := p.peerTotalOrder[id].PushTimestamp(msg); !ok {
				panic(fmt.Errorf("unable to push timestamp: %v", m))
			}
		}
	default:
		panic(fmt.Errorf("unknown simulation message type: %v", m))
	}
}

func (p *PeerServer) mainLoop() {
	for {
		select {
		case <-p.ctx.Done():
			return
		default:
		}
		select {
		case <-p.ctx.Done():
			return
		case e := <-p.msgChannel:
			if !p.isNode(e.From) {
				break
			}
			// Handle messages based on their type.
			switch val := e.Msg.(type) {
			case *BlockList:
				p.handleBlockList(e.From, val)
			case *message:
				p.handleMessage(e.From, val)
			default:
				panic(fmt.Errorf("unknown message: %v", reflect.TypeOf(e.Msg)))
			}
		}
	}
}

// Setup prepares simualtion.
func (p *PeerServer) Setup(
	cfg *config.Config) (serverEndpoint interface{}, err error) {
	// Setup transport layer.
	switch cfg.Networking.Type {
	case "tcp", "tcp-local":
		p.trans = test.NewTCPTransportServer(&jsonMarshaller{}, peerPort)
	case "fake":
		p.trans = test.NewFakeTransportServer()
	default:
		panic(fmt.Errorf("unknown network type: %v", cfg.Networking.Type))
	}
	p.msgChannel, err = p.trans.Host()
	if err != nil {
		return
	}
	p.cfg = cfg
	serverEndpoint = p.msgChannel
	return
}

// Run the simulation.
func (p *PeerServer) Run() {
	if err := p.trans.WaitForPeers(p.cfg.Node.Num); err != nil {
		panic(err)
	}
	// Cache peers' info.
	for _, pubKey := range p.trans.Peers() {
		p.peers[types.NewNodeID(pubKey)] = struct{}{}
	}
	// Initialize total order result cache.
	for id := range p.peers {
		p.peerTotalOrder[id] = NewTotalOrderResult(id)
	}
	// Block to handle incoming messages.
	p.mainLoop()
	// The simulation is done, clean up.
	LogStatus(p.peerTotalOrder)
	if err := p.trans.Close(); err != nil {
		log.Printf("Error shutting down peerServer: %v\n", err)
	}
}
