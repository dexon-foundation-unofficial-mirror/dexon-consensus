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

package integration

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type NonByzantineTestSuite struct {
	suite.Suite
}

func (s *NonByzantineTestSuite) TestNonByzantine() {
	numNodes := 25
	if testing.Short() {
		numNodes = 7
	}
	var (
		networkLatency = &test.NormalLatencyModel{
			Sigma: 20,
			Mean:  250,
		}
		proposingLatency = &test.NormalLatencyModel{
			Sigma: 30,
			Mean:  500,
		}
		apps = make(map[types.NodeID]*test.App)
		dbs  = make(map[types.NodeID]blockdb.BlockDatabase)
		req  = s.Require()
	)

	apps, dbs, nodes, err := PrepareNodes(
		numNodes, networkLatency, proposingLatency)
	req.Nil(err)
	now := time.Now().UTC()
	sch := test.NewScheduler(test.NewStopByConfirmedBlocks(50, apps, dbs))
	for vID, v := range nodes {
		sch.RegisterEventHandler(vID, v)
		req.Nil(sch.Seed(NewProposeBlockEvent(vID, now)))
	}
	sch.Run(4)
	// Check results by comparing test.App instances.
	req.NoError(VerifyApps(apps))
}

func TestNonByzantine(t *testing.T) {
	suite.Run(t, new(NonByzantineTestSuite))
}
