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
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type BlockRevealerTestSuite struct {
	suite.Suite

	db              db.Database
	totalBlockCount int
}

func (s *BlockRevealerTestSuite) SetupSuite() {
	var (
		err         error
		genesisTime = time.Now().UTC()
	)
	// Setup block database.
	s.db, err = db.NewMemBackedDB()
	s.Require().NoError(err)

	// Randomly generate blocks.
	config := &BlocksGeneratorConfig{
		NumChains:            19,
		MinBlockTimeInterval: 250 * time.Millisecond,
	}
	gen := NewBlocksGenerator(config, nil)
	s.Require().NoError(gen.Generate(
		0,
		genesisTime,
		genesisTime.Add(30*time.Second),
		s.db))
	// Cache the count of total generated block.
	iter, err := s.db.GetAllBlocks()
	s.Require().NoError(err)
	blocks, err := loadAllBlocks(iter)
	s.Require().NoError(err)
	s.totalBlockCount = len(blocks)
}

func (s *BlockRevealerTestSuite) baseTest(
	revealer BlockRevealer,
	repeat int,
	checkFunc func(*types.Block, map[common.Hash]struct{})) {

	revealingSequence := map[string]struct{}{}
	for i := 0; i < repeat; i++ {
		revealed := map[common.Hash]struct{}{}
		sequence := ""
		for {
			b, err := revealer.NextBlock()
			if err != nil {
				if err == db.ErrIterationFinished {
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

func (s *BlockRevealerTestSuite) TestRandomBlockReveal() {
	// This test case would make sure we could at least generate
	// two different revealing sequence when revealing more than
	// 10 times.
	iter, err := s.db.GetAllBlocks()
	s.Require().Nil(err)
	revealer, err := NewRandomBlockRevealer(iter)
	s.Require().Nil(err)

	checkFunc := func(b *types.Block, revealed map[common.Hash]struct{}) {
		// Make sure the revealer won't reveal the same block twice.
		_, alreadyRevealed := revealed[b.Hash]
		s.False(alreadyRevealed)
	}
	s.baseTest(revealer, 10, checkFunc)
}

func (s *BlockRevealerTestSuite) TestRandomDAGBlockReveal() {
	// This test case would make sure we could at least generate
	// two different revealing sequence when revealing more than
	// 10 times, and each of them would form valid DAGs during
	// revealing.

	iter, err := s.db.GetAllBlocks()
	s.Require().Nil(err)
	revealer, err := NewRandomDAGBlockRevealer(iter)
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

func (s *BlockRevealerTestSuite) TestRandomTipBlockReveal() {
	// This test case would make sure we could at least generate
	// two different revealing sequence when revealing more than
	// 10 times.
	iter, err := s.db.GetAllBlocks()
	s.Require().Nil(err)
	revealer, err := NewRandomTipBlockRevealer(iter)
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

func (s *BlockRevealerTestSuite) TestCompactionChainBlockReveal() {
	dbInst, err := db.NewMemBackedDB()
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
	s.Require().NoError(dbInst.PutBlock(*b1))
	s.Require().NoError(dbInst.PutBlock(*b3))
	iter, err := dbInst.GetAllBlocks()
	s.Require().NoError(err)
	// The compaction chain is not complete, we can't construct a revealer
	// instance successfully.
	r, err := NewCompactionChainBlockRevealer(iter, 0)
	s.Require().Nil(r)
	s.Require().IsType(ErrNotValidCompactionChain, err)
	// Put a block to make the compaction chain complete.
	s.Require().NoError(dbInst.PutBlock(*b2))
	// We can construct that revealer now.
	iter, err = dbInst.GetAllBlocks()
	s.Require().NoError(err)
	r, err = NewCompactionChainBlockRevealer(iter, 0)
	s.Require().NotNil(r)
	s.Require().NoError(err)
	// The revealing order should be ok.
	chk := func(h uint64) {
		b, err := r.NextBlock()
		s.Require().NoError(err)
		s.Require().Equal(b.Finalization.Height, h)
	}
	chk(1)
	chk(2)
	chk(3)
	// Iteration should be finished
	_, err = r.NextBlock()
	s.Require().IsType(db.ErrIterationFinished, err)
	// Test 'startHeight' parameter.
	iter, err = dbInst.GetAllBlocks()
	s.Require().NoError(err)
	r, err = NewCompactionChainBlockRevealer(iter, 2)
	s.Require().NotNil(r)
	s.Require().NoError(err)
	chk(2)
	chk(3)
}

func TestBlockRevealer(t *testing.T) {
	suite.Run(t, new(BlockRevealerTestSuite))
}
