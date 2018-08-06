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
	"math/rand"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

const retries = 5

// TCPNetwork implements the Network interface.
type TCPNetwork struct {
	local    bool
	port     int
	endpoint Endpoint

	peerServer    string
	endpointMutex sync.RWMutex
	endpoints     map[types.ValidatorID]string
	recieveChan   chan interface{}
}

// NewTCPNetwork returns pointer to a new Network instance.
func NewTCPNetwork(local bool, peerServer string) *TCPNetwork {
	port := 1024 + rand.Int()%1024
	if !local {
		port = peerPort
	}
	pServer := peerServer
	if local {
		pServer = "127.0.0.1"
	}
	return &TCPNetwork{
		local:       local,
		peerServer:  pServer,
		port:        port,
		endpoints:   make(map[types.ValidatorID]string),
		recieveChan: make(chan interface{}, msgBufferSize),
	}
}

// Start starts the http server for accepting message.
func (n *TCPNetwork) Start() {
	addr := fmt.Sprintf("0.0.0.0:%d", n.port)
	server := &http.Server{
		Addr:    addr,
		Handler: n,
	}
	fmt.Printf("Validator started at %s\n", addr)
	server.ListenAndServe()
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
	default:
		w.WriteHeader(http.StatusBadRequest)
		return
	}
}

// Join allow a client to join the network. It reutnrs a interface{} channel for
// the client to recieve information.
func (n *TCPNetwork) Join(endpoint Endpoint) chan interface{} {
	n.endpointMutex.Lock()
	defer n.endpointMutex.Unlock()

	n.endpoint = endpoint

	joinURL := fmt.Sprintf("http://%s:%d/join", n.peerServer, peerPort)
	peersURL := fmt.Sprintf("http://%s:%d/peers", n.peerServer, peerPort)

	client := &http.Client{Timeout: 5 * time.Second}

	// Join the peer list.
	for {
		time.Sleep(time.Second)

		req, err := http.NewRequest(http.MethodGet, joinURL, nil)
		if err != nil {
			continue
		}
		req.Header.Add("ID", endpoint.GetID().String())
		req.Header.Add("PORT", fmt.Sprintf("%d", n.port))

		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
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
		resp, err := client.Do(req)
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
	return n.recieveChan
}

// Send sends a msg to another client.
func (n *TCPNetwork) Send(destID types.ValidatorID, msg interface{}) {
	clientAddr, exists := n.endpoints[destID]
	if !exists {
		return
	}

	message := struct {
		Type    string      `json:"type"`
		Payload interface{} `json:"payload"`
	}{}

	switch v := msg.(type) {
	case *types.Block:
		message.Type = "block"
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

	msgURL := fmt.Sprintf("http://%s/msg", clientAddr)

	go func() {
		client := &http.Client{Timeout: 5 * time.Second}

		for i := 0; i < retries; i++ {
			req, err := http.NewRequest(
				http.MethodPost, msgURL, strings.NewReader(string(messageJSON)))
			if err != nil {
				continue
			}
			req.Close = true
			req.Header.Add("ID", n.endpoint.GetID().String())

			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
			}
			if err == nil && resp.StatusCode == http.StatusOK {
				runtime.Goexit()
			}

			fmt.Printf("failed to submit message: %s\n", err)
			time.Sleep(1 * time.Second)
		}
		fmt.Printf("failed to send message: %v\n", msg)
	}()
}

// BroadcastBlock broadcast blocks into the network.
func (n *TCPNetwork) BroadcastBlock(block *types.Block) {
	for endpoint := range n.endpoints {
		n.Send(endpoint, block.Clone())
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
		client := &http.Client{Timeout: 5 * time.Second}

		for i := 0; i < retries; i++ {
			req, err := http.NewRequest(
				http.MethodPost, msgURL, strings.NewReader(string(messageJSON)))
			if err != nil {
				continue
			}
			req.Header.Add("ID", n.endpoint.GetID().String())

			resp, err := client.Do(req)
			if err == nil {
				defer resp.Body.Close()
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

	client := &http.Client{Timeout: 5 * time.Second}

	for i := 0; i < retries; i++ {
		req, err := http.NewRequest(
			http.MethodPost, msgURL, strings.NewReader(string(messageJSON)))
		if err != nil {
			continue
		}
		req.Header.Add("ID", n.endpoint.GetID().String())

		resp, err := client.Do(req)
		if err == nil {
			defer resp.Body.Close()
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
	client := &http.Client{Timeout: 5 * time.Second}

	req, err := http.NewRequest(
		http.MethodGet, msgURL, nil)
	if err != nil {
		fmt.Printf("error: %v\n", err)
	}

	resp, err := client.Do(req)
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
