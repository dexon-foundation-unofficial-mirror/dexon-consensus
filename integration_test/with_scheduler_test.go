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

	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/stretchr/testify/suite"
)

type WithSchedulerTestSuite struct {
	suite.Suite
}

func (s *WithSchedulerTestSuite) TestNonByzantine() {
	var (
		networkLatency = &test.NormalLatencyModel{
			Sigma: 20,
			Mean:  250,
		}
		proposingLatency = &test.NormalLatencyModel{
			Sigma: 30,
			Mean:  500,
		}
		numNodes = 25
		req      = s.Require()
	)
	if testing.Short() {
		numNodes = 7
	}
	// Setup key pairs.
	prvKeys, pubKeys, err := test.NewKeys(numNodes)
	req.NoError(err)
	// Setup governance.
	gov, err := test.NewGovernance(pubKeys, 250*time.Millisecond)
	req.NoError(err)
	// Setup nodes.
	nodes, err := PrepareNodes(
		gov, prvKeys, 25, networkLatency, proposingLatency)
	req.NoError(err)
	// Setup scheduler.
	apps, dbs := CollectAppAndDBFromNodes(nodes)
	now := time.Now().UTC()
	sch := test.NewScheduler(test.NewStopByConfirmedBlocks(50, apps, dbs))
	for _, n := range nodes {
		req.NoError(n.Bootstrap(sch, now))
	}
	sch.Run(4)
	// Check results by comparing test.App instances.
	req.NoError(VerifyApps(apps))
}

func (s *WithSchedulerTestSuite) TestConfigurationChange() {
	// This test case verify the correctness of core.Lattice when configuration
	// changes.
	// - Configuration changes are registered at 'pickedNode', and would carried
	//   in blocks' payload and broadcast to other nodes.
	var (
		networkLatency = &test.NormalLatencyModel{
			Sigma: 20,
			Mean:  250,
		}
		proposingLatency = &test.NormalLatencyModel{
			Sigma: 30,
			Mean:  500,
		}
		numNodes     = 4
		req          = s.Require()
		maxNumChains = uint32(9)
	)
	// Setup key pairs.
	prvKeys, pubKeys, err := test.NewKeys(numNodes)
	req.NoError(err)
	// Setup governance.
	gov, err := test.NewGovernance(pubKeys, 250*time.Millisecond)
	req.NoError(err)
	// Change default round interval, expect 1 round produce 30 blocks.
	gov.State().RequestChange(test.StateChangeRoundInterval, 15*time.Second)
	// Setup nodes.
	nodes, err := PrepareNodes(
		gov, prvKeys, maxNumChains, networkLatency, proposingLatency)
	req.NoError(err)
	// Set scheduler.
	apps, dbs := CollectAppAndDBFromNodes(nodes)
	now := time.Now().UTC()
	sch := test.NewScheduler(test.NewStopByRound(9, apps, dbs))
	for _, n := range nodes {
		req.NoError(n.Bootstrap(sch, now))
	}
	// Register some configuration changes at some node.
	var pickedNode *Node
	for _, pickedNode = range nodes {
		break
	}
	// Config changes for round 4, numChains from 4 to 7.
	req.NoError(pickedNode.gov().RegisterConfigChange(
		4, test.StateChangeNumChains, uint32(7)))
	req.NoError(pickedNode.gov().RegisterConfigChange(
		4, test.StateChangeK, 3))
	req.NoError(pickedNode.gov().RegisterConfigChange(
		4, test.StateChangePhiRatio, float32(0.5)))
	// Config changes for round 5, numChains from 7 to 9.
	req.NoError(pickedNode.gov().RegisterConfigChange(
		5, test.StateChangeNumChains, maxNumChains))
	req.NoError(pickedNode.gov().RegisterConfigChange(
		5, test.StateChangeK, 0))
	// Config changes for round 6, numChains from 9 to 7.
	req.NoError(pickedNode.gov().RegisterConfigChange(
		6, test.StateChangeNumChains, uint32(7)))
	req.NoError(pickedNode.gov().RegisterConfigChange(
		6, test.StateChangeK, 1))
	// Config changes for round 6, numChains from 7 to 5.
	req.NoError(pickedNode.gov().RegisterConfigChange(
		7, test.StateChangeNumChains, uint32(5)))
	req.NoError(pickedNode.gov().RegisterConfigChange(
		7, test.StateChangeK, 1))
	// Perform test.
	sch.Run(4)
	// Check results by comparing test.App instances.
	req.NoError(VerifyApps(apps))
}

func TestWithScheduler(t *testing.T) {
	suite.Run(t, new(WithSchedulerTestSuite))
}
