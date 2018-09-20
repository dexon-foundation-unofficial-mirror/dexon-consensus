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

package test

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

func stableRandomHash(block *types.Block) (common.Hash, error) {
	if (block.Hash != common.Hash{}) {
		return block.Hash, nil
	}
	return common.NewRandomHash(), nil
}

// GenerateRandomNodeIDs generates randomly a slices of types.NodeID.
func GenerateRandomNodeIDs(nodeCount int) (nIDs types.NodeIDs) {
	nIDs = types.NodeIDs{}
	for i := 0; i < nodeCount; i++ {
		nIDs = append(nIDs, types.NodeID{Hash: common.NewRandomHash()})
	}
	return
}

// CalcLatencyStatistics calculates average and deviation from a slice
// of latencies.
func CalcLatencyStatistics(latencies []time.Duration) (avg, dev time.Duration) {
	var (
		sum             float64
		sumOfSquareDiff float64
	)

	// Calculate average.
	for _, v := range latencies {
		sum += float64(v)
	}
	avgAsFloat := sum / float64(len(latencies))
	avg = time.Duration(avgAsFloat)
	// Calculate deviation
	for _, v := range latencies {
		diff := math.Abs(float64(v) - avgAsFloat)
		sumOfSquareDiff += diff * diff
	}
	dev = time.Duration(math.Sqrt(sumOfSquareDiff / float64(len(latencies)-1)))
	return
}

// FindMyIP returns local IP address.
func FindMyIP() (ip string, err error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ipnet.IP.IsLoopback() {
			continue
		}
		if ipnet.IP.To4() != nil {
			ip = ipnet.IP.String()
			return
		}
	}
	err = fmt.Errorf("unable to find IP")
	return
}
