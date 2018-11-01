package integration

import (
	"errors"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/core"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/test"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

func genRoundEndTimes(
	configs []*types.Config, dMoment time.Time) (ends []time.Time) {
	now := dMoment
	for _, config := range configs {
		now = now.Add(config.RoundInterval)
		ends = append(ends, now)
	}
	return
}

// loadAllConfigs loads all prepared configuration from governance,
// starts from round 0.
func loadAllConfigs(gov core.Governance) (configs []*types.Config) {
	var round uint64
	for {
		config := gov.Configuration(round)
		if config == nil {
			break
		}
		configs = append(configs, config)
		round++
	}
	return
}

// decideOwnChains compute which chainIDs belongs to this node.
func decideOwnChains(numChains uint32, numNodes, id int) (own []uint32) {
	var cur = uint32(id)
	if numNodes == 0 {
		panic(errors.New("attempt to arrange chains on 0 nodes"))
	}
	for {
		if cur >= numChains {
			break
		}
		own = append(own, cur)
		cur += uint32(numNodes)
	}
	return
}

// PrepareNodes setups nodes for testing.
func PrepareNodes(
	gov *test.Governance,
	prvKeys []crypto.PrivateKey,
	maxNumChains uint32,
	networkLatency, proposingLatency test.LatencyModel) (
	nodes map[types.NodeID]*Node, err error) {
	if maxNumChains == 0 {
		err = errors.New("zero NumChains is unexpected")
		return
	}
	// Setup nodes, count of nodes is derived from the count of private keys
	// hold in Governance.
	nodes = make(map[types.NodeID]*Node)
	dMoment := time.Now().UTC()
	broadcastTargets := make(map[types.NodeID]struct{})
	for idx, prvKey := range prvKeys {
		nID := types.NewNodeID(prvKey.PublicKey())
		broadcastTargets[nID] = struct{}{}
		// Decides which chains are owned by this node.
		if nodes[nID], err = newNode(
			gov,
			prvKey,
			dMoment,
			decideOwnChains(maxNumChains, len(prvKeys), idx),
			networkLatency,
			proposingLatency); err != nil {
			return
		}
	}
	// Assign broadcast targets.
	for _, n := range nodes {
		n.setBroadcastTargets(broadcastTargets)
		n.gov().State().SwitchToRemoteMode()
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

// CollectAppAndDBFromNodes collects test.App and blockdb.BlockDatabase
// from nodes.
func CollectAppAndDBFromNodes(nodes map[types.NodeID]*Node) (
	apps map[types.NodeID]*test.App,
	dbs map[types.NodeID]blockdb.BlockDatabase) {
	apps = make(map[types.NodeID]*test.App)
	dbs = make(map[types.NodeID]blockdb.BlockDatabase)
	for nID, node := range nodes {
		apps[nID] = node.app()
		dbs[nID] = node.db()
	}
	return
}
