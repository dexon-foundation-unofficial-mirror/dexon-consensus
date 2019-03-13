// Copyright 2019 The dexon-consensus Authors
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

package utils

import (
	"testing"

	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type RoundBasedConfigTestSuite struct {
	suite.Suite
}

func (s *RoundBasedConfigTestSuite) TestBasicUsage() {
	c1 := RoundBasedConfig{}
	c1.SetupRoundBasedFields(1, &types.Config{RoundLength: 100})
	c1.SetRoundBeginHeight(11)
	s.Require().Equal(c1.RoundID(), uint64(1))
	s.Require().Equal(c1.roundLength, uint64(100))
	s.Require().Equal(c1.roundBeginHeight, uint64(11))
	s.Require().Equal(c1.roundEndHeight, uint64(111))
	s.Require().True(c1.Contains(110))
	s.Require().False(c1.Contains(111))
	c1.ExtendLength()
	s.Require().True(c1.Contains(111))
	s.Require().True(c1.Contains(210))
	s.Require().False(c1.Contains(211))
	s.Require().Equal(c1.LastPeriodBeginHeight(), uint64(111))
	s.Require().Equal(c1.RoundEndHeight(), uint64(211))
	// Test AppendTo.
	c2 := RoundBasedConfig{}
	c2.SetupRoundBasedFields(2, &types.Config{RoundLength: 50})
	c2.AppendTo(c1)
	s.Require().Equal(c2.roundBeginHeight, uint64(211))
	s.Require().Equal(c2.roundEndHeight, uint64(261))
}

func TestRoundBasedConfig(t *testing.T) {
	suite.Run(t, new(RoundBasedConfigTestSuite))
}
