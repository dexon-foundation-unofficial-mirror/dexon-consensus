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

package types

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/suite"
)

type PositionTestSuite struct {
	suite.Suite
}

func (s *PositionTestSuite) TestNewer() {
	pos := Position{
		Round:  1,
		Height: 1,
	}
	s.False(pos.Newer(Position{
		Round:  2,
		Height: 0,
	}))
	s.False(pos.Newer(Position{
		Round:  1,
		Height: 2,
	}))
	s.True(pos.Newer(Position{
		Round:  0,
		Height: 100,
	}))
	s.True(pos.Newer(Position{
		Round:  1,
		Height: 0,
	}))
}

func (s *PositionTestSuite) TestOlder() {
	pos := Position{
		Round:  1,
		Height: 1,
	}
	s.False(pos.Older(Position{
		Round:  0,
		Height: 0,
	}))
	s.False(pos.Older(Position{
		Round:  1,
		Height: 0,
	}))
	s.True(pos.Older(Position{
		Round:  2,
		Height: 0,
	}))
	s.True(pos.Older(Position{
		Round:  1,
		Height: 100,
	}))
}

func (s *PositionTestSuite) TestSearchInAsendingOrder() {
	positions := []Position{
		Position{Round: 0, Height: 1},
		Position{Round: 0, Height: 2},
		Position{Round: 0, Height: 3},
		Position{Round: 2, Height: 0},
		Position{Round: 2, Height: 1},
		Position{Round: 2, Height: 2},
		Position{Round: 4, Height: 0},
		Position{Round: 4, Height: 1},
		Position{Round: 4, Height: 2},
	}
	search := func(pos Position) int {
		return sort.Search(len(positions), func(i int) bool {
			return positions[i].Newer(pos) || positions[i].Equal(pos)
		})
	}
	s.Equal(0, search(Position{Round: 0, Height: 0}))
	s.Equal(len(positions), search(Position{Round: 4, Height: 4}))
	s.Equal(0, search(Position{Round: 0, Height: 1}))
	s.Equal(len(positions)-1, search(Position{Round: 4, Height: 2}))
	s.Equal(2, search(Position{Round: 0, Height: 3}))
}

func (s *PositionTestSuite) TestEqual() {
	pos := Position{}
	s.True(pos.Equal(Position{}))
	s.False(pos.Equal(Position{Round: 1}))
	s.False(pos.Equal(Position{Height: 1}))
}

func TestPosition(t *testing.T) {
	suite.Run(t, new(PositionTestSuite))
}
