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
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// TODO(mission): blocks generator should generate blocks based on chain,
//                not nodes.

// ErrParentNotAcked would be raised when some block doesn't
// ack its parent block.
var ErrParentNotAcked = errors.New("parent is not acked")

// nodeStatus is a state holder for each node
// during generating blocks.
type nodeStatus struct {
	blocks           []*types.Block
	lastAckingHeight map[types.NodeID]uint64
}

type hashBlockFn func(*types.Block) (common.Hash, error)

// getAckedBlockHash would randomly pick one block between
// last acked one to current head.
func (vs *nodeStatus) getAckedBlockHash(
	ackedNID types.NodeID,
	ackedNode *nodeStatus,
	randGen *rand.Rand) (
	hash common.Hash, ok bool) {

	baseAckingHeight, exists := vs.lastAckingHeight[ackedNID]
	if exists {
		// Do not ack the same block(height) twice.
		baseAckingHeight++
	}
	totalBlockCount := uint64(len(ackedNode.blocks))
	if totalBlockCount <= baseAckingHeight {
		// There is no new block to ack.
		return
	}
	ackableRange := totalBlockCount - baseAckingHeight
	height := uint64((randGen.Uint64() % ackableRange) + baseAckingHeight)
	vs.lastAckingHeight[ackedNID] = height
	hash = ackedNode.blocks[height].Hash
	ok = true
	return
}

// nodeSetStatus is a state holder for all nodes
// during generating blocks.
type nodeSetStatus struct {
	status        map[types.NodeID]*nodeStatus
	proposerChain map[types.NodeID]uint32
	timestamps    []time.Time
	nodeIDs       []types.NodeID
	randGen       *rand.Rand
	hashBlock     hashBlockFn
}

func newNodeSetStatus(nIDs []types.NodeID, hashBlock hashBlockFn) *nodeSetStatus {
	status := make(map[types.NodeID]*nodeStatus)
	timestamps := make([]time.Time, 0, len(nIDs))
	proposerChain := make(map[types.NodeID]uint32)
	for i, nID := range nIDs {
		status[nID] = &nodeStatus{
			blocks:           []*types.Block{},
			lastAckingHeight: make(map[types.NodeID]uint64),
		}
		timestamps = append(timestamps, time.Now().UTC())
		proposerChain[nID] = uint32(i)
	}
	return &nodeSetStatus{
		status:        status,
		proposerChain: proposerChain,
		timestamps:    timestamps,
		nodeIDs:       nIDs,
		randGen:       rand.New(rand.NewSource(time.Now().UnixNano())),
		hashBlock:     hashBlock,
	}
}

// findIncompleteNodes is a helper to check which node
// doesn't generate enough blocks.
func (vs *nodeSetStatus) findIncompleteNodes(
	blockCount int) (nIDs []types.NodeID) {

	for nID, status := range vs.status {
		if len(status.blocks) < blockCount {
			nIDs = append(nIDs, nID)
		}
	}
	return
}

// prepareAcksForNewBlock collects acks for one block.
func (vs *nodeSetStatus) prepareAcksForNewBlock(
	proposerID types.NodeID, ackingCount int) (
	acks common.Hashes, err error) {

	acks = common.Hashes{}
	if len(vs.status[proposerID].blocks) == 0 {
		// The 'Acks' filed of genesis blocks would always be empty.
		return
	}
	// Pick nodeIDs to be acked.
	ackingNIDs := map[types.NodeID]struct{}{
		proposerID: struct{}{}, // Acking parent block is always required.
	}
	if ackingCount > 0 {
		ackingCount-- // We would always include ack to parent block.
	}
	for _, i := range vs.randGen.Perm(len(vs.nodeIDs))[:ackingCount] {
		ackingNIDs[vs.nodeIDs[i]] = struct{}{}
	}
	// Generate acks.
	for nID := range ackingNIDs {
		ack, ok := vs.status[proposerID].getAckedBlockHash(
			nID, vs.status[nID], vs.randGen)
		if !ok {
			if nID == proposerID {
				err = ErrParentNotAcked
			}
			continue
		}
		acks = append(acks, ack)
	}
	return
}

// proposeBlock propose new block and update node status.
func (vs *nodeSetStatus) proposeBlock(
	proposerID types.NodeID,
	acks common.Hashes) (*types.Block, error) {

	status := vs.status[proposerID]
	parentHash := common.Hash{}
	if len(status.blocks) > 0 {
		parentHash = status.blocks[len(status.blocks)-1].Hash
	}
	chainID := vs.proposerChain[proposerID]
	vs.timestamps[chainID] = vs.timestamps[chainID].Add(time.Second)

	newBlock := &types.Block{
		ProposerID: proposerID,
		ParentHash: parentHash,
		Position: types.Position{
			Height:  uint64(len(status.blocks)),
			ChainID: chainID,
		},
		Acks:      common.NewSortedHashes(acks),
		Timestamp: vs.timestamps[chainID],
	}
	for i, nID := range vs.nodeIDs {
		if nID == proposerID {
			newBlock.Position.ChainID = uint32(i)
		}
	}
	var err error
	newBlock.Hash, err = vs.hashBlock(newBlock)
	if err != nil {
		return nil, err
	}
	status.blocks = append(status.blocks, newBlock)
	return newBlock, nil
}

// normalAckingCountGenerator would randomly pick acking count
// by a normal distribution.
func normalAckingCountGenerator(
	nodeCount int, mean, deviation float64) func() int {

	return func() int {
		var expected float64
		for {
			expected = rand.NormFloat64()*deviation + mean
			if expected >= 0 && expected <= float64(nodeCount) {
				break
			}
		}
		return int(math.Ceil(expected))
	}
}

// MaxAckingCountGenerator return generator which returns
// fixed maximum acking count.
func MaxAckingCountGenerator(count int) func() int {
	return func() int { return count }
}

// generateNodePicker is a function generator, which would generate
// a function to randomly pick one node ID from a slice of node ID.
func generateNodePicker() func([]types.NodeID) types.NodeID {
	privateRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	return func(nIDs []types.NodeID) types.NodeID {
		return nIDs[privateRand.Intn(len(nIDs))]
	}
}

// BlocksGenerator could generate blocks forming valid DAGs.
type BlocksGenerator struct {
	nodePicker func([]types.NodeID) types.NodeID
	hashBlock  hashBlockFn
}

// NewBlocksGenerator constructs BlockGenerator.
func NewBlocksGenerator(nodePicker func(
	[]types.NodeID) types.NodeID,
	hashBlock hashBlockFn) *BlocksGenerator {

	if nodePicker == nil {
		nodePicker = generateNodePicker()
	}
	return &BlocksGenerator{
		nodePicker: nodePicker,
		hashBlock:  hashBlock,
	}
}

// Generate is the entry point to generate blocks. The caller is responsible
// to provide a function to generate count of acked block for each new block.
// The prototype of ackingCountGenerator is a function returning 'int'.
// For example, if you need to generate a group of blocks and each of them
// has maximum 2 acks.
//   func () int { return 2 }
// The default ackingCountGenerator would randomly pick a number based on
// the nodeCount you provided with a normal distribution.
func (gen *BlocksGenerator) Generate(
	nodeCount int,
	blockCount int,
	ackingCountGenerator func() int,
	writer blockdb.Writer) (
	nodes types.NodeIDs, err error) {

	if ackingCountGenerator == nil {
		ackingCountGenerator = normalAckingCountGenerator(
			nodeCount,
			float64(nodeCount/2),
			float64(nodeCount/4+1))
	}
	nodes = types.NodeIDs{}
	for i := 0; i < nodeCount; i++ {
		nodes = append(
			nodes, types.NodeID{Hash: common.NewRandomHash()})
	}
	status := newNodeSetStatus(nodes, gen.hashBlock)

	// We would record the smallest height of block that could be acked
	// from each node's point-of-view.
	toAck := make(map[types.NodeID]map[types.NodeID]uint64)
	for _, nID := range nodes {
		toAck[nID] = make(map[types.NodeID]uint64)
	}

	for {
		// Find nodes that doesn't propose enough blocks and
		// pick one from them randomly.
		notYet := status.findIncompleteNodes(blockCount)
		if len(notYet) == 0 {
			break
		}

		// Propose a new block.
		var (
			proposerID = gen.nodePicker(notYet)
			acks       common.Hashes
		)
		acks, err = status.prepareAcksForNewBlock(
			proposerID, ackingCountGenerator())
		if err != nil {
			return
		}
		var newBlock *types.Block
		newBlock, err = status.proposeBlock(proposerID, acks)
		if err != nil {
			return
		}

		// Persist block to db.
		err = writer.Put(*newBlock)
		if err != nil {
			return
		}
	}
	return
}
