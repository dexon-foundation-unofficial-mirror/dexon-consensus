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

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/types/dkg"
)

const (
	tcpThroughputReportNum = 10
)

type tcpHandshake struct {
	DMoment time.Time
	Peers   map[types.NodeID]string
}

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

// BlockEventMessage is for monitoring block events' time.
type BlockEventMessage struct {
	BlockHash  common.Hash `json:"hash"`
	Timestamps []time.Time `json:"timestamps"`
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
	key, err = ecdsa.NewPublicKeyFromByteSlice(data)
	if err != nil {
		panic(err)
	}
	return
}

var (
	// ErrTCPHandShakeFail is reported if the tcp handshake fails.
	ErrTCPHandShakeFail = fmt.Errorf("tcp handshake fail")

	// ErrConnectToUnexpectedPeer is reported if connect to unexpected peer.
	ErrConnectToUnexpectedPeer = fmt.Errorf("connect to unexpected peer")

	// ErrMessageOverflow is reported if the message is too long.
	ErrMessageOverflow = fmt.Errorf("message size overflow")
)

// TCPTransport implements Transport interface via TCP connection.
type TCPTransport struct {
	peerType          TransportPeerType
	nID               types.NodeID
	pubKey            crypto.PublicKey
	localPort         int
	peers             map[types.NodeID]*tcpPeerRecord
	peersLock         sync.RWMutex
	recvChannel       chan *TransportEnvelope
	ctx               context.Context
	cancel            context.CancelFunc
	marshaller        Marshaller
	throughputRecords []ThroughputRecord
	throughputLock    sync.Mutex
	dMoment           time.Time
}

// NewTCPTransport constructs an TCPTransport instance.
func NewTCPTransport(peerType TransportPeerType, pubKey crypto.PublicKey,
	marshaller Marshaller, localPort int) *TCPTransport {
	ctx, cancel := context.WithCancel(context.Background())
	return &TCPTransport{
		peerType:          peerType,
		nID:               types.NewNodeID(pubKey),
		pubKey:            pubKey,
		peers:             make(map[types.NodeID]*tcpPeerRecord),
		recvChannel:       make(chan *TransportEnvelope, 1000),
		ctx:               ctx,
		cancel:            cancel,
		localPort:         localPort,
		marshaller:        marshaller,
		throughputRecords: []ThroughputRecord{},
	}
}

const handshakeMsg = "Welcome to DEXON network for test."

func (t *TCPTransport) serverHandshake(conn net.Conn) (
	nID types.NodeID, err error) {
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		panic(err)
	}
	msg := &tcpMessage{
		NodeID: t.nID,
		Type:   "handshake",
		Info:   handshakeMsg,
	}
	var payload []byte
	payload, err = json.Marshal(msg)
	if err != nil {
		return
	}
	if err = t.write(conn, payload); err != nil {
		return
	}
	if payload, err = t.read(conn); err != nil {
		return
	}
	if err = json.Unmarshal(payload, &msg); err != nil {
		return
	}
	if msg.Type != "handshake-ack" || msg.Info != handshakeMsg {
		err = ErrTCPHandShakeFail
		return
	}
	nID = msg.NodeID
	return
}

func (t *TCPTransport) clientHandshake(conn net.Conn) (
	nID types.NodeID, err error) {
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		panic(err)
	}
	var payload []byte
	if payload, err = t.read(conn); err != nil {
		return
	}
	msg := &tcpMessage{}
	if err = json.Unmarshal(payload, &msg); err != nil {
		return
	}
	if msg.Type != "handshake" || msg.Info != handshakeMsg {
		err = ErrTCPHandShakeFail
		return
	}
	nID = msg.NodeID
	msg = &tcpMessage{
		NodeID: t.nID,
		Type:   "handshake-ack",
		Info:   handshakeMsg,
	}
	payload, err = json.Marshal(msg)
	if err != nil {
		return
	}
	if err = t.write(conn, payload); err != nil {
		return
	}
	return
}

// Disconnect implements Transport.Disconnect method.
func (t *TCPTransport) Disconnect(endpoint types.NodeID) {
	delete(t.peers, endpoint)
}

func (t *TCPTransport) send(
	endpoint types.NodeID, msg interface{}, payload []byte) {
	t.peersLock.RLock()
	defer t.peersLock.RUnlock()
	t.handleThroughputData(msg, payload)
	t.peers[endpoint].sendChannel <- payload
}

// Send implements Transport.Send method.
func (t *TCPTransport) Send(
	endpoint types.NodeID, msg interface{}) (err error) {

	if _, exist := t.peers[endpoint]; !exist {
		return fmt.Errorf("the endpoint does not exists: %v", endpoint)
	}

	payload, err := t.marshalMessage(msg)
	if err != nil {
		return
	}
	go t.send(endpoint, msg, payload)
	return
}

// Broadcast implements Transport.Broadcast method.
func (t *TCPTransport) Broadcast(endpoints map[types.NodeID]struct{},
	latency LatencyModel, msg interface{}) (err error) {
	payload, err := t.marshalMessage(msg)
	if err != nil {
		return
	}
	for nID := range endpoints {
		if nID == t.nID {
			continue
		}
		go func(ID types.NodeID) {
			time.Sleep(latency.Delay())
			t.send(ID, msg, payload)
		}(nID)
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

func (t *TCPTransport) write(conn net.Conn, b []byte) (err error) {
	if len(b) > math.MaxUint32 {
		return ErrMessageOverflow
	}
	msgLength := make([]byte, 4)
	binary.LittleEndian.PutUint32(msgLength, uint32(len(b)))
	if _, err = conn.Write(msgLength); err != nil {
		return
	}
	if _, err = conn.Write(b); err != nil {
		return
	}
	return
}

func (t *TCPTransport) read(conn net.Conn) (b []byte, err error) {
	msgLength := make([]byte, 4)
	if _, err = io.ReadFull(conn, msgLength); err != nil {
		return
	}
	b = make([]byte, int(binary.LittleEndian.Uint32(msgLength)))
	if _, err = io.ReadFull(conn, b); err != nil {
		return
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
	case *tcpHandshake:
		msgCarrier.Type = "tcp-handshake"
	case *tcpMessage:
		msgCarrier.Type = "trans-msg"
	case []ThroughputRecord:
		msgCarrier.Type = "throughput-record"
	case *BlockEventMessage:
		msgCarrier.Type = "block-event"
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
	case "tcp-handshake":
		handshake := &tcpHandshake{}
		if err = json.Unmarshal(msgCarrier.Payload, &handshake); err != nil {
			return
		}
		msg = handshake
	case "trans-msg":
		m := &tcpMessage{}
		if err = json.Unmarshal(msgCarrier.Payload, m); err != nil {
			return
		}
		msg = m
	case "throughput-record":
		m := &[]ThroughputRecord{}
		if err = json.Unmarshal(msgCarrier.Payload, m); err != nil {
			return
		}
		msg = m
	case "block-event":
		m := &BlockEventMessage{}
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
		err     error
		payload []byte
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
		if payload, err = t.read(conn); err != nil {
			if checkErr(err) {
				break
			}
			continue
		}
		peerType, from, msg, err := t.unmarshalMessage(payload)
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
	// Disable write deadline.
	if err := conn.SetWriteDeadline(time.Time{}); err != nil {
		panic(err)
	}

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
				if err := t.write(conn, msg); err != nil {
					panic(err)
				}
			}
		}
	}()
	return ch
}

// listenerRoutine is a routine to accept incoming request for TCP connection.
func (t *TCPTransport) listenerRoutine(listener *net.TCPListener) {
	closed := false
	defer func() {
		if closed {
			return
		}
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

		if err := listener.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
			panic(err)
		}
		conn, err := listener.Accept()
		if err != nil {
			// Check if the connection is closed.
			if strings.Contains(err.Error(), "use of closed network connection") {
				closed = true
				return
			}
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
		if _, err := t.serverHandshake(conn); err != nil {
			fmt.Println(err)
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
	var errs []error
	var errsLock sync.Mutex
	addErr := func(err error) {
		errsLock.Lock()
		defer errsLock.Unlock()
		errs = append(errs, err)
	}
	for nID, rec := range t.peers {
		if nID == t.nID {
			continue
		}
		wg.Add(1)
		go func(nID types.NodeID, addr string) {
			defer wg.Done()
			conn, localErr := net.Dial("tcp", addr)
			if localErr != nil {
				addErr(localErr)
				return
			}
			serverID, localErr := t.clientHandshake(conn)
			if localErr != nil {
				addErr(localErr)
				return
			}
			if nID != serverID {
				addErr(ErrConnectToUnexpectedPeer)
				return
			}
			t.peersLock.Lock()
			defer t.peersLock.Unlock()
			t.peers[nID].sendChannel = t.connWriter(conn)
		}(nID, rec.conn)
	}
	wg.Wait()
	if len(errs) > 0 {
		// Propagate this error to outside, at least one error
		// could be returned to caller.
		err = errs[0]
	}
	return
}

// ThroughputRecord records the network throughput data.
type ThroughputRecord struct {
	Type string    `json:"type"`
	Size int       `json:"size"`
	Time time.Time `json:"time"`
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
	marshaller Marshaller,
	local bool) *TCPTransportClient {

	return &TCPTransportClient{
		TCPTransport: *NewTCPTransport(TransportPeer, pubKey, marshaller, 8080),
		local:        local,
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
		addr = net.JoinHostPort("0.0.0.0", strconv.Itoa(t.localPort))
		ln, err = net.Listen("tcp", addr)
		if err == nil {
			go t.listenerRoutine(ln.(*net.TCPListener))
			// It is possible to listen on the same port in some platform.
			// Check if this one is actually listening.
			testConn, e := net.Dial("tcp", addr)
			if e != nil {
				err = e
				return
			}
			nID, e := t.clientHandshake(testConn)
			if e != nil {
				err = e
				return
			}
			if nID == t.nID {
				break
			}
			// #nosec G104
			ln.Close()
		}
		if err != nil {
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
		}
		// The port is used, generate another port randomly.
		t.localPort = 1024 + rand.Int()%1024 // #nosec G404
	}

	fmt.Println("Connecting to server", "endpoint", serverEndpoint)
	serverConn, err := net.Dial("tcp", serverEndpoint.(string))
	if err != nil {
		return
	}
	_, err = t.clientHandshake(serverConn)
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
	handshake, ok := e.Msg.(*tcpHandshake)
	if !ok {
		panic(fmt.Errorf("expect handshake, not %v", e))
	}
	t.dMoment = handshake.DMoment
	// Setup peers information.
	for nID, info := range handshake.Peers {
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

// Send calls TCPTransport's Send, and send the throughput data to peer server.
func (t *TCPTransportClient) Send(
	endpoint types.NodeID, msg interface{}) (err error) {

	if err := t.TCPTransport.Send(endpoint, msg); err != nil {
		return err
	}
	if len(t.throughputRecords) > tcpThroughputReportNum {
		t.throughputLock.Lock()
		defer t.throughputLock.Unlock()
		if err := t.Report(t.throughputRecords); err != nil {
			panic(err)
		}
		t.throughputRecords = t.throughputRecords[:0]
	}
	return
}

// DMoment implments TransportClient.
func (t *TCPTransportClient) DMoment() time.Time {
	return t.dMoment
}

// TCPTransportServer implements TransportServer via TCP connections.
type TCPTransportServer struct {
	TCPTransport
}

// NewTCPTransportServer constructs TCPTransportServer instance.
func NewTCPTransportServer(
	marshaller Marshaller,
	serverPort int) *TCPTransportServer {

	prvKey, err := ecdsa.NewPrivateKey()
	if err != nil {
		panic(err)
	}
	return &TCPTransportServer{
		// NOTE: the assumption here is the node ID of peers
		//       won't be zero.
		TCPTransport: *NewTCPTransport(
			TransportPeerServer, prvKey.PublicKey(), marshaller, serverPort),
	}
}

// Host implements TransportServer.Host method.
func (t *TCPTransportServer) Host() (chan *TransportEnvelope, error) {
	// The port of peer server should be known to other peers,
	// if we can listen on the pre-defiend part, we don't have to
	// retry with other random ports.
	ln, err := net.Listen(
		"tcp", net.JoinHostPort("0.0.0.0", strconv.Itoa(t.localPort)))
	if err != nil {
		return nil, err
	}
	go t.listenerRoutine(ln.(*net.TCPListener))
	return t.recvChannel, nil
}

// SetDMoment implements TransportServer.SetDMoment method.
func (t *TCPTransportServer) SetDMoment(dMoment time.Time) {
	t.dMoment = dMoment
}

// WaitForPeers implements TransportServer.WaitForPeers method.
func (t *TCPTransportServer) WaitForPeers(numPeers uint32) (err error) {
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
		fmt.Println("Peer connected", "peer", conn)
		t.peers[msg.NodeID] = &tcpPeerRecord{
			conn:   conn,
			pubKey: pubKey,
		}
		peersInfo[msg.NodeID] = msg.Info
		// Check if we already collect enought peers.
		if uint32(len(peersInfo)) == numPeers {
			break
		}
	}
	// Send collected peers back to them.
	if err = t.buildConnectionsToPeers(); err != nil {
		return
	}
	peers := make(map[types.NodeID]struct{})
	for ID := range t.peers {
		peers[ID] = struct{}{}
	}
	handshake := &tcpHandshake{
		DMoment: t.dMoment,
		Peers:   peersInfo,
	}
	if err = t.Broadcast(peers, &FixedLatencyModel{}, handshake); err != nil {
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
		if uint32(len(readies)) == numPeers {
			break
		}
	}
	// Ack all peers ready to go.
	if err = t.Broadcast(peers, &FixedLatencyModel{},
		&tcpMessage{Type: "all-ready"}); err != nil {
		return
	}
	return
}

func (t *TCPTransport) handleThroughputData(msg interface{}, payload []byte) {
	sentTime := time.Now()
	t.throughputLock.Lock()
	defer t.throughputLock.Unlock()
	recordType := ""
	switch msg.(type) {
	case *types.Vote:
		recordType = "vote"
	case *types.Block:
		recordType = "block"
	case *types.AgreementResult:
		recordType = "agreement_result"
	case *dkg.PartialSignature:
		recordType = "partial_sig"
	}
	if len(recordType) > 0 {
		t.throughputRecords = append(t.throughputRecords, ThroughputRecord{
			Type: recordType,
			Time: sentTime,
			Size: len(payload),
		})
	}
}
