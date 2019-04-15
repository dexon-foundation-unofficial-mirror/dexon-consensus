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
	"errors"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/db"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon/rlp"
)

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

// CloneDKGComplaint clones a tpyesDKG.Complaint instance.
func CloneDKGComplaint(
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

// CloneDKGMasterPublicKey clones a typesDKG.MasterPublicKey instance.
func CloneDKGMasterPublicKey(mpk *typesDKG.MasterPublicKey) (
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

// CloneDKGMPKReady clones a typesDKG.MPKReady instance.
func CloneDKGMPKReady(ready *typesDKG.MPKReady) (
	copied *typesDKG.MPKReady) {
	b, err := rlp.EncodeToBytes(ready)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.MPKReady{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

// CloneDKGFinalize clones a typesDKG.Finalize instance.
func CloneDKGFinalize(final *typesDKG.Finalize) (
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

// CloneDKGSuccess clones a typesDKG.Success instance.
func CloneDKGSuccess(success *typesDKG.Success) (
	copied *typesDKG.Success) {
	b, err := rlp.EncodeToBytes(success)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.Success{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

// CloneDKGPrivateShare clones a typesDKG.PrivateShare instance.
func CloneDKGPrivateShare(prvShare *typesDKG.PrivateShare) (
	copied *typesDKG.PrivateShare) {
	b, err := rlp.EncodeToBytes(prvShare)
	if err != nil {
		panic(err)
	}
	copied = &typesDKG.PrivateShare{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

func cloneAgreementResult(result *types.AgreementResult) (
	copied *types.AgreementResult) {
	b, err := rlp.EncodeToBytes(result)
	if err != nil {
		panic(err)
	}
	copied = &types.AgreementResult{}
	if err = rlp.DecodeBytes(b, copied); err != nil {
		panic(err)
	}
	return
}

var (
	// ErrCompactionChainTipBlockNotExists raised when the hash of compaction
	// chain tip doesn't match a block in database.
	ErrCompactionChainTipBlockNotExists = errors.New(
		"compaction chain tip block not exists")
	// ErrEmptyCompactionChainTipInfo raised when a compaction chain tip info
	// is empty.
	ErrEmptyCompactionChainTipInfo = errors.New(
		"empty compaction chain tip info")
	// ErrMismatchBlockHash raise when the hash for that block mismatched.
	ErrMismatchBlockHash = errors.New("mismatched block hash")
)

// VerifyDB check if a database is valid after test.
func VerifyDB(db db.Database) error {
	hash, height := db.GetCompactionChainTipInfo()
	if (hash == common.Hash{}) || height == 0 {
		return ErrEmptyCompactionChainTipInfo
	}
	b, err := db.GetBlock(hash)
	if err != nil {
		return err
	}
	if b.Hash != hash {
		return ErrMismatchBlockHash
	}
	return nil
}

func getComplementSet(
	all, set map[types.NodeID]struct{}) map[types.NodeID]struct{} {
	complement := make(map[types.NodeID]struct{})
	for nID := range all {
		if _, exists := set[nID]; exists {
			continue
		}
		complement[nID] = struct{}{}
	}
	return complement
}
