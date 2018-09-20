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
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type RevealerTestSuite struct {
	suite.Suite

	db              blockdb.BlockDatabase
	totalBlockCount int
}

func (s *RevealerTestSuite) SetupSuite() {
	var (
		err        error
		nodeCount  = 19
		blockCount = 50
	)
	// Setup block database.
	s.db, err = blockdb.NewMemBackedBlockDB()
	s.Require().Nil(err)

	// Randomly generate blocks.
	gen := NewBlocksGenerator(nil, stableRandomHash)
	nodes, err := gen.Generate(
		nodeCount, blockCount, nil, s.db)
	s.Require().Nil(err)
	s.Require().Len(nodes, nodeCount)

	// Cache the count of total generated block.
	iter, err := s.db.GetAll()
	s.Require().Nil(err)
	blocks, err := loadAllBlocks(iter)
	s.Require().Nil(err)
	s.totalBlockCount = len(blocks)
}

func (s *RevealerTestSuite) baseTest(
	revealer Revealer,
	repeat int,
	checkFunc func(*types.Block, map[common.Hash]struct{})) {

	revealingSequence := map[string]struct{}{}
	for i := 0; i < repeat; i++ {
		revealed := map[common.Hash]struct{}{}
		sequence := ""
		for {
			b, err := revealer.Next()
			if err != nil {
				if err == blockdb.ErrIterationFinished {
					err = nil
					break
				}
				s.Require().NotNil(err)
			}
			checkFunc(&b, revealed)
			revealed[b.Hash] = struct{}{}
			sequence += b.Hash.String() + ","
		}
		s.Len(revealed, s.totalBlockCount)
		revealingSequence[sequence] = struct{}{}
		revealer.Reset()
	}
	// It should be reasonable to reveal at least two
	// different sequence.
	s.True(len(revealingSequence) > 1)

}

func (s *RevealerTestSuite) TestRandomReveal() {
	// This test case would make sure we could at least generate
	// two different revealing sequence when revealing more than
	// 10 times.
	iter, err := s.db.GetAll()
	s.Require().Nil(err)
	revealer, err := NewRandomRevealer(iter)
	s.Require().Nil(err)

	checkFunc := func(b *types.Block, revealed map[common.Hash]struct{}) {
		// Make sure the revealer won't reveal the same block twice.
		_, alreadyRevealed := revealed[b.Hash]
		s.False(alreadyRevealed)
	}
	s.baseTest(revealer, 10, checkFunc)
}

func (s *RevealerTestSuite) TestRandomDAGReveal() {
	// This test case would make sure we could at least generate
	// two different revealing sequence when revealing more than
	// 10 times, and each of them would form valid DAGs during
	// revealing.

	iter, err := s.db.GetAll()
	s.Require().Nil(err)
	revealer, err := NewRandomDAGRevealer(iter)
	s.Require().Nil(err)

	checkFunc := func(b *types.Block, revealed map[common.Hash]struct{}) {
		// Make sure this revealer won't reveal
		// the same block twice.
		_, alreadyRevealed := revealed[b.Hash]
		s.False(alreadyRevealed)
		// Make sure the newly revealed block would still
		// form a valid DAG after added to revealed blocks.
		s.True(isAllAckingBlockRevealed(b, revealed))
	}
	s.baseTest(revealer, 10, checkFunc)
}

func TestRevealer(t *testing.T) {
	suite.Run(t, new(RevealerTestSuite))
}
