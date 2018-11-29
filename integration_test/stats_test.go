package integration

import (
	"testing"
	"time"

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
		pubKeys, 100*time.Millisecond, core.ConfigRoundShift)
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
