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

package test

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

// NetworkType is the simulation network type.
type NetworkType string

// NetworkType enums.
const (
	NetworkTypeTCP      NetworkType = "tcp"
	NetworkTypeTCPLocal NetworkType = "tcp-local"
	NetworkTypeFake     NetworkType = "fake"
)

// NetworkConfig is the configuration for Network module.
type NetworkConfig struct {
	Type       NetworkType
	PeerServer string
	PeerPort   int
}

// Network implements core.Network interface based on TransportClient.
type Network struct {
	config         NetworkConfig
	ctx            context.Context
	ctxCancel      context.CancelFunc
	trans          TransportClient
	fromTransport  <-chan *TransportEnvelope
	toConsensus    chan interface{}
	toNode         chan interface{}
	sentRandomness map[common.Hash]struct{}
	sentAgreement  map[common.Hash]struct{}
	blockCache     map[common.Hash]*types.Block
}

// NewNetwork setup network stuffs for nodes, which provides an
// implementation of core.Network based on TransportClient.
func NewNetwork(pubKey crypto.PublicKey, latency LatencyModel,
	marshaller Marshaller, config NetworkConfig) (n *Network) {
	// Construct basic network instance.
	n = &Network{
		config:         config,
		toConsensus:    make(chan interface{}, 1000),
		toNode:         make(chan interface{}, 1000),
		sentRandomness: make(map[common.Hash]struct{}),
		sentAgreement:  make(map[common.Hash]struct{}),
		blockCache:     make(map[common.Hash]*types.Block),
	}
	n.ctx, n.ctxCancel = context.WithCancel(context.Background())
	// Construct transport layer.
	switch config.Type {
	case NetworkTypeTCPLocal:
		n.trans = NewTCPTransportClient(pubKey, latency, marshaller, true)
	case NetworkTypeTCP:
		n.trans = NewTCPTransportClient(pubKey, latency, marshaller, false)
	case NetworkTypeFake:
		n.trans = NewFakeTransportClient(pubKey, latency)
	default:
		panic(fmt.Errorf("unknown network type: %v", config.Type))
	}
	return
}

// PullBlocks implements core.Network interface.
func (n *Network) PullBlocks(hashes common.Hashes) {
	go func() {
		for _, hash := range hashes {
			// TODO(jimmy-dexon): request block from network instead of cache.
			if block, exist := n.blockCache[hash]; exist {
				n.toConsensus <- block
				continue
			}
			panic(fmt.Errorf("unknown block %s requested", hash))
		}
	}()
}

// PullVotes implements core.Network interface.
func (n *Network) PullVotes(pos types.Position) {
	// TODO(jimmy-dexon): implement this.
}

// BroadcastVote implements core.Network interface.
func (n *Network) BroadcastVote(vote *types.Vote) {
	if err := n.trans.Broadcast(vote); err != nil {
		panic(err)
	}
}

// BroadcastBlock implements core.Network interface.
func (n *Network) BroadcastBlock(block *types.Block) {
	if err := n.trans.Broadcast(block); err != nil {
		panic(err)
	}
}

// BroadcastAgreementResult implements core.Network interface.
func (n *Network) BroadcastAgreementResult(
	randRequest *types.AgreementResult) {
	if _, exist := n.sentAgreement[randRequest.BlockHash]; exist {
		return
	}
	if len(n.sentAgreement) > 1000 {
		// Randomly drop one entry.
		for k := range n.sentAgreement {
			delete(n.sentAgreement, k)
			break
		}
	}
	n.sentAgreement[randRequest.BlockHash] = struct{}{}
	if err := n.trans.Broadcast(randRequest); err != nil {
		panic(err)
	}
}

// BroadcastRandomnessResult implements core.Network interface.
func (n *Network) BroadcastRandomnessResult(
	randResult *types.BlockRandomnessResult) {
	if _, exist := n.sentRandomness[randResult.BlockHash]; exist {
		return
	}
	if len(n.sentRandomness) > 1000 {
		// Randomly drop one entry.
		for k := range n.sentRandomness {
			delete(n.sentRandomness, k)
			break
		}
	}
	n.sentRandomness[randResult.BlockHash] = struct{}{}
	if err := n.trans.Broadcast(randResult); err != nil {
		panic(err)
	}
}

// broadcast message to all other nodes in the network.
func (n *Network) broadcast(message interface{}) {
	if err := n.trans.Broadcast(message); err != nil {
		panic(err)
	}
}

// SendDKGPrivateShare implements core.Network interface.
func (n *Network) SendDKGPrivateShare(
	recv crypto.PublicKey, prvShare *typesDKG.PrivateShare) {
	if err := n.trans.Send(types.NewNodeID(recv), prvShare); err != nil {
		panic(err)
	}
}

// BroadcastDKGPrivateShare implements core.Network interface.
func (n *Network) BroadcastDKGPrivateShare(
	prvShare *typesDKG.PrivateShare) {
	if err := n.trans.Broadcast(prvShare); err != nil {
		panic(err)
	}
}

// BroadcastDKGPartialSignature implements core.Network interface.
func (n *Network) BroadcastDKGPartialSignature(
	psig *typesDKG.PartialSignature) {
	if err := n.trans.Broadcast(psig); err != nil {
		panic(err)
	}
}

// ReceiveChan implements core.Network interface.
func (n *Network) ReceiveChan() <-chan interface{} {
	return n.toConsensus
}

// Setup transport layer.
func (n *Network) Setup(serverEndpoint interface{}) (err error) {
	// Join the p2p network.
	switch n.config.Type {
	case NetworkTypeTCP, NetworkTypeTCPLocal:
		addr := net.JoinHostPort(
			n.config.PeerServer, strconv.Itoa(n.config.PeerPort))
		n.fromTransport, err = n.trans.Join(addr)
	case NetworkTypeFake:
		n.fromTransport, err = n.trans.Join(serverEndpoint)
	default:
		err = fmt.Errorf("unknown network type: %v", n.config.Type)
	}
	if err != nil {
		return
	}
	return
}

func (n *Network) msgHandler(e *TransportEnvelope) {
	switch v := e.Msg.(type) {
	case *types.Block:
		if len(n.blockCache) > 500 {
			// Randomly purge one block from cache.
			for k := range n.blockCache {
				delete(n.blockCache, k)
				break
			}
		}
		n.blockCache[v.Hash] = v
		n.toConsensus <- e.Msg
	case *types.Vote, *types.AgreementResult, *types.BlockRandomnessResult,
		*typesDKG.PrivateShare, *typesDKG.PartialSignature:
		n.toConsensus <- e.Msg
	default:
		n.toNode <- e.Msg
	}
}

// Run the main loop.
func (n *Network) Run() {
Loop:
	for {
		select {
		case <-n.ctx.Done():
			break Loop
		default:
		}
		select {
		case <-n.ctx.Done():
			break Loop
		case e, ok := <-n.fromTransport:
			if !ok {
				break Loop
			}
			n.msgHandler(e)
		}
	}
}

// Close stops the network.
func (n *Network) Close() (err error) {
	n.ctxCancel()
	close(n.toConsensus)
	n.toConsensus = nil
	close(n.toNode)
	n.toNode = nil
	if err = n.trans.Close(); err != nil {
		return
	}
	return
}

// Report exports 'Report' method of TransportClient.
func (n *Network) Report(msg interface{}) error {
	return n.trans.Report(msg)
}

// Peers exports 'Peers' method of Transport.
func (n *Network) Peers() []crypto.PublicKey {
	return n.trans.Peers()
}

// Broadcast exports 'Broadcast' method of Transport, and would panic when
// error.
func (n *Network) Broadcast(msg interface{}) {
	if err := n.trans.Broadcast(msg); err != nil {
		panic(err)
	}
}

// ReceiveChanForNode returns a channel for messages not handled by
// core.Consensus.
func (n *Network) ReceiveChanForNode() <-chan interface{} {
	return n.toNode
}
