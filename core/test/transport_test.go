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

package test

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type testPeer struct {
	vID               types.ValidatorID
	trans             TransportClient
	recv              <-chan *TransportEnvelope
	expectedEchoHash  common.Hash
	echoBlock         *types.Block
	myBlock           *types.Block
	myBlockSentTime   time.Time
	blocks            map[types.ValidatorID]*types.Block
	blocksReceiveTime map[common.Hash]time.Time
}

type testPeerServer struct {
	trans      TransportServer
	recv       chan *TransportEnvelope
	peerBlocks map[types.ValidatorID]*types.Block
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
	server *testPeerServer,
	peers map[types.ValidatorID]*testPeer,
	delay time.Duration) {

	var (
		req = s.Require()
		wg  sync.WaitGroup
	)

	// For each peers, do following stuffs:
	//  - broadcast 1 block.
	//  - report one random block to server, along with its validator ID.
	// Server would echo the random block back to the peer.
	handleServer := func(server *testPeerServer) {
		defer wg.Done()
		server.peerBlocks = make(map[types.ValidatorID]*types.Block)
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
		peer.blocks = make(map[types.ValidatorID]*types.Block)
		peer.blocksReceiveTime = make(map[common.Hash]time.Time)
		for {
			select {
			case e := <-peer.recv:
				switch v := e.Msg.(type) {
				case *types.Block:
					if v.ProposerID == peer.vID {
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
	for vID, peer := range peers {
		go handlePeer(peer)
		// Broadcast a block.
		peer.myBlock = &types.Block{
			ProposerID: vID,
			Hash:       common.NewRandomHash(),
		}
		peer.myBlockSentTime = time.Now()
		peer.trans.Broadcast(peer.myBlock)
		// Report a block to server.
		peer.expectedEchoHash = common.NewRandomHash()
		peer.trans.Report(&types.Block{
			ProposerID: vID,
			Hash:       peer.expectedEchoHash,
		})
	}
	wg.Wait()
	// Make sure each sent block is received.
	for vID, peer := range peers {
		req.NotNil(peer.echoBlock)
		req.Equal(peer.echoBlock.Hash, peer.expectedEchoHash)
		for otherVID, otherPeer := range peers {
			if vID == otherVID {
				continue
			}
			req.Equal(
				peer.myBlock.Hash,
				otherPeer.blocks[peer.vID].Hash)
		}
	}
	// Make sure the latency is expected.
	for vID, peer := range peers {
		for otherVID, otherPeer := range peers {
			if otherVID == vID {
				continue
			}
			req.True(otherPeer.blocksReceiveTime[peer.myBlock.Hash].Sub(
				peer.myBlockSentTime) >= delay)
		}
	}
}

func (s *TransportTestSuite) TestFake() {
	var (
		peerCount = 13
		req       = s.Require()
		peers     = make(map[types.ValidatorID]*testPeer)
		vIDs      = GenerateRandomValidatorIDs(peerCount)
		err       error
		wg        sync.WaitGroup
		latency   = &FixedLatencyModel{Latency: 300}
		server    = &testPeerServer{trans: NewFakeTransportServer()}
	)
	// Setup PeerServer
	server.recv, err = server.trans.Host()
	req.Nil(err)
	// Setup Peers
	wg.Add(len(vIDs))
	for _, vID := range vIDs {
		peer := &testPeer{
			vID:   vID,
			trans: NewFakeTransportClient(vID, latency),
		}
		peers[vID] = peer
		go func() {
			defer wg.Done()
			recv, err := peer.trans.Join(server.recv)
			req.Nil(err)
			peer.recv = recv
		}()
	}
	// Block here until we collect enough peers.
	server.trans.WaitForPeers(peerCount)
	// Make sure all clients are ready.
	wg.Wait()
	s.baseTest(server, peers, 300*time.Millisecond)
	req.Nil(server.trans.Close())
	for _, peer := range peers {
		req.Nil(peer.trans.Close())
	}
}

func (s *TransportTestSuite) TestTCPLocal() {
	var (
		peerCount  = 25
		req        = s.Require()
		peers      = make(map[types.ValidatorID]*testPeer)
		vIDs       = GenerateRandomValidatorIDs(peerCount)
		err        error
		wg         sync.WaitGroup
		latency    = &FixedLatencyModel{Latency: 300}
		serverPort = 8080
		serverAddr = net.JoinHostPort("0.0.0.0", strconv.Itoa(serverPort))
		server     = &testPeerServer{
			trans: NewTCPTransportServer(&testMarshaller{}, serverPort)}
	)
	// Setup PeerServer
	server.recv, err = server.trans.Host()
	req.Nil(err)
	// Setup Peers
	wg.Add(len(vIDs))
	for _, vID := range vIDs {
		peer := &testPeer{
			vID:   vID,
			trans: NewTCPTransportClient(vID, latency, &testMarshaller{}, true),
		}
		peers[vID] = peer
		go func() {
			defer wg.Done()

			recv, err := peer.trans.Join(serverAddr)
			req.Nil(err)
			peer.recv = recv
		}()
	}
	// Block here until we collect enough peers.
	server.trans.WaitForPeers(peerCount)
	// Make sure all clients are ready.
	wg.Wait()

	s.baseTest(server, peers, 300*time.Millisecond)
	req.Nil(server.trans.Close())
	for _, peer := range peers {
		req.Nil(peer.trans.Close())
	}
}

func TestTransport(t *testing.T) {
	suite.Run(t, new(TransportTestSuite))
}
