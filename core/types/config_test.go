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
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type ConfigTestSuite struct {
	suite.Suite
}

func (s *ConfigTestSuite) TestClone() {
	c := &Config{
		NumChains:        2,
		LambdaBA:         1 * time.Millisecond,
		LambdaDKG:        2 * time.Hour,
		K:                4,
		NotarySetSize:    5,
		DKGSetSize:       6,
		RoundInterval:    3 * time.Second,
		MinBlockInterval: 7 * time.Nanosecond,
		MaxBlockInterval: 9 * time.Minute,
	}
	s.Require().Equal(c, c.Clone())
}

func TestConfig(t *testing.T) {
	suite.Run(t, new(ConfigTestSuite))
}
