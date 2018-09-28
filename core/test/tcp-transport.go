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
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type tcpPeerRecord struct {
	conn        string
	sendChannel chan<- []byte
	pubKey      crypto.PublicKey
}

// tcpMessage is the general message between peers and server.
type tcpMessage struct {
	NodeID types.NodeID `json:"nid"`
	Type   string       `json:"type"`
	Info   string       `json:"conn"`
}

// buildPeerInfo is a tricky way to combine connection string and
// base64 encoded byte slice for public key into a single string,
// separated by ';'.
func buildPeerInfo(pubKey crypto.PublicKey, conn string) string {
	return conn + ";" + base64.StdEncoding.EncodeToString(pubKey.Bytes())
}

// parsePeerInfo parse connection string and base64 encoded public key built
// via buildPeerInfo.
func parsePeerInfo(info string) (key crypto.PublicKey, conn string) {
	tokens := strings.Split(info, ";")
	conn = tokens[0]
	data, err := base64.StdEncoding.DecodeString(tokens[1])
	if err != nil {
		panic(err)
	}
	key = ecdsa.NewPublicKeyFromByteSlice(data)
	return
}

// TCPTransport implements Transport interface via TCP connection.
type TCPTransport struct {
	peerType    TransportPeerType
	nID         types.NodeID
	pubKey      crypto.PublicKey
	localPort   int
	peers       map[types.NodeID]*tcpPeerRecord
	peersLock   sync.RWMutex
	recvChannel chan *TransportEnvelope
	ctx         context.Context
	cancel      context.CancelFunc
	latency     LatencyModel
	marshaller  Marshaller
}

// NewTCPTransport constructs an TCPTransport instance.
func NewTCPTransport(
	peerType TransportPeerType,
	pubKey crypto.PublicKey,
	latency LatencyModel,
	marshaller Marshaller,
	localPort int) *TCPTransport {

	ctx, cancel := context.WithCancel(context.Background())
	return &TCPTransport{
		peerType:    peerType,
		nID:         types.NewNodeID(pubKey),
		pubKey:      pubKey,
		peers:       make(map[types.NodeID]*tcpPeerRecord),
		recvChannel: make(chan *TransportEnvelope, 1000),
		ctx:         ctx,
		cancel:      cancel,
		localPort:   localPort,
		latency:     latency,
		marshaller:  marshaller,
	}
}

// Send implements Transport.Send method.
func (t *TCPTransport) Send(
	endpoint types.NodeID, msg interface{}) (err error) {

	payload, err := t.marshalMessage(msg)
	if err != nil {
		return
	}
	go func() {
		if t.latency != nil {
			time.Sleep(t.latency.Delay())
		}

		t.peersLock.RLock()
		defer t.peersLock.RUnlock()

		t.peers[endpoint].sendChannel <- payload
	}()
	return
}

// Broadcast implements Transport.Broadcast method.
func (t *TCPTransport) Broadcast(msg interface{}) (err error) {
	payload, err := t.marshalMessage(msg)
	if err != nil {
		return
	}
	t.peersLock.RLock()
	defer t.peersLock.RUnlock()

	for nID, rec := range t.peers {
		if nID == t.nID {
			continue
		}
		go func(ch chan<- []byte) {
			if t.latency != nil {
				time.Sleep(t.latency.Delay())
			}
			ch <- payload
		}(rec.sendChannel)
	}
	return
}

// Close implements Transport.Close method.
func (t *TCPTransport) Close() (err error) {
	// Tell all routines raised by us to die.
	t.cancel()
	// Reset peers.
	t.peersLock.Lock()
	defer t.peersLock.Unlock()
	t.peers = make(map[types.NodeID]*tcpPeerRecord)
	// Tell our user that this channel is closed.
	close(t.recvChannel)
	t.recvChannel = nil
	return
}

// Peers implements Transport.Peers method.
func (t *TCPTransport) Peers() (peers []crypto.PublicKey) {
	for _, rec := range t.peers {
		peers = append(peers, rec.pubKey)
	}
	return
}

func (t *TCPTransport) marshalMessage(
	msg interface{}) (payload []byte, err error) {

	msgCarrier := struct {
		PeerType TransportPeerType `json:"peer_type"`
		From     types.NodeID      `json:"from"`
		Type     string            `json:"type"`
		Payload  interface{}       `json:"payload"`
	}{
		PeerType: t.peerType,
		From:     t.nID,
		Payload:  msg,
	}
	switch msg.(type) {
	case map[types.NodeID]string:
		msgCarrier.Type = "peerlist"
	case *tcpMessage:
		msgCarrier.Type = "trans-msg"
	default:
		if t.marshaller == nil {
			err = fmt.Errorf("unknown msg type: %v", msg)
			break
		}
		// Delegate to user defined marshaller.
		var buff []byte
		msgCarrier.Type, buff, err = t.marshaller.Marshal(msg)
		if err != nil {
			break
		}
		msgCarrier.Payload = json.RawMessage(buff)
	}
	if err != nil {
		return
	}
	payload, err = json.Marshal(msgCarrier)
	return
}

func (t *TCPTransport) unmarshalMessage(
	payload []byte) (
	peerType TransportPeerType,
	from types.NodeID,
	msg interface{},
	err error) {

	msgCarrier := struct {
		PeerType TransportPeerType `json:"peer_type"`
		From     types.NodeID      `json:"from"`
		Type     string            `json:"type"`
		Payload  json.RawMessage   `json:"payload"`
	}{}
	if err = json.Unmarshal(payload, &msgCarrier); err != nil {
		return
	}
	peerType = msgCarrier.PeerType
	from = msgCarrier.From
	switch msgCarrier.Type {
	case "peerlist":
		var peers map[types.NodeID]string
		if err = json.Unmarshal(msgCarrier.Payload, &peers); err != nil {
			return
		}
		msg = peers
	case "trans-msg":
		m := &tcpMessage{}
		if err = json.Unmarshal(msgCarrier.Payload, m); err != nil {
			return
		}
		msg = m
	default:
		if t.marshaller == nil {
			err = fmt.Errorf("unknown msg type: %v", msgCarrier.Type)
			break
		}
		msg, err = t.marshaller.Unmarshal(msgCarrier.Type, msgCarrier.Payload)
	}
	return
}

// connReader is a reader routine to read from a TCP connection.
func (t *TCPTransport) connReader(conn net.Conn) {
	defer func() {
		if err := conn.Close(); err != nil {
			panic(err)
		}
	}()

	var (
		msgLengthInByte [4]byte
		msgLength       uint32
		err             error
		payload         = make([]byte, 4096)
	)

	checkErr := func(err error) (toBreak bool) {
		if err == io.EOF {
			toBreak = true
			return
		}
		// Check if timeout.
		nErr, ok := err.(*net.OpError)
		if !ok {
			panic(err)
		}
		if !nErr.Timeout() {
			panic(err)
		}
		return
	}
Loop:
	for {
		select {
		case <-t.ctx.Done():
			break Loop
		default:
		}
		// Add timeout when reading to check if shutdown.
		if err := conn.SetReadDeadline(
			time.Now().Add(2 * time.Second)); err != nil {

			panic(err)
		}
		// Read message length.
		if _, err = io.ReadFull(conn, msgLengthInByte[:]); err != nil {
			if checkErr(err) {
				break
			}
			continue
		}
		msgLength = binary.LittleEndian.Uint32(msgLengthInByte[:])
		// Resize buffer
		if msgLength > uint32(len(payload)) {
			payload = make([]byte, msgLength)
		}
		buff := payload[:msgLength]
		// Read the message in bytes.
		if _, err = io.ReadFull(conn, buff); err != nil {
			if checkErr(err) {
				break
			}
			continue
		}
		peerType, from, msg, err := t.unmarshalMessage(buff)
		if err != nil {
			panic(err)
		}
		t.recvChannel <- &TransportEnvelope{
			PeerType: peerType,
			From:     from,
			Msg:      msg,
		}
	}
}

// connWriter is a writer routine to write to TCP connection.
func (t *TCPTransport) connWriter(conn net.Conn) chan<- []byte {
	ch := make(chan []byte, 1000)
	go func() {
		defer func() {
			close(ch)
			if err := conn.Close(); err != nil {
				panic(err)
			}
		}()
		for {
			select {
			case <-t.ctx.Done():
				return
			default:
			}
			select {
			case <-t.ctx.Done():
				return
			case msg := <-ch:
				// Send message length in uint32.
				var msgLength [4]byte
				if len(msg) > math.MaxUint32 {
					panic(fmt.Errorf("message size overflow"))
				}
				binary.LittleEndian.PutUint32(msgLength[:], uint32(len(msg)))
				if _, err := conn.Write(msgLength[:]); err != nil {
					panic(err)
				}
				// Send the payload.
				if _, err := conn.Write(msg); err != nil {
					panic(err)
				}
			}
		}
	}()
	return ch
}

// listenerRoutine is a routine to accept incoming request for TCP connection.
func (t *TCPTransport) listenerRoutine(listener *net.TCPListener) {
	defer func() {
		if err := listener.Close(); err != nil {
			panic(err)
		}
	}()
	for {
		select {
		case <-t.ctx.Done():
			return
		default:
		}

		listener.SetDeadline(time.Now().Add(5 * time.Second))
		conn, err := listener.Accept()
		if err != nil {
			// Check if timeout error.
			nErr, ok := err.(*net.OpError)
			if !ok {
				panic(err)
			}
			if !nErr.Timeout() {
				panic(err)
			}
			continue
		}
		go t.connReader(conn)
	}
}

// buildConnectionToPeers constructs TCP connections to each peer.
// Although TCP connection could be used for both read/write operation,
// we only utilize the write part for simplicity.
func (t *TCPTransport) buildConnectionsToPeers() (err error) {
	var wg sync.WaitGroup
	for nID, rec := range t.peers {
		if nID == t.nID {
			continue
		}
		wg.Add(1)
		go func(nID types.NodeID, addr string) {
			defer wg.Done()

			conn, localErr := net.Dial("tcp", addr)
			if localErr != nil {
				// Propagate this error to outside, at least one error
				// could be returned to caller.
				err = localErr
				return
			}
			t.peersLock.Lock()
			defer t.peersLock.Unlock()

			t.peers[nID].sendChannel = t.connWriter(conn)
		}(nID, rec.conn)
	}
	wg.Wait()
	return
}

// TCPTransportClient implement TransportClient base on TCP connection.
type TCPTransportClient struct {
	TCPTransport
	local              bool
	serverWriteChannel chan<- []byte
}

// NewTCPTransportClient constructs a TCPTransportClient instance.
func NewTCPTransportClient(
	pubKey crypto.PublicKey,
	latency LatencyModel,
	marshaller Marshaller,
	local bool) *TCPTransportClient {

	return &TCPTransportClient{
		TCPTransport: *NewTCPTransport(
			TransportPeer, pubKey, latency, marshaller, 8080),
		local: local,
	}
}

// Report implements TransportClient.Report method.
func (t *TCPTransportClient) Report(msg interface{}) (err error) {
	payload, err := t.marshalMessage(msg)
	if err != nil {
		return
	}
	go func() {
		t.serverWriteChannel <- payload
	}()
	return
}

// Join implements TransportClient.Join method.
func (t *TCPTransportClient) Join(
	serverEndpoint interface{}) (ch <-chan *TransportEnvelope, err error) {
	// Initiate a TCP server.
	// TODO(mission): config initial listening port.
	var (
		ln        net.Listener
		envelopes = []*TransportEnvelope{}
		ok        bool
		addr      string
		conn      string
	)
	for {
		addr = net.JoinHostPort("127.0.0.1", strconv.Itoa(t.localPort))
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			break
		}
		if !t.local {
			return
		}
		// In local-tcp, retry with other port when the address is in use.
		operr, ok := err.(*net.OpError)
		if !ok {
			panic(err)
		}
		oserr, ok := operr.Err.(*os.SyscallError)
		if !ok {
			panic(operr)
		}
		errno, ok := oserr.Err.(syscall.Errno)
		if !ok {
			panic(oserr)
		}
		if errno != syscall.EADDRINUSE {
			panic(errno)
		}
		// The port is used, generate another port randomly.
		t.localPort = 1024 + rand.Int()%1024
	}
	go t.listenerRoutine(ln.(*net.TCPListener))
	serverConn, err := net.Dial("tcp", serverEndpoint.(string))
	if err != nil {
		return
	}
	t.serverWriteChannel = t.connWriter(serverConn)
	if t.local {
		conn = addr
	} else {
		// Find my IP.
		var ip string
		if ip, err = FindMyIP(); err != nil {
			return
		}
		conn = net.JoinHostPort(ip, strconv.Itoa(t.localPort))
	}
	if err = t.Report(&tcpMessage{
		NodeID: t.nID,
		Type:   "conn",
		Info:   buildPeerInfo(t.pubKey, conn),
	}); err != nil {
		return
	}
	// Wait for peers list sent by server.
	e := <-t.recvChannel
	peersInfo, ok := e.Msg.(map[types.NodeID]string)
	if !ok {
		panic(fmt.Errorf("expect peer list, not %v", e))
	}
	// Setup peers information.
	for nID, info := range peersInfo {
		pubKey, conn := parsePeerInfo(info)
		t.peers[nID] = &tcpPeerRecord{
			conn:   conn,
			pubKey: pubKey,
		}
	}
	// Setup connections to other peers.
	if err = t.buildConnectionsToPeers(); err != nil {
		return
	}
	// Report to server that the connections to other peers are ready.
	if err = t.Report(&tcpMessage{
		Type:   "conn-ready",
		NodeID: t.nID,
	}); err != nil {
		return
	}
	// Wait for server to ack us that all peers are ready.
	for {
		e := <-t.recvChannel
		msg, ok := e.Msg.(*tcpMessage)
		if !ok {
			envelopes = append(envelopes, e)
			continue
		}
		if msg.Type != "all-ready" {
			err = fmt.Errorf("expected ready message, but %v", msg)
			return
		}
		break
	}
	// Replay those messages sent before peer list and ready-ack.
	for _, e := range envelopes {
		t.recvChannel <- e
	}
	ch = t.recvChannel
	return
}

// TCPTransportServer implements TransportServer via TCP connections.
type TCPTransportServer struct {
	TCPTransport
}

// NewTCPTransportServer constructs TCPTransportServer instance.
func NewTCPTransportServer(
	marshaller Marshaller,
	serverPort int) *TCPTransportServer {

	return &TCPTransportServer{
		// NOTE: the assumption here is the node ID of peers
		//       won't be zero.
		TCPTransport: *NewTCPTransport(
			TransportPeerServer,
			ecdsa.NewPublicKeyFromByteSlice(nil),
			nil,
			marshaller,
			serverPort),
	}
}

// Host implements TransportServer.Host method.
func (t *TCPTransportServer) Host() (chan *TransportEnvelope, error) {
	// The port of peer server should be known to other peers,
	// if we can listen on the pre-defiend part, we don't have to
	// retry with other random ports.
	ln, err := net.Listen(
		"tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(t.localPort)))
	if err != nil {
		return nil, err
	}
	go t.listenerRoutine(ln.(*net.TCPListener))
	return t.recvChannel, nil
}

// WaitForPeers implements TransportServer.WaitForPeers method.
func (t *TCPTransportServer) WaitForPeers(numPeers int) (err error) {
	// Collect peers info. Packets other than peer info is
	// unexpected.
	peersInfo := make(map[types.NodeID]string)
	for {
		// Wait for connection info reported by peers.
		e := <-t.recvChannel
		msg, ok := e.Msg.(*tcpMessage)
		if !ok {
			panic(fmt.Errorf("expect tcpMessage, not %v", e))
		}
		if msg.Type != "conn" {
			panic(fmt.Errorf("expect connection report, not %v", e))
		}
		pubKey, conn := parsePeerInfo(msg.Info)
		t.peers[msg.NodeID] = &tcpPeerRecord{
			conn:   conn,
			pubKey: pubKey,
		}
		peersInfo[msg.NodeID] = msg.Info
		// Check if we already collect enought peers.
		if len(peersInfo) == numPeers {
			break
		}
	}
	// Send collected peers back to them.
	if err = t.buildConnectionsToPeers(); err != nil {
		return
	}
	if err = t.Broadcast(peersInfo); err != nil {
		return
	}
	// Wait for peers to send 'ready' report.
	readies := make(map[types.NodeID]struct{})
	for {
		e := <-t.recvChannel
		msg, ok := e.Msg.(*tcpMessage)
		if !ok {
			panic(fmt.Errorf("expect tcpMessage, not %v", e))
		}
		if msg.Type != "conn-ready" {
			panic(fmt.Errorf("expect connection ready, not %v", e))
		}
		if _, reported := readies[msg.NodeID]; reported {
			panic(fmt.Errorf("already report conn-ready message: %v", e))
		}
		readies[msg.NodeID] = struct{}{}
		if len(readies) == numPeers {
			break
		}
	}
	// Ack all peers ready to go.
	if err = t.Broadcast(&tcpMessage{Type: "all-ready"}); err != nil {
		return
	}
	return
}
