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
	"io"
	"io/ioutil"
	"math/rand"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

const retries = 5

// TCPNetwork implements the Network interface.
type TCPNetwork struct {
	local    bool
	port     int
	endpoint Endpoint
	client   *http.Client

	peerServer    string
	endpointMutex sync.RWMutex
	endpoints     map[types.ValidatorID]string
	recieveChan   chan interface{}
	model         Model
}

// NewTCPNetwork returns pointer to a new Network instance.
func NewTCPNetwork(local bool, peerServer string, model Model) *TCPNetwork {
	pServer := peerServer
	if local {
		pServer = "127.0.0.1"
	}
	// Force connection reuse.
	tr := &http.Transport{
		MaxIdleConnsPerHost: 1024,
		TLSHandshakeTimeout: 0 * time.Second,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
	}
	return &TCPNetwork{
		local:       local,
		peerServer:  pServer,
		client:      client,
		endpoints:   make(map[types.ValidatorID]string),
		recieveChan: make(chan interface{}, msgBufferSize),
		model:       model,
	}
}

// Start starts the http server for accepting message.
func (n *TCPNetwork) Start() {
	listenSuccess := make(chan struct{})
	go func() {
		for {
			ctx, cancel := context.WithTimeout(context.Background(),
				50*time.Millisecond)
			defer cancel()
			go func() {
				<-ctx.Done()
				if ctx.Err() != context.Canceled {
					listenSuccess <- struct{}{}
				}
			}()
			port := 1024 + rand.Int()%1024
			if !n.local {
				port = peerPort
			}
			addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
			server := &http.Server{
				Addr:    addr,
				Handler: n,
			}

			n.port = port
			if err := server.ListenAndServe(); err != nil {
				cancel()
				if err == http.ErrServerClosed {
					break
				}
				if !n.local {
					panic(err)
				}
				// In local-tcp, retry with other port.
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
		}
	}()
	<-listenSuccess
	fmt.Printf("Validator started at 0.0.0.0:%d\n", n.port)
}

// NumPeers returns the number of peers in the network.
func (n *TCPNetwork) NumPeers() int {
	n.endpointMutex.Lock()
	defer n.endpointMutex.Unlock()

	return len(n.endpoints)
}

// ServerHTTP implements the http.Handler interface.
func (n *TCPNetwork) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	m := struct {
		Type    string          `json:"type"`
		Payload json.RawMessage `json:"payload"`
	}{}
	if err := json.Unmarshal(body, &m); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	switch m.Type {
	case "block":
		block := &types.Block{}
		if err := json.Unmarshal(m.Payload, block); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.recieveChan <- block
	case "vote":
		vote := &types.Vote{}
		if err := json.Unmarshal(m.Payload, vote); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.recieveChan <- vote
	case "notaryAck":
		ack := &types.NotaryAck{}
		if err := json.Unmarshal(m.Payload, ack); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		n.recieveChan <- ack
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

// Join allow a client to join the network. It reutnrs a interface{} channel for
// the client to recieve information.
func (n *TCPNetwork) Join(endpoint Endpoint) {
	n.endpointMutex.Lock()
	defer n.endpointMutex.Unlock()

	n.endpoint = endpoint

	joinURL := fmt.Sprintf("http://%s:%d/join", n.peerServer, peerPort)
	peersURL := fmt.Sprintf("http://%s:%d/peers", n.peerServer, peerPort)

	// Join the peer list.
	for {
		time.Sleep(time.Second)

		req, err := http.NewRequest(http.MethodGet, joinURL, nil)
		if err != nil {
			continue
		}
		req.Header.Add("ID", endpoint.GetID().String())
		req.Header.Add("PORT", fmt.Sprintf("%d", n.port))

		resp, err := n.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			io.Copy(ioutil.Discard, resp.Body)
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			break
		}
	}

	var peerList map[types.ValidatorID]string

	// Wait for the server to collect all validators and return a list.
	for {
		time.Sleep(time.Second)

		req, err := http.NewRequest(http.MethodGet, peersURL, nil)
		if err != nil {
			fmt.Println(err)
			continue
		}
		resp, err := n.client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			continue
		}

		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)

		if err := json.Unmarshal(body, &peerList); err != nil {
			fmt.Printf("error: %v", err)
			continue
		}
		break
	}

	for key, val := range peerList {
		n.endpoints[key] = val
	}
}

// ReceiveChan return the receive channel.
func (n *TCPNetwork) ReceiveChan() <-chan interface{} {
	return n.recieveChan
}

// Send sends a msg to another client.
func (n *TCPNetwork) Send(destID types.ValidatorID, messageJSON []byte) {
	clientAddr, exists := n.endpoints[destID]
	if !exists {
		return
	}

	msgURL := fmt.Sprintf("http://%s/msg", clientAddr)
	go func() {
		time.Sleep(n.model.Delay())
		for i := 0; i < retries; i++ {
			req, err := http.NewRequest(
				http.MethodPost, msgURL, strings.NewReader(string(messageJSON)))
			if err != nil {
				continue
			}
			req.Header.Add("ID", n.endpoint.GetID().String())

			resp, err := n.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				io.Copy(ioutil.Discard, resp.Body)
			}
			if err == nil && resp.StatusCode == http.StatusOK {
				runtime.Goexit()
			}

			fmt.Printf("failed to submit message: %s\n", err)
			time.Sleep(1 * time.Second)
		}
		fmt.Printf("failed to send message: %v\n", string(messageJSON))
	}()
}

func (n *TCPNetwork) marshalMessage(msg interface{}) (messageJSON []byte) {
	message := struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}{}

	switch v := msg.(type) {
	case *types.Block:
		message.Type = "block"
		message.Payload = v
	case *types.NotaryAck:
		message.Type = "notaryAck"
		message.Payload = v
	case *types.Vote:
		message.Type = "vote"
		message.Payload = v
	default:
		fmt.Println("error: invalid message type")
		return
	}

	messageJSON, err := json.Marshal(message)
	if err != nil {
		fmt.Printf("error: failed to marshal json: %v\n%+v\n", err, message)
		return
	}
	return
}

// BroadcastBlock broadcast blocks into the network.
func (n *TCPNetwork) BroadcastBlock(block *types.Block) {
	payload := n.marshalMessage(block)
	for endpoint := range n.endpoints {
		if endpoint == block.ProposerID {
			continue
		}
		n.Send(endpoint, payload)
	}
}

// BroadcastNotaryAck broadcast notaryAck into the network.
func (n *TCPNetwork) BroadcastNotaryAck(notaryAck *types.NotaryAck) {
	payload := n.marshalMessage(notaryAck)
	for endpoint := range n.endpoints {
		if endpoint == notaryAck.ProposerID {
			continue
		}
		n.Send(endpoint, payload)
	}
}

// BroadcastVote broadcast vote into the network.
func (n *TCPNetwork) BroadcastVote(vote *types.Vote) {
	payload := n.marshalMessage(vote)
	for endpoint := range n.endpoints {
		if endpoint == vote.ProposerID {
			continue
		}
		n.Send(endpoint, payload)
	}
}

// DeliverBlocks sends blocks to peerServer.
func (n *TCPNetwork) DeliverBlocks(blocks BlockList) {
	messageJSON, err := json.Marshal(blocks)
	if err != nil {
		fmt.Printf("error: failed to marshal json: %v\n%+v\n", err, blocks)
		return
	}

	msgURL := fmt.Sprintf("http://%s:%d/delivery", n.peerServer, peerPort)

	go func() {
		for i := 0; i < retries; i++ {
			req, err := http.NewRequest(
				http.MethodPost, msgURL, strings.NewReader(string(messageJSON)))
			if err != nil {
				continue
			}
			req.Header.Add("ID", n.endpoint.GetID().String())

			resp, err := n.client.Do(req)
			if err == nil {
				defer resp.Body.Close()
				io.Copy(ioutil.Discard, resp.Body)
			}

			if err == nil && resp.StatusCode == http.StatusOK {
				runtime.Goexit()
			}
			time.Sleep(1 * time.Second)
		}
		fmt.Printf("failed to send message: %v\n", blocks)
	}()
}

// NotifyServer sends message to peerServer
func (n *TCPNetwork) NotifyServer(msg Message) {
	messageJSON, err := json.Marshal(msg)
	if err != nil {
		fmt.Printf("error: failed to marshal json: %v\n%+v\n", err, msg)
		return
	}

	msgURL := fmt.Sprintf("http://%s:%d/message", n.peerServer, peerPort)

	for i := 0; i < retries; i++ {
		req, err := http.NewRequest(
			http.MethodPost, msgURL, strings.NewReader(string(messageJSON)))
		if err != nil {
			continue
		}
		req.Header.Add("ID", n.endpoint.GetID().String())

		resp, err := n.client.Do(req)
		if err == nil {
			defer resp.Body.Close()
			io.Copy(ioutil.Discard, resp.Body)
		}
		if err == nil && resp.StatusCode == http.StatusOK {
			return
		}
		time.Sleep(1 * time.Second)
	}
	fmt.Printf("failed to send message: %v\n", msg)

	return
}

// GetServerInfo retrieve the info message from peerServer.
func (n *TCPNetwork) GetServerInfo() InfoMessage {
	infoMsg := InfoMessage{}
	msgURL := fmt.Sprintf("http://%s:%d/info", n.peerServer, peerPort)

	req, err := http.NewRequest(
		http.MethodGet, msgURL, nil)
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}

	resp, err := n.client.Do(req)
	if err != nil {
		fmt.Printf("error: %v\n", err)
		return infoMsg
	}
	if resp.StatusCode != http.StatusOK {
		fmt.Printf("error: %v\n", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)

	if err := json.Unmarshal(body, &infoMsg); err != nil {
		fmt.Printf("error: %v", err)
	}
	return infoMsg
}

// Endpoints returns all validatorIDs.
func (n *TCPNetwork) Endpoints() types.ValidatorIDs {
	vIDs := make(types.ValidatorIDs, 0, len(n.endpoints))
	for vID := range n.endpoints {
		vIDs = append(vIDs, vID)
	}
	return vIDs
}
