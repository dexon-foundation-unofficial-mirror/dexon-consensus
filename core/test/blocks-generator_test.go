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
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type BlocksGeneratorTestSuite struct {
	suite.Suite
}

func (s *BlocksGeneratorTestSuite) TestGenerate() {
	// This test case is to make sure the generated blocks are legimate.
	var (
		config = &BlocksGeneratorConfig{
			NumChains:            19,
			MinBlockTimeInterval: 50 * time.Millisecond,
			MaxBlockTimeInterval: 400 * time.Millisecond,
		}
		gen       = NewBlocksGenerator(config, nil, stableRandomHash)
		req       = s.Require()
		beginTime = time.Now().UTC()
		endTime   = beginTime.Add(time.Minute)
	)
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	req.NoError(gen.Generate(1, beginTime, endTime, db))
	// Load all blocks in that database for further checking.
	iter, err := db.GetAll()
	req.NoError(err)
	blocksByNode := make(map[types.NodeID][]*types.Block)
	blocksByHash := make(map[common.Hash]*types.Block)
	for {
		block, err := iter.Next()
		if err == blockdb.ErrIterationFinished {
			break
		}
		req.NoError(err)
		// TODO(mission): Make sure each block is correctly signed once
		//                we have a way to access core.hashBlock.
		req.NotEqual(block.Hash, common.Hash{})
		req.NotEmpty(block.Signature)
		req.Equal(block.Position.Round, uint64(1))
		blocksByNode[block.ProposerID] =
			append(blocksByNode[block.ProposerID], &block)
		sort.Sort(types.ByHeight(blocksByNode[block.ProposerID]))
		blocksByHash[block.Hash] = &block
	}
	// Make sure these two rules are hold for these blocks:
	//  - No backward acking: the later block should only ack new blocks
	//                        compared to its parent block.
	//  - Parent Ack: always ack its parent block.
	//  - Timestamp: timestamp are increasing, and with valid interval to
	//               previous block.
	//  - The last block of each chain should pass endTime.
	//  - No Acks in genesis bloc
	for _, blocks := range blocksByNode {
		lastAckingHeights := map[types.NodeID]uint64{}
		req.NotEmpty(blocks)
		// Check genesis block.
		genesisBlock := blocks[0]
		req.Equal(genesisBlock.ParentHash, common.Hash{})
		req.Equal(genesisBlock.Position.Height, uint64(0))
		req.Empty(genesisBlock.Acks)
		// Check normal blocks.
		for index, block := range blocks[1:] {
			parentAcked := false
			for _, ack := range block.Acks {
				if ack == block.ParentHash {
					parentAcked = true
				}
				ackedBlock := blocksByHash[ack]
				req.NotNil(ackedBlock)
				prevAckingHeight, exists :=
					lastAckingHeights[ackedBlock.ProposerID]
				if exists {
					s.True(prevAckingHeight < ackedBlock.Position.Height)
				}
				lastAckingHeights[ackedBlock.ProposerID] = ackedBlock.Position.Height
				// Block Height should always incremental by 1.
				//
				// Because we iterate blocks slice from 1,
				// we need to add 1 to the index.
				req.Equal(block.Position.Height, uint64(index+1))
			}
			req.True(parentAcked)
		}
		// The block time of the last block should be after end time.
		req.True(blocks[len(blocks)-1].Timestamp.After(endTime))
	}
}

func (s *BlocksGeneratorTestSuite) TestGenerateWithMaxAckCount() {
	var (
		config = &BlocksGeneratorConfig{
			NumChains:            13,
			MinBlockTimeInterval: 0,
			MaxBlockTimeInterval: 500 * time.Millisecond,
		}
		req              = s.Require()
		totalAckingCount = 0
		totalBlockCount  = 0
		genesisTime      = time.Now().UTC()
	)
	// Generate with 0 acks.
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	gen := NewBlocksGenerator(
		config, MaxAckingCountGenerator(0), stableRandomHash)
	req.NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(50*time.Second),
		db))
	// Load blocks to check their acking count.
	iter, err := db.GetAll()
	req.NoError(err)
	for {
		block, err := iter.Next()
		if err == blockdb.ErrIterationFinished {
			break
		}
		req.NoError(err)
		if block.IsGenesis() {
			continue
		}
		req.Len(block.Acks, 1)
	}
	// Generate with acks as many as possible.
	db, err = blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	gen = NewBlocksGenerator(
		config, MaxAckingCountGenerator(config.NumChains), stableRandomHash)
	req.NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(50*time.Second),
		db))
	// Load blocks to verify the average acking count.
	iter, err = db.GetAll()
	req.NoError(err)
	for {
		block, err := iter.Next()
		if err == blockdb.ErrIterationFinished {
			break
		}
		req.NoError(err)
		if block.IsGenesis() {
			continue
		}
		totalAckingCount += len(block.Acks)
		totalBlockCount++
	}
	req.NotZero(totalBlockCount)
	req.True((totalAckingCount / totalBlockCount) >= int(config.NumChains/2))
}

// TestFindTips make sure findTips works as expected.
func (s *BlocksGeneratorTestSuite) TestFindTips() {
	var (
		config = &BlocksGeneratorConfig{
			NumChains:            10,
			MinBlockTimeInterval: 0,
			MaxBlockTimeInterval: 500 * time.Millisecond,
		}
		req         = s.Require()
		genesisTime = time.Now().UTC()
		endTime     = genesisTime.Add(100 * time.Second)
	)
	gen := NewBlocksGenerator(config, nil, stableRandomHash)
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	req.NoError(gen.Generate(
		0,
		genesisTime,
		endTime,
		db))
	tips, err := gen.findTips(0, db)
	req.NoError(err)
	req.Len(tips, int(config.NumChains))
	for _, b := range tips {
		req.True(b.Timestamp.After(endTime))
	}
}

func (s *BlocksGeneratorTestSuite) TestConcateBlocksFromRounds() {
	// This test case run these steps:
	//  - generate blocks by round but sharing one blockdb.
	//  - if those rounds are continuous, they should be concated.
	var (
		req         = s.Require()
		genesisTime = time.Now().UTC()
	)
	db, err := blockdb.NewMemBackedBlockDB()
	req.NoError(err)
	// Generate round 0 blocks.
	gen := NewBlocksGenerator(&BlocksGeneratorConfig{
		NumChains:            4,
		MinBlockTimeInterval: 0,
		MaxBlockTimeInterval: 500 * time.Millisecond,
	}, nil, stableRandomHash)
	req.NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(10*time.Second),
		db))
	tips0, err := gen.findTips(0, db)
	req.NoError(err)
	req.Len(tips0, 4)
	// Generate round 1 blocks.
	gen = NewBlocksGenerator(&BlocksGeneratorConfig{
		NumChains:            10,
		MinBlockTimeInterval: 0,
		MaxBlockTimeInterval: 500 * time.Millisecond,
	}, nil, stableRandomHash)
	req.NoError(gen.Generate(
		1,
		genesisTime.Add(10*time.Second),
		genesisTime.Add(20*time.Second),
		db))
	tips1, err := gen.findTips(1, db)
	req.NoError(err)
	req.Len(tips1, 10)
	// Generate round 2 blocks.
	gen = NewBlocksGenerator(&BlocksGeneratorConfig{
		NumChains:            7,
		MinBlockTimeInterval: 0,
		MaxBlockTimeInterval: 500 * time.Millisecond,
	}, nil, stableRandomHash)
	req.NoError(gen.Generate(
		2,
		genesisTime.Add(20*time.Second),
		genesisTime.Add(30*time.Second),
		db))
	tips2, err := gen.findTips(2, db)
	req.NoError(err)
	req.Len(tips2, 7)
	// Check results, make sure tips0, tips1 are acked by correct blocks.
	iter, err := db.GetAll()
	req.NoError(err)
	revealer, err := NewRandomRevealer(iter)
	req.NoError(err)
	removeTip := func(tips map[uint32]*types.Block, b *types.Block) {
		toRemove := []uint32{}
		for chainID, tip := range tips {
			if b.ParentHash == tip.Hash {
				req.Equal(b.Position.Height, tip.Position.Height+1)
				req.Equal(b.Position.Round, tip.Position.Round+1)
				req.True(b.IsAcking(tip.Hash))
				toRemove = append(toRemove, chainID)
			}
		}
		for _, ID := range toRemove {
			delete(tips, ID)
		}
	}
	// Make sure all tips are acked by loading blocks from db
	// and check them one by one.
	for {
		b, err := revealer.Next()
		if err != nil {
			if err == blockdb.ErrIterationFinished {
				err = nil
				break
			}
			req.NoError(err)
		}
		switch b.Position.Round {
		case 1:
			removeTip(tips0, &b)
		case 2:
			removeTip(tips1, &b)
		}
	}
	req.Empty(tips0)
	req.Len(tips1, 3)
	req.Contains(tips1, uint32(7))
	req.Contains(tips1, uint32(8))
	req.Contains(tips1, uint32(9))
	// Check the acking frequency of last round, it might be wrong.
	totalBlockCount := 0
	totalAckCount := 0
	revealer.Reset()
	for {
		b, err := revealer.Next()
		if err != nil {
			if err == blockdb.ErrIterationFinished {
				err = nil
				break
			}
			req.NoError(err)
		}
		if b.Position.Round != 2 {
			continue
		}
		totalBlockCount++
		totalAckCount += len(b.Acks)
	}
	// At least all blocks can ack some non-parent block.
	req.True(totalAckCount/totalBlockCount >= 2)
}

func TestBlocksGenerator(t *testing.T) {
	suite.Run(t, new(BlocksGeneratorTestSuite))
}
