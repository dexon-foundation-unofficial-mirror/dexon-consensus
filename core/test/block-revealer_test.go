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

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type BlockRevealerTestSuite struct {
	suite.Suite
}

func (s *BlockRevealerTestSuite) TestBlockRevealByPosition() {
	dbInst, err := db.NewMemBackedDB()
	s.Require().NoError(err)
	// Put several blocks with position field ready.
	b1 := &types.Block{
		Hash:     common.NewRandomHash(),
		Position: types.Position{Height: 1},
	}
	b2 := &types.Block{
		Hash:     common.NewRandomHash(),
		Position: types.Position{Height: 2},
	}
	b3 := &types.Block{
		Hash:     common.NewRandomHash(),
		Position: types.Position{Height: 3},
	}
	s.Require().NoError(dbInst.PutBlock(*b1))
	s.Require().NoError(dbInst.PutBlock(*b3))
	iter, err := dbInst.GetAllBlocks()
	s.Require().NoError(err)
	// The compaction chain is not complete, we can't construct a revealer
	// instance successfully.
	r, err := NewBlockRevealerByPosition(iter, 0)
	s.Require().Nil(r)
	s.Require().Equal(ErrNotValidCompactionChain.Error(), err.Error())
	// Put a block to make the compaction chain complete.
	s.Require().NoError(dbInst.PutBlock(*b2))
	// We can construct that revealer now.
	iter, err = dbInst.GetAllBlocks()
	s.Require().NoError(err)
	r, err = NewBlockRevealerByPosition(iter, 0)
	s.Require().NotNil(r)
	s.Require().NoError(err)
	// The revealing order should be ok.
	chk := func(h uint64) {
		b, err := r.NextBlock()
		s.Require().NoError(err)
		s.Require().Equal(b.Position.Height, h)
	}
	chk(1)
	chk(2)
	chk(3)
	// Iteration should be finished
	_, err = r.NextBlock()
	s.Require().Equal(db.ErrIterationFinished.Error(), err.Error())
	// Test 'startHeight' parameter.
	iter, err = dbInst.GetAllBlocks()
	s.Require().NoError(err)
	r, err = NewBlockRevealerByPosition(iter, 2)
	s.Require().NotNil(r)
	s.Require().NoError(err)
	chk(2)
	chk(3)
}

func TestBlockRevealer(t *testing.T) {
	suite.Run(t, new(BlockRevealerTestSuite))
}
