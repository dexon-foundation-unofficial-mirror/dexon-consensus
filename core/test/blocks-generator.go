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
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// ErrParentNotAcked would be raised when some block doesn't
// ack its parent block.
var ErrParentNotAcked = errors.New("parent is not acked")

// nodeStatus is a state holder for each node
// during generating blocks.
type nodeStatus struct {
	blocks          []*types.Block
	genesisTime     time.Time
	prvKey          crypto.PrivateKey
	tip             *types.Block
	nextAckingIndex map[types.NodeID]uint64
}

type hashBlockFn func(*types.Block) (common.Hash, error)

// getAckedBlockHash would randomly pick one block between
// last acked one to current head.
func (ns *nodeStatus) getAckedBlockHash(
	ackedNID types.NodeID,
	ackedNode *nodeStatus,
	randGen *rand.Rand) (
	hash common.Hash, ok bool) {
	baseAckingIndex := ns.nextAckingIndex[ackedNID]
	totalBlockCount := uint64(len(ackedNode.blocks))
	if totalBlockCount <= baseAckingIndex {
		// There is no new block to ack.
		return
	}
	ackableRange := totalBlockCount - baseAckingIndex
	idx := uint64((randGen.Uint64() % ackableRange) + baseAckingIndex)
	ns.nextAckingIndex[ackedNID] = idx + 1
	hash = ackedNode.blocks[idx].Hash
	ok = true
	return
}

func (ns *nodeStatus) getNextBlockTime(
	timePicker func(time.Time) time.Time) time.Time {
	if ns.tip == nil {
		return timePicker(ns.genesisTime)
	}
	return timePicker(ns.tip.Timestamp)
}

// nodeSetStatus is a state holder for all nodes
// during generating blocks.
type nodeSetStatus struct {
	round         uint64
	status        map[types.NodeID]*nodeStatus
	proposerChain map[types.NodeID]uint32
	endTime       time.Time
	nIDs          []types.NodeID
	randGen       *rand.Rand
	timePicker    func(time.Time) time.Time
	hashBlock     hashBlockFn
}

func newNodeSetStatus(
	numChains uint32,
	tips map[uint32]*types.Block,
	round uint64,
	genesisTime, endTime time.Time,
	timePicker func(time.Time) time.Time,
	hashBlock hashBlockFn) *nodeSetStatus {
	var (
		status        = make(map[types.NodeID]*nodeStatus)
		proposerChain = make(map[types.NodeID]uint32)
		nIDs          = []types.NodeID{}
	)
	for i := uint32(0); i < numChains; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		if err != nil {
			panic(err)
		}
		nID := types.NewNodeID(prvKey.PublicKey())
		nIDs = append(nIDs, nID)
		status[nID] = &nodeStatus{
			blocks:          []*types.Block{},
			genesisTime:     genesisTime,
			prvKey:          prvKey,
			tip:             tips[i],
			nextAckingIndex: make(map[types.NodeID]uint64),
		}
		proposerChain[nID] = i
	}
	return &nodeSetStatus{
		round:         round,
		status:        status,
		proposerChain: proposerChain,
		endTime:       endTime,
		nIDs:          nIDs,
		randGen:       rand.New(rand.NewSource(time.Now().UnixNano())),
		timePicker:    timePicker,
		hashBlock:     hashBlock,
	}
}

// findIncompleteNodes is a helper to check which node doesn't generate
// enough blocks.
func (ns *nodeSetStatus) findIncompleteNodes() (nIDs []types.NodeID) {
	for nID, status := range ns.status {
		if status.tip == nil {
			nIDs = append(nIDs, nID)
			continue
		}
		if status.tip.Timestamp.After(ns.endTime) {
			continue
		}
		nIDs = append(nIDs, nID)
	}
	return
}

// prepareAcksForNewBlock collects acks for one block.
func (ns *nodeSetStatus) prepareAcksForNewBlock(
	proposerID types.NodeID, ackingCount int) (
	acks common.Hashes, err error) {
	acks = common.Hashes{}
	if len(ns.status[proposerID].blocks) == 0 {
		// The 'Acks' filed of genesis blocks would always be empty.
		return
	}
	// Pick nodeIDs to be acked.
	ackingNIDs := map[types.NodeID]struct{}{}
	if ackingCount > 0 {
		ackingCount-- // We would always include ack to parent block.
	}
	for _, i := range ns.randGen.Perm(len(ns.nIDs))[:ackingCount] {
		ackingNIDs[ns.nIDs[i]] = struct{}{}
	}
	// Generate acks.
	for nID := range ackingNIDs {
		if nID == proposerID {
			continue
		}
		ack, ok := ns.status[proposerID].getAckedBlockHash(
			nID, ns.status[nID], ns.randGen)
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
func (ns *nodeSetStatus) proposeBlock(
	proposerID types.NodeID, acks common.Hashes) (*types.Block, error) {
	status := ns.status[proposerID]
	parentHash := common.Hash{}
	blockHeight := uint64(0)
	if status.tip != nil {
		parentHash = status.tip.Hash
		blockHeight = status.tip.Position.Height + 1
		acks = append(acks, parentHash)
	}
	chainID := ns.proposerChain[proposerID]
	newBlock := &types.Block{
		ProposerID: proposerID,
		ParentHash: parentHash,
		Position: types.Position{
			Round:   ns.round,
			Height:  blockHeight,
			ChainID: chainID,
		},
		Acks:      common.NewSortedHashes(acks),
		Timestamp: status.getNextBlockTime(ns.timePicker),
	}
	var err error
	newBlock.Hash, err = ns.hashBlock(newBlock)
	if err != nil {
		return nil, err
	}
	newBlock.Signature, err = status.prvKey.Sign(newBlock.Hash)
	if err != nil {
		return nil, err
	}
	status.blocks = append(status.blocks, newBlock)
	status.tip = newBlock
	return newBlock, nil
}

// normalAckingCountGenerator would randomly pick acking count
// by a normal distribution.
func normalAckingCountGenerator(
	chainNum uint32, mean, deviation float64) func() int {
	return func() int {
		var expected float64
		for {
			expected = rand.NormFloat64()*deviation + mean
			if expected >= 0 && expected <= float64(chainNum) {
				break
			}
		}
		return int(math.Ceil(expected))
	}
}

// MaxAckingCountGenerator return generator which returns
// fixed maximum acking count.
func MaxAckingCountGenerator(count uint32) func() int {
	return func() int { return int(count) }
}

// generateNodePicker is a function generator, which would generate
// a function to randomly pick one node ID from a slice of node ID.
func generateNodePicker() func([]types.NodeID) types.NodeID {
	privateRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	return func(nIDs []types.NodeID) types.NodeID {
		return nIDs[privateRand.Intn(len(nIDs))]
	}
}

// defaultTimePicker would pick a time based on reference time and
// the given minimum/maximum time range.
func generateTimePicker(min, max time.Duration) (f func(time.Time) time.Time) {
	privateRand := rand.New(rand.NewSource(time.Now().UnixNano()))
	return func(ref time.Time) time.Time {
		return ref.Add(min + time.Duration(privateRand.Int63n(int64(max-min))))
	}
}

// BlocksGeneratorConfig is the configuration for BlocksGenerator.
type BlocksGeneratorConfig struct {
	NumChains            uint32
	MinBlockTimeInterval time.Duration
	MaxBlockTimeInterval time.Duration
}

// NewBlocksGeneratorConfig construct a BlocksGeneratorConfig instance.
func NewBlocksGeneratorConfig(c *types.Config) *BlocksGeneratorConfig {
	return &BlocksGeneratorConfig{
		NumChains:            c.NumChains,
		MinBlockTimeInterval: c.MinBlockInterval,
		MaxBlockTimeInterval: c.MaxBlockInterval,
	}
}

// BlocksGenerator could generate blocks forming valid DAGs.
type BlocksGenerator struct {
	config               *BlocksGeneratorConfig
	nodePicker           func([]types.NodeID) types.NodeID
	timePicker           func(time.Time) time.Time
	ackingCountGenerator func() int
	hashBlock            hashBlockFn
}

// NewBlocksGenerator constructs BlockGenerator.
//
// The caller is responsible to provide a function to generate count of
// acked block for each new block. The prototype of ackingCountGenerator is
// a function returning 'int'. For example, if you need to generate a group of
// blocks and each of them has maximum 2 acks.
//   func () int { return 2 }
// The default ackingCountGenerator would randomly pick a number based on
// the nodeCount you provided with a normal distribution.
func NewBlocksGenerator(
	config *BlocksGeneratorConfig,
	ackingCountGenerator func() int,
	hashBlock hashBlockFn) *BlocksGenerator {
	if ackingCountGenerator == nil {
		ackingCountGenerator = normalAckingCountGenerator(
			config.NumChains,
			float64(config.NumChains/2),
			float64(config.NumChains/4+1))
	}
	timePicker := generateTimePicker(
		config.MinBlockTimeInterval, config.MaxBlockTimeInterval)
	return &BlocksGenerator{
		config:               config,
		nodePicker:           generateNodePicker(),
		timePicker:           timePicker,
		ackingCountGenerator: ackingCountGenerator,
		hashBlock:            hashBlock,
	}
}

// Generate is the entry point to generate blocks in one round.
func (gen *BlocksGenerator) Generate(
	roundID uint64,
	roundBegin, roundEnd time.Time,
	db blockdb.BlockDatabase) (err error) {
	// Find tips of previous round if available.
	tips := make(map[uint32]*types.Block)
	if roundID > 0 {
		tips, err = gen.findTips(roundID-1, db)
		if err != nil {
			return
		}
	}
	status := newNodeSetStatus(gen.config.NumChains, tips, roundID,
		roundBegin, roundEnd, gen.timePicker, gen.hashBlock)
	// We would record the smallest height of block that could be acked
	// from each node's point-of-view.
	toAck := make(map[types.NodeID]map[types.NodeID]uint64)
	for _, nID := range status.nIDs {
		toAck[nID] = make(map[types.NodeID]uint64)
	}
	for {
		// Find nodes that doesn't propose enough blocks and
		// pick one from them randomly.
		notYet := status.findIncompleteNodes()
		if len(notYet) == 0 {
			break
		}
		// Propose a new block.
		var (
			proposerID = gen.nodePicker(notYet)
			acks       common.Hashes
		)
		if acks, err = status.prepareAcksForNewBlock(
			proposerID, gen.ackingCountGenerator()); err != nil {
			return
		}
		var newBlock *types.Block
		if newBlock, err = status.proposeBlock(proposerID, acks); err != nil {
			return
		}
		// Persist block to db.
		if err = db.Put(*newBlock); err != nil {
			return
		}
	}
	return
}

// findTips is an utility to find tips of each chain in that round in blockdb.
func (gen *BlocksGenerator) findTips(
	round uint64, db blockdb.Reader) (tips map[uint32]*types.Block, err error) {
	iter, err := db.GetAll()
	if err != nil {
		return
	}
	revealer, err := NewRandomRevealer(iter)
	if err != nil {
		return
	}
	tips = make(map[uint32]*types.Block)
	for {
		var b types.Block
		if b, err = revealer.Next(); err != nil {
			if err == blockdb.ErrIterationFinished {
				err = nil
				break
			}
			return
		}
		if b.Position.Round != round {
			continue
		}
		tip, exists := tips[b.Position.ChainID]
		if exists && tip.Position.Height > b.Position.Height {
			continue
		}
		tips[b.Position.ChainID] = &b
	}
	return
}
