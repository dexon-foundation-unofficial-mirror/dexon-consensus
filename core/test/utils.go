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

package test

import (
	"fmt"
	"math"
	"net"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon/rlp"
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

// GenerateRandomPrivateKeys generate a set of private keys.
func GenerateRandomPrivateKeys(nodeCount int) (prvKeys []crypto.PrivateKey) {
	for i := 0; i < nodeCount; i++ {
		prvKey, err := ecdsa.NewPrivateKey()
		if err != nil {
			panic(err)
		}
		prvKeys = append(prvKeys, prvKey)
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

// NewKeys creates private keys and corresponding public keys as slice.
func NewKeys(count int) (
	prvKeys []crypto.PrivateKey, pubKeys []crypto.PublicKey, err error) {
	for i := 0; i < count; i++ {
		var prvKey crypto.PrivateKey
		if prvKey, err = ecdsa.NewPrivateKey(); err != nil {
			return
		}
		prvKeys = append(prvKeys, prvKey)
		pubKeys = append(pubKeys, prvKey.PublicKey())
	}
	return
}

func cloneDKGComplaint(
	comp *typesDKG.Complaint) (copied *typesDKG.Complaint) {
	b, err := rlp.EncodeToBytes(comp)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.Complaint{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

func cloneDKGMasterPublicKey(mpk *typesDKG.MasterPublicKey) (
	copied *typesDKG.MasterPublicKey) {
	b, err := rlp.EncodeToBytes(mpk)
	if err != nil {
		panic(err)
	}
	copied = typesDKG.NewMasterPublicKey()
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

func cloneDKGFinalize(final *typesDKG.Finalize) (
	copied *typesDKG.Finalize) {
	b, err := rlp.EncodeToBytes(final)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.Finalize{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}
