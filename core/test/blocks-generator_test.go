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

	"github.com/dexon-foundation/dexon-consensus-core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type BlocksGeneratorTestCase struct {
	suite.Suite
}

func (s *BlocksGeneratorTestCase) TestGenerate() {
	// This test case is to make sure the generated blocks are legimate.
	validatorCount := 19
	blockCount := 50
	gen := NewBlocksGenerator(nil, stableRandomHash)
	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)

	err = gen.Generate(
		validatorCount, blockCount, nil, db)
	s.Require().Nil(err)

	// Load all blocks in that database for further checking.
	iter, err := db.GetAll()
	s.Require().Nil(err)
	blocksByValidator := make(map[types.ValidatorID][]*types.Block)
	blocksByHash := make(map[common.Hash]*types.Block)
	for {
		block, err := iter.Next()
		if err == blockdb.ErrIterationFinished {
			break
		}
		s.Nil(err)

		blocksByValidator[block.ProposerID] =
			append(blocksByValidator[block.ProposerID], &block)
		sort.Sort(types.ByHeight(blocksByValidator[block.ProposerID]))
		blocksByHash[block.Hash] = &block
	}

	// Make sure these two rules are hold for these blocks:
	//  - No backward acking: the later block should only ack new blocks
	//                        compared to its parent block.
	//  - Parent Ack: always ack its parent block.
	//  - No Acks in genesis bloc
	for _, blocks := range blocksByValidator {
		lastAckingHeights := map[types.ValidatorID]uint64{}
		s.Require().NotEmpty(blocks)

		// Check genesis block.
		genesisBlock := blocks[0]
		s.Equal(genesisBlock.ParentHash, common.Hash{})
		s.Equal(genesisBlock.Height, uint64(0))
		s.Empty(genesisBlock.Acks)

		// Check normal blocks.
		for index, block := range blocks[1:] {
			parentAcked := false
			for ack := range block.Acks {
				if ack == block.ParentHash {
					parentAcked = true
				}

				ackedBlock := blocksByHash[ack]
				s.Require().NotNil(ackedBlock)
				prevAckingHeight, exists :=
					lastAckingHeights[ackedBlock.ProposerID]
				if exists {
					s.True(prevAckingHeight < ackedBlock.Height)
				}
				lastAckingHeights[ackedBlock.ProposerID] = ackedBlock.Height
				// Block Height should always incremental by 1.
				//
				// Because we iterate blocks slice from 1,
				// we need to add 1 to the index.
				s.Equal(block.Height, uint64(index+1))
			}
			s.True(parentAcked)
		}
	}
}

func (s *BlocksGeneratorTestCase) TestGenerateWithMaxAckCount() {
	var (
		validatorCount = 13
		blockCount     = 50
		gen            = NewBlocksGenerator(nil, stableRandomHash)
		req            = s.Require()
	)

	// Generate with 0 acks.
	db, err := blockdb.NewMemBackedBlockDB()
	req.Nil(err)
	req.Nil(gen.Generate(
		validatorCount, blockCount, MaxAckingCountGenerator(0), db))
	// Load blocks to check their acking count.
	iter, err := db.GetAll()
	req.Nil(err)
	for {
		block, err := iter.Next()
		if err == blockdb.ErrIterationFinished {
			break
		}
		req.Nil(err)
		if block.IsGenesis() {
			continue
		}
		req.Len(block.Acks, 1)
	}

	// Generate with acks as many as possible.
	db, err = blockdb.NewMemBackedBlockDB()
	req.Nil(err)
	req.Nil(gen.Generate(
		validatorCount, blockCount, MaxAckingCountGenerator(
			validatorCount), db))
	// Load blocks to verify the average acking count.
	totalAckingCount := 0
	totalBlockCount := 0
	iter, err = db.GetAll()
	req.Nil(err)
	for {
		block, err := iter.Next()
		if err == blockdb.ErrIterationFinished {
			break
		}
		req.Nil(err)
		if block.IsGenesis() {
			continue
		}
		totalAckingCount += len(block.Acks)
		totalBlockCount++
	}
	req.NotZero(totalBlockCount)
	req.True((totalAckingCount / totalBlockCount) >= (validatorCount / 2))
}

func TestBlocksGenerator(t *testing.T) {
	suite.Run(t, new(BlocksGeneratorTestCase))
}
