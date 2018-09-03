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

package types

import (
	"sort"
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/stretchr/testify/suite"
)

type BlockTestSuite struct {
	suite.Suite
}

func (s *BlockTestSuite) TestSortByHash() {
	hash := common.Hash{}
	copy(hash[:], "aaaaaa")
	b0 := &Block{Hash: hash}
	copy(hash[:], "bbbbbb")
	b1 := &Block{Hash: hash}
	copy(hash[:], "cccccc")
	b2 := &Block{Hash: hash}
	copy(hash[:], "dddddd")
	b3 := &Block{Hash: hash}

	blocks := []*Block{b3, b2, b1, b0}
	sort.Sort(ByHash(blocks))
	s.Equal(blocks[0].Hash, b0.Hash)
	s.Equal(blocks[1].Hash, b1.Hash)
	s.Equal(blocks[2].Hash, b2.Hash)
	s.Equal(blocks[3].Hash, b3.Hash)
}

func (s *BlockTestSuite) TestSortByHeight() {
	b0 := &Block{Position: Position{Height: 0}}
	b1 := &Block{Position: Position{Height: 1}}
	b2 := &Block{Position: Position{Height: 2}}
	b3 := &Block{Position: Position{Height: 3}}

	blocks := []*Block{b3, b2, b1, b0}
	sort.Sort(ByHeight(blocks))
	s.Equal(blocks[0].Hash, b0.Hash)
	s.Equal(blocks[1].Hash, b1.Hash)
	s.Equal(blocks[2].Hash, b2.Hash)
	s.Equal(blocks[3].Hash, b3.Hash)
}

func (s *BlockTestSuite) TestGenesisBlock() {
	b0 := &Block{
		Position: Position{
			Height: 0,
		},
		ParentHash: common.Hash{},
	}
	s.True(b0.IsGenesis())
	b1 := &Block{
		Position: Position{
			Height: 1,
		},
		ParentHash: common.Hash{},
	}
	s.False(b1.IsGenesis())
	b2 := &Block{
		Position: Position{
			Height: 0,
		},
		ParentHash: common.NewRandomHash(),
	}
	s.False(b2.IsGenesis())
}

func TestBlock(t *testing.T) {
	suite.Run(t, new(BlockTestSuite))
}
