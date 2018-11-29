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
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type fakePeerRecord struct {
	sendChannel chan<- *TransportEnvelope
	pubKey      crypto.PublicKey
}

// FakeTransport implement TransportServer and TransportClient interface
// by using golang channel.
type FakeTransport struct {
	peerType      TransportPeerType
	nID           types.NodeID
	pubKey        crypto.PublicKey
	recvChannel   chan *TransportEnvelope
	serverChannel chan<- *TransportEnvelope
	peers         map[types.NodeID]fakePeerRecord
	latency       LatencyModel
}

// NewFakeTransportServer constructs FakeTransport instance for peer server.
func NewFakeTransportServer() TransportServer {
	return &FakeTransport{
		peerType:    TransportPeerServer,
		recvChannel: make(chan *TransportEnvelope, 1000),
	}
}

// NewFakeTransportClient constructs FakeTransport instance for peer.
func NewFakeTransportClient(
	pubKey crypto.PublicKey, latency LatencyModel) TransportClient {

	return &FakeTransport{
		peerType:    TransportPeer,
		recvChannel: make(chan *TransportEnvelope, 1000),
		nID:         types.NewNodeID(pubKey),
		pubKey:      pubKey,
		latency:     latency,
	}
}

// Send implements Transport.Send method.
func (t *FakeTransport) Send(
	endpoint types.NodeID, msg interface{}) (err error) {

	rec, exists := t.peers[endpoint]
	if !exists {
		err = fmt.Errorf("the endpoint does not exists: %v", endpoint)
		return
	}
	go func(ch chan<- *TransportEnvelope) {
		if t.latency != nil {
			time.Sleep(t.latency.Delay())
		}
		ch <- &TransportEnvelope{
			PeerType: t.peerType,
			From:     t.nID,
			Msg:      msg,
		}
	}(rec.sendChannel)
	return
}

// Report implements Transport.Report method.
func (t *FakeTransport) Report(msg interface{}) (err error) {
	go func() {
		t.serverChannel <- &TransportEnvelope{
			PeerType: TransportPeer,
			From:     t.nID,
			Msg:      msg,
		}
	}()
	return
}

// Broadcast implements Transport.Broadcast method.
func (t *FakeTransport) Broadcast(msg interface{}) (err error) {
	for k := range t.peers {
		if k == t.nID {
			continue
		}
		t.Send(k, msg)
	}
	return
}

// Close implements Transport.Close method.
func (t *FakeTransport) Close() (err error) {
	close(t.recvChannel)
	return
}

// Peers implements Transport.Peers method.
func (t *FakeTransport) Peers() (peers []crypto.PublicKey) {
	for _, rec := range t.peers {
		peers = append(peers, rec.pubKey)
	}
	return
}

// Join implements TransportClient.Join method.
func (t *FakeTransport) Join(
	serverEndpoint interface{}) (<-chan *TransportEnvelope, error) {

	var (
		envelopes = []*TransportEnvelope{}
		ok        bool
	)
	if t.serverChannel, ok = serverEndpoint.(chan *TransportEnvelope); !ok {
		return nil, fmt.Errorf("accept channel of *TransportEnvelope when join")
	}
	t.Report(t)
	// Wait for peers info.
	for {
		envelope := <-t.recvChannel
		if envelope.PeerType != TransportPeerServer {
			envelopes = append(envelopes, envelope)
			continue
		}
		if t.peers, ok =
			envelope.Msg.(map[types.NodeID]fakePeerRecord); !ok {

			envelopes = append(envelopes, envelope)
			continue
		}
		for _, envelope := range envelopes {
			t.recvChannel <- envelope
		}
		break
	}
	return t.recvChannel, nil
}

// Host implements TransportServer.Host method.
func (t *FakeTransport) Host() (chan *TransportEnvelope, error) {
	return t.recvChannel, nil
}

// WaitForPeers implements TransportServer.WaitForPeers method.
func (t *FakeTransport) WaitForPeers(numPeers uint32) (err error) {
	t.peers = make(map[types.NodeID]fakePeerRecord)
	for {
		envelope := <-t.recvChannel
		// Panic here if some peer send other stuffs before
		// receiving peer lists.
		newPeer := envelope.Msg.(*FakeTransport)
		t.peers[envelope.From] = fakePeerRecord{
			sendChannel: newPeer.recvChannel,
			pubKey:      newPeer.pubKey,
		}
		if uint32(len(t.peers)) == numPeers {
			break
		}
	}
	// The collected peer channels are shared for all peers.
	if err = t.Broadcast(t.peers); err != nil {
		return
	}
	return
}
