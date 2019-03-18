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
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type testPeer struct {
	nID               types.NodeID
	trans             TransportClient
	recv              <-chan *TransportEnvelope
	expectedEchoHash  common.Hash
	echoBlock         *types.Block
	myBlock           *types.Block
	myBlockSentTime   time.Time
	blocks            map[types.NodeID]*types.Block
	blocksReceiveTime map[common.Hash]time.Time
}

type testPeerServer struct {
	trans      TransportServer
	recv       chan *TransportEnvelope
	peerBlocks map[types.NodeID]*types.Block
}

type testMarshaller struct{}

func (m *testMarshaller) Unmarshal(
	msgType string, payload []byte) (msg interface{}, err error) {

	switch msgType {
	case "block":
		block := &types.Block{}
		if err = json.Unmarshal(payload, block); err != nil {
			return
		}
		msg = block
	default:
		err = fmt.Errorf("unknown message type: %v", msgType)
	}
	return
}

func (m *testMarshaller) Marshal(
	msg interface{}) (msgType string, payload []byte, err error) {

	switch msg.(type) {
	case *types.Block:
		if payload, err = json.Marshal(msg); err != nil {
			return
		}
		msgType = "block"
	default:
		err = fmt.Errorf("unknown message type: %v", msg)
	}
	return
}

type TransportTestSuite struct {
	suite.Suite
}

func (s *TransportTestSuite) baseTest(
	server *testPeerServer, peers map[types.NodeID]*testPeer, delay float64) {
	var (
		req           = s.Require()
		delayDuration = time.Duration(delay) * time.Millisecond
		wg            sync.WaitGroup
	)

	// For each peers, do following stuffs:
	//  - broadcast 1 block.
	//  - report one random block to server, along with its node ID.
	// Server would echo the random block back to the peer.
	handleServer := func(server *testPeerServer) {
		defer wg.Done()
		server.peerBlocks = make(map[types.NodeID]*types.Block)
		for {
			select {
			case e := <-server.recv:
				req.Equal(e.PeerType, TransportPeer)
				switch v := e.Msg.(type) {
				case *types.Block:
					req.Equal(v.ProposerID, e.From)
					server.peerBlocks[v.ProposerID] = v
					// Echo the block back
					server.trans.Send(v.ProposerID, v)
				}
			}
			// Upon receiving blocks from all peers, stop.
			if len(server.peerBlocks) == len(peers) {
				return
			}
		}
	}
	handlePeer := func(peer *testPeer) {
		defer wg.Done()
		peer.blocks = make(map[types.NodeID]*types.Block)
		peer.blocksReceiveTime = make(map[common.Hash]time.Time)
		for {
			select {
			case e := <-peer.recv:
				switch v := e.Msg.(type) {
				case *types.Block:
					if v.ProposerID == peer.nID {
						req.Equal(e.PeerType, TransportPeerServer)
						peer.echoBlock = v
					} else {
						req.Equal(e.PeerType, TransportPeer)
						req.Equal(e.From, v.ProposerID)
						peer.blocks[v.ProposerID] = v
						peer.blocksReceiveTime[v.Hash] = time.Now()
					}
				}
			}
			// Upon receiving blocks from all other peers, and echoed from
			// server, stop.
			if peer.echoBlock != nil && len(peer.blocks) == len(peers)-1 {
				return
			}
		}
	}
	wg.Add(len(peers) + 1)
	go handleServer(server)
	peersAsMap := make(map[types.NodeID]struct{})
	for nID := range peers {
		peersAsMap[nID] = struct{}{}
	}
	for nID, peer := range peers {
		go handlePeer(peer)
		// Broadcast a block.
		peer.myBlock = &types.Block{
			ProposerID: nID,
			Hash:       common.NewRandomHash(),
		}
		peer.myBlockSentTime = time.Now()
		peer.trans.Broadcast(
			peersAsMap, &FixedLatencyModel{Latency: delay}, peer.myBlock)
		// Report a block to server.
		peer.expectedEchoHash = common.NewRandomHash()
		peer.trans.Report(&types.Block{
			ProposerID: nID,
			Hash:       peer.expectedEchoHash,
		})
	}
	wg.Wait()
	// Make sure each sent block is received.
	for nID, peer := range peers {
		req.NotNil(peer.echoBlock)
		req.Equal(peer.echoBlock.Hash, peer.expectedEchoHash)
		for othernID, otherPeer := range peers {
			if nID == othernID {
				continue
			}
			req.Equal(
				peer.myBlock.Hash,
				otherPeer.blocks[peer.nID].Hash)
		}
	}
	// Make sure the latency is expected.
	for nID, peer := range peers {
		for othernID, otherPeer := range peers {
			if othernID == nID {
				continue
			}
			req.True(otherPeer.blocksReceiveTime[peer.myBlock.Hash].Sub(
				peer.myBlockSentTime) >= delayDuration)
		}
	}
}

func (s *TransportTestSuite) TestFake() {
	var (
		peerCount = 13
		req       = s.Require()
		peers     = make(map[types.NodeID]*testPeer)
		prvKeys   = GenerateRandomPrivateKeys(peerCount)
		err       error
		wg        sync.WaitGroup
		server    = &testPeerServer{trans: NewFakeTransportServer()}
	)
	// Setup PeerServer
	server.recv, err = server.trans.Host()
	req.Nil(err)
	// Setup Peers
	wg.Add(len(prvKeys))
	for _, key := range prvKeys {
		nID := types.NewNodeID(key.PublicKey())
		peer := &testPeer{
			nID:   nID,
			trans: NewFakeTransportClient(key.PublicKey()),
		}
		peers[nID] = peer
		go func() {
			defer wg.Done()
			recv, err := peer.trans.Join(server.recv)
			req.Nil(err)
			peer.recv = recv
		}()
	}
	// Block here until we collect enough peers.
	server.trans.WaitForPeers(uint32(peerCount))
	// Make sure all clients are ready.
	wg.Wait()
	s.baseTest(server, peers, 300)
	req.Nil(server.trans.Close())
	for _, peer := range peers {
		req.Nil(peer.trans.Close())
	}
}

func (s *TransportTestSuite) TestTCPLocal() {

	var (
		peerCount  = 13
		req        = s.Require()
		peers      = make(map[types.NodeID]*testPeer)
		prvKeys    = GenerateRandomPrivateKeys(peerCount)
		err        error
		wg         sync.WaitGroup
		serverPort = 8080
		serverAddr = net.JoinHostPort("127.0.0.1", strconv.Itoa(serverPort))
		server     = &testPeerServer{
			trans: NewTCPTransportServer(&testMarshaller{}, serverPort)}
	)
	// Setup PeerServer
	server.recv, err = server.trans.Host()
	req.Nil(err)
	// Setup Peers
	wg.Add(len(prvKeys))
	for _, prvKey := range prvKeys {
		nID := types.NewNodeID(prvKey.PublicKey())
		peer := &testPeer{
			nID: nID,
			trans: NewTCPTransportClient(
				prvKey.PublicKey(), &testMarshaller{}, true),
		}
		peers[nID] = peer
		go func() {
			defer wg.Done()

			recv, err := peer.trans.Join(serverAddr)
			req.Nil(err)
			peer.recv = recv
		}()
	}
	// Block here until we collect enough peers.
	server.trans.WaitForPeers(uint32(peerCount))
	// Make sure all clients are ready.
	wg.Wait()

	s.baseTest(server, peers, 300)
	req.Nil(server.trans.Close())
	for _, peer := range peers {
		req.Nil(peer.trans.Close())
	}
}

func TestTransport(t *testing.T) {
	suite.Run(t, new(TransportTestSuite))
}
