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

package simulation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

type NetworkModelsTestSuite struct {
	suite.Suite
}

func (n *NetworkModelsTestSuite) SetupTest() {
}

func (n *NetworkModelsTestSuite) TearDownTest() {
}

// TestNormalNetwork make sure the Delay() or NormalNetwork does not
// exceeds 200ms.
func (n *NetworkModelsTestSuite) TestNormalNetwork() {
	m := NormalNetwork{}
	for i := 0; i < 1000; i++ {
		n.Require().True(m.Delay() < 200*time.Millisecond)
	}
}

func TestNetworkModels(t *testing.T) {
	suite.Run(t, new(NetworkModelsTestSuite))
}
