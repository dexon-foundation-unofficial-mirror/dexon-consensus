// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package test

import (
	"fmt"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// FakeTransport implement TransportServer and TransportClient interface
// by using golang channel.
type FakeTransport struct {
	peerType      TransportPeerType
	vID           types.ValidatorID
	recvChannel   chan *TransportEnvelope
	serverChannel chan<- *TransportEnvelope
	peers         map[types.ValidatorID]chan<- *TransportEnvelope
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
	vID types.ValidatorID, latency LatencyModel) TransportClient {

	return &FakeTransport{
		peerType:    TransportPeer,
		recvChannel: make(chan *TransportEnvelope, 1000),
		vID:         vID,
		latency:     latency,
	}
}

// Send implements Transport.Send method.
func (t *FakeTransport) Send(
	endpoint types.ValidatorID, msg interface{}) (err error) {

	ch, exists := t.peers[endpoint]
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
			From:     t.vID,
			Msg:      msg,
		}
	}(ch)
	return
}

// Report implements Transport.Report method.
func (t *FakeTransport) Report(msg interface{}) (err error) {
	go func() {
		t.serverChannel <- &TransportEnvelope{
			PeerType: TransportPeer,
			From:     t.vID,
			Msg:      msg,
		}
	}()
	return
}

// Broadcast implements Transport.Broadcast method.
func (t *FakeTransport) Broadcast(msg interface{}) (err error) {
	for k := range t.peers {
		if k == t.vID {
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
func (t *FakeTransport) Peers() (peers map[types.ValidatorID]struct{}) {
	peers = make(map[types.ValidatorID]struct{})
	for vID := range t.peers {
		peers[vID] = struct{}{}
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
			envelope.Msg.(map[types.ValidatorID]chan<- *TransportEnvelope); !ok {

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
func (t *FakeTransport) WaitForPeers(numPeers int) (err error) {
	t.peers = make(map[types.ValidatorID]chan<- *TransportEnvelope)
	for {
		envelope := <-t.recvChannel
		// Panic here if some peer send other stuffs before
		// receiving peer lists.
		newPeer := envelope.Msg.(*FakeTransport)
		t.peers[envelope.From] = newPeer.recvChannel
		if len(t.peers) == numPeers {
			break
		}
	}
	// The collected peer channels are shared for all peers.
	if err = t.Broadcast(t.peers); err != nil {
		return
	}
	return
}
