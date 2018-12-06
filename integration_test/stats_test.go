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

package integration

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/stretchr/testify/suite"
)

type EventStatsTestSuite struct {
	suite.Suite
}

func (s *EventStatsTestSuite) TestCalculate() {
	// Setup a test with fixed latency in proposing and network,
	// and make sure the calculated statistics is expected.
	var (
		networkLatency   = &test.FixedLatencyModel{Latency: 100}
		proposingLatency = &test.FixedLatencyModel{Latency: 300}
		req              = s.Require()
	)
	prvKeys, pubKeys, err := test.NewKeys(7)
	req.NoError(err)
	gov, err := test.NewGovernance(
		test.NewState(
			pubKeys, 100*time.Millisecond, &common.NullLogger{}, true),
		core.ConfigRoundShift)
	req.NoError(err)
	nodes, err := PrepareNodes(
		gov, prvKeys, 7, networkLatency, proposingLatency)
	req.NoError(err)
	apps, dbs := CollectAppAndDBFromNodes(nodes)
	sch := test.NewScheduler(test.NewStopByConfirmedBlocks(50, apps, dbs))
	now := time.Now().UTC()
	for _, n := range nodes {
		req.NoError(n.Bootstrap(sch, now))
	}
	sch.Run(10)
	req.Nil(VerifyApps(apps))
	// Check total statistics result.
	stats, err := NewStats(sch.CloneExecutionHistory(), apps)
	req.Nil(err)
	req.True(stats.All.ProposedBlockCount > 350)
	req.True(stats.All.ReceivedBlockCount > 350)
	req.True(stats.All.ConfirmedBlockCount > 350)
	req.True(stats.All.TotalOrderedBlockCount >= 350)
	req.True(stats.All.DeliveredBlockCount >= 350)
	req.Equal(stats.All.ProposingLatency, 300*time.Millisecond)
	req.Equal(stats.All.ReceivingLatency, 100*time.Millisecond)
	// Check statistics for each node.
	for _, vStats := range stats.ByNode {
		req.True(vStats.ProposedBlockCount > 50)
		req.True(vStats.ReceivedBlockCount > 50)
		req.True(vStats.ConfirmedBlockCount > 50)
		req.True(vStats.TotalOrderedBlockCount >= 50)
		req.True(vStats.DeliveredBlockCount >= 50)
		req.Equal(vStats.ProposingLatency, 300*time.Millisecond)
		req.Equal(vStats.ReceivingLatency, 100*time.Millisecond)
	}
}

func TestEventStats(t *testing.T) {
	suite.Run(t, new(EventStatsTestSuite))
}
