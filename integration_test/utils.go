package integration

import (
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// PrepareNodes setups nodes for testing.
func PrepareNodes(
	nodeCount int,
	networkLatency, proposingLatency test.LatencyModel) (
	apps map[types.NodeID]*test.App,
	dbs map[types.NodeID]blockdb.BlockDatabase,
	nodes map[types.NodeID]*Node,
	err error) {

	apps = make(map[types.NodeID]*test.App)
	dbs = make(map[types.NodeID]blockdb.BlockDatabase)
	nodes = make(map[types.NodeID]*Node)

	gov, err := test.NewGovernance(nodeCount, 700*time.Millisecond)
	if err != nil {
		return
	}
	dMoment := time.Now().UTC()
	for _, prvKey := range gov.PrivateKeys() {
		nID := types.NewNodeID(prvKey.PublicKey())
		apps[nID] = test.NewApp()
		dbs[nID], err = blockdb.NewMemBackedBlockDB()
		if err != nil {
			return
		}
		nodes[nID] = NewNode(
			apps[nID],
			gov,
			dbs[nID],
			prvKey,
			dMoment,
			networkLatency,
			proposingLatency,
		)
	}
	return
}

// VerifyApps is a helper to check delivery between test.Apps
func VerifyApps(apps map[types.NodeID]*test.App) (err error) {
	for vFrom, fromApp := range apps {
		if err = fromApp.Verify(); err != nil {
			return
		}
		for vTo, toApp := range apps {
			if vFrom == vTo {
				continue
			}
			if err = fromApp.Compare(toApp); err != nil {
				return
			}
		}
	}
	return
}
