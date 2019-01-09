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

package simulation

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sort"
	"sync"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/simulation/config"
)

// PeerServer is the main object to collect results and monitor simulation.
type PeerServer struct {
	peers             map[types.NodeID]struct{}
	msgChannel        chan *test.TransportEnvelope
	trans             test.TransportServer
	peerTotalOrder    PeerTotalOrder
	peerTotalOrderMu  sync.Mutex
	verifiedLen       uint64
	cfg               *config.Config
	ctx               context.Context
	ctxCancel         context.CancelFunc
	blockEvents       map[types.NodeID]map[common.Hash][]time.Time
	throughputRecords map[types.NodeID][]test.ThroughputRecord
}

// NewPeerServer returns a new PeerServer instance.
func NewPeerServer() *PeerServer {
	ctx, cancel := context.WithCancel(context.Background())
	return &PeerServer{
		peers:             make(map[types.NodeID]struct{}),
		peerTotalOrder:    make(PeerTotalOrder),
		ctx:               ctx,
		ctxCancel:         cancel,
		blockEvents:       make(map[types.NodeID]map[common.Hash][]time.Time),
		throughputRecords: make(map[types.NodeID][]test.ThroughputRecord),
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
			if err := p.trans.Broadcast(
				p.peers, &test.FixedLatencyModel{}, ntfShutdown); err != nil {
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

func (p *PeerServer) handleBlockEventMessage(
	id types.NodeID, msg *test.BlockEventMessage) {

	if _, exist := p.blockEvents[id]; !exist {
		p.blockEvents[id] = make(map[common.Hash][]time.Time)
	}
	nodeEvents := p.blockEvents[id]
	if _, exist := nodeEvents[msg.BlockHash]; !exist {
		nodeEvents[msg.BlockHash] = []time.Time{}
	}
	nodeEvents[msg.BlockHash] = msg.Timestamps
}

func (p *PeerServer) handleThroughputData(
	id types.NodeID, records *[]test.ThroughputRecord) {

	p.throughputRecords[id] = append(p.throughputRecords[id], *records...)
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
			case *test.BlockEventMessage:
				p.handleBlockEventMessage(e.From, val)
			case *[]test.ThroughputRecord:
				p.handleThroughputData(e.From, val)
			default:
				panic(fmt.Errorf("unknown message: %v", reflect.TypeOf(e.Msg)))
			}
		}
	}
}

// Setup prepares simualtion.
func (p *PeerServer) Setup(
	cfg *config.Config) (serverEndpoint interface{}, err error) {
	dMoment := time.Now().UTC()
	// Setup transport layer.
	switch cfg.Networking.Type {
	case "tcp", "tcp-local":
		p.trans = test.NewTCPTransportServer(&jsonMarshaller{}, peerPort)
		dMoment = dMoment.Add(10 * time.Second)
	case "fake":
		p.trans = test.NewFakeTransportServer()
	default:
		panic(fmt.Errorf("unknown network type: %v", cfg.Networking.Type))
	}
	p.trans.SetDMoment(dMoment)
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
		nID := types.NewNodeID(pubKey)
		p.peers[nID] = struct{}{}
	}
	// Pick a mater node to execute pending config changes.
	for nID := range p.peers {
		if err := p.trans.Send(nID, ntfSelectedAsMaster); err != nil {
			panic(err)
		}
		break
	}
	// Wait for peers to report 'setupOK' message.
	readyPeers := make(map[types.NodeID]struct{})
	for {
		e := <-p.msgChannel
		if !p.isNode(e.From) {
			break
		}
		msg := e.Msg.(*message)
		if msg.Type != setupOK {
			panic(fmt.Errorf("receive an unexpected peer report: %v", msg))
		}
		log.Println("receive setupOK message from", e.From)
		readyPeers[e.From] = struct{}{}
		if len(readyPeers) == len(p.peers) {
			break
		}
	}
	if err := p.trans.Broadcast(
		p.peers, &test.FixedLatencyModel{}, ntfReady); err != nil {
		panic(err)
	}
	log.Println("Simulation is ready to go with", len(p.peers), "nodes")
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
	p.logBlockEvents()
	p.logThroughputRecords()
}

func (p *PeerServer) logThroughputRecords() {
	// Interval is the sample rate of calculating throughput data, the unit is
	// nano second.
	intervals := []int64{int64(time.Second), int64(100 * time.Millisecond)}
	log.Println("======== throughput data ============")
	for nid, records := range p.throughputRecords {
		log.Printf("[Node %s]\n", nid)
		msgTypes := []string{}
		msgMap := make(map[string][]test.ThroughputRecord)
		for _, record := range records {
			msgMap[record.Type] = append(msgMap[record.Type], record)
		}
		for k := range msgMap {
			msgTypes = append(msgTypes, k)
		}
		sort.Strings(msgTypes)
		for _, interval := range intervals {
			log.Printf("    %dms", interval/int64(time.Millisecond))
			for _, msgType := range msgTypes {
				sum := 0
				startTime := msgMap[msgType][0].Time.UnixNano()
				endTime := startTime
				for _, record := range msgMap[msgType] {
					sum += record.Size
					t := record.Time.UnixNano()
					// The receiving order might be different with sending order.
					if t < startTime {
						startTime = t
					}
					if t > endTime {
						endTime = t
					}
				}
				startIndex := startTime / interval
				endIndex := endTime / interval
				log.Printf("        %s (count: %d, size: %d)",
					msgType, len(msgMap[msgType]), sum)
				// A slot stores total throughput in the interval of that time. The
				// index of slot of a specified time is calculated by deviding the
				// interval and minusing the starting time. For example, start time is
				// 5.5s, then time "7.123s"'s index of slot is
				// 7123000000 / 100000000 - 55 = 71 - 55 = 16.
				slots := make([]int, endIndex-startIndex+1)
				for _, record := range msgMap[msgType] {
					slots[record.Time.UnixNano()/interval-startIndex] += record.Size
				}
				mean, std := calculateMeanStdDeviationInts(slots)
				log.Printf("            mean: %f, std: %f", mean, std)
				min, med, max := getMinMedianMaxInts(slots)
				log.Printf("            min: %d, med: %d, max: %d", min, med, max)
			}
		}
	}
}

func (p *PeerServer) logBlockEvents() {
	// diffs stores the difference between two consecutive event time.
	diffs := [blockEventCount - 1][]float64{}
	for _, blocks := range p.blockEvents {
		for _, timestamps := range blocks {
			for i := 0; i < blockEventCount-1; i++ {
				diffs[i] = append(
					diffs[i],
					float64(timestamps[i+1].Sub(timestamps[i]))/1000000000,
				)
			}
		}
	}
	log.Printf("======== block events (%d blocks) ============", len(diffs[0]))
	for i, ary := range diffs {
		mean, stdDeviation := calculateMeanStdDeviationFloat64s(ary)
		min, med, max := getMinMedianMaxFloat64s(ary)
		log.Printf("    event %d to %d", i, i+1)
		log.Printf("        mean: %f, std dev = %f", mean, stdDeviation)
		log.Printf("        min: %f, median: %f, max: %f", min, med, max)
	}
}
