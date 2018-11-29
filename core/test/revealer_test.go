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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type RevealerTestSuite struct {
	suite.Suite

	db              blockdb.BlockDatabase
	totalBlockCount int
}

func (s *RevealerTestSuite) SetupSuite() {
	var (
		err         error
		genesisTime = time.Now().UTC()
	)
	// Setup block database.
	s.db, err = blockdb.NewMemBackedBlockDB()
	s.Require().NoError(err)

	// Randomly generate blocks.
	config := &BlocksGeneratorConfig{
		NumChains:            19,
		MinBlockTimeInterval: 250 * time.Millisecond,
	}
	gen := NewBlocksGenerator(config, nil, stableRandomHash)
	s.Require().NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(30*time.Second),
		s.db))
	// Cache the count of total generated block.
	iter, err := s.db.GetAll()
	s.Require().NoError(err)
	blocks, err := loadAllBlocks(iter)
	s.Require().NoError(err)
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

func (s *RevealerTestSuite) TestRandomTipReveal() {
	// This test case would make sure we could at least generate
	// two different revealing sequence when revealing more than
	// 10 times.
	iter, err := s.db.GetAll()
	s.Require().Nil(err)
	revealer, err := NewRandomTipRevealer(iter)
	s.Require().Nil(err)

	checkFunc := func(b *types.Block, revealed map[common.Hash]struct{}) {
		// Make sure the revealer won't reveal the same block twice.
		_, alreadyRevealed := revealed[b.Hash]
		s.False(alreadyRevealed)
		// Make sure the parent is already revealed.
		if b.Position.Height == 0 {
			return
		}
		_, alreadyRevealed = revealed[b.ParentHash]
		s.True(alreadyRevealed)
	}
	s.baseTest(revealer, 10, checkFunc)
}

func (s *RevealerTestSuite) TestCompactionChainReveal() {
	db, err := blockdb.NewMemBackedBlockDB()
	s.Require().NoError(err)
	// Put several blocks with finalization field ready.
	b1 := &types.Block{
		Hash: common.NewRandomHash(),
		Finalization: types.FinalizationResult{
			Height: 1,
		}}
	b2 := &types.Block{
		Hash: common.NewRandomHash(),
		Finalization: types.FinalizationResult{
			ParentHash: b1.Hash,
			Height:     2,
		}}
	b3 := &types.Block{
		Hash: common.NewRandomHash(),
		Finalization: types.FinalizationResult{
			ParentHash: b2.Hash,
			Height:     3,
		}}
	s.Require().NoError(db.Put(*b1))
	s.Require().NoError(db.Put(*b3))
	iter, err := db.GetAll()
	s.Require().NoError(err)
	// The compaction chain is not complete, we can't construct a revealer
	// instance successfully.
	r, err := NewCompactionChainRevealer(iter, 0)
	s.Require().Nil(r)
	s.Require().IsType(ErrNotValidCompactionChain, err)
	// Put a block to make the compaction chain complete.
	s.Require().NoError(db.Put(*b2))
	// We can construct that revealer now.
	iter, err = db.GetAll()
	s.Require().NoError(err)
	r, err = NewCompactionChainRevealer(iter, 0)
	s.Require().NotNil(r)
	s.Require().NoError(err)
	// The revealing order should be ok.
	chk := func(h uint64) {
		b, err := r.Next()
		s.Require().NoError(err)
		s.Require().Equal(b.Finalization.Height, h)
	}
	chk(1)
	chk(2)
	chk(3)
	// Iteration should be finished
	_, err = r.Next()
	s.Require().IsType(blockdb.ErrIterationFinished, err)
	// Test 'startHeight' parameter.
	iter, err = db.GetAll()
	s.Require().NoError(err)
	r, err = NewCompactionChainRevealer(iter, 2)
	s.Require().NotNil(r)
	s.Require().NoError(err)
	chk(2)
	chk(3)
}

func TestRevealer(t *testing.T) {
	suite.Run(t, new(RevealerTestSuite))
}
