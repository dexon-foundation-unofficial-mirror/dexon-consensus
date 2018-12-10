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

package core

import (
	"errors"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// NodeSetCache is type alias to avoid fullnode compile error when moving
// it to core/utils package.
type NodeSetCache = utils.NodeSetCache

// NewNodeSetCache is function alias to avoid fullnode compile error when moving
// it to core/utils package.
var NewNodeSetCache = utils.NewNodeSetCache

var (
	debug = false
	// ErrEmptyTimestamps would be reported if Block.timestamps is empty.
	ErrEmptyTimestamps = errors.New("timestamp vector should not be empty")
)

func init() {
	if os.Getenv("DEBUG") != "" {
		debug = true
	}
}

// Debugf is like fmt.Printf, but only output when we are in debug mode.
func Debugf(format string, args ...interface{}) {
	if debug {
		fmt.Printf(format, args...)
	}
}

// Debugln is like fmt.Println, but only output when we are in debug mode.
func Debugln(args ...interface{}) {
	if debug {
		fmt.Println(args...)
	}
}

func interpoTime(t1 time.Time, t2 time.Time, sep int) []time.Time {
	if sep == 0 {
		return []time.Time{}
	}
	if t1.After(t2) {
		return interpoTime(t2, t1, sep)
	}
	timestamps := make([]time.Time, sep)
	duration := t2.Sub(t1)
	period := time.Duration(
		(duration.Nanoseconds() / int64(sep+1))) * time.Nanosecond
	prevTime := t1
	for idx := range timestamps {
		prevTime = prevTime.Add(period)
		timestamps[idx] = prevTime
	}
	return timestamps
}

func getMedianTime(timestamps []time.Time) (t time.Time, err error) {
	if len(timestamps) == 0 {
		err = ErrEmptyTimestamps
		return
	}
	tscopy := make([]time.Time, 0, len(timestamps))
	for _, ts := range timestamps {
		tscopy = append(tscopy, ts)
	}
	sort.Sort(common.ByTime(tscopy))
	if len(tscopy)%2 == 0 {
		t1 := tscopy[len(tscopy)/2-1]
		t2 := tscopy[len(tscopy)/2]
		t = interpoTime(t1, t2, 1)[0]
	} else {
		t = tscopy[len(tscopy)/2]
	}
	return
}

func removeFromSortedUint32Slice(xs []uint32, x uint32) []uint32 {
	indexToRemove := sort.Search(len(xs), func(idx int) bool {
		return xs[idx] >= x
	})
	if indexToRemove == len(xs) || xs[indexToRemove] != x {
		// This value is not found.
		return xs
	}
	return append(xs[:indexToRemove], xs[indexToRemove+1:]...)
}

// pickBiasedTime returns a biased time based on a given range.
func pickBiasedTime(base time.Time, biasedRange time.Duration) time.Time {
	return base.Add(time.Duration(rand.Intn(int(biasedRange))))
}

// HashConfigurationBlock returns the hash value of configuration block.
func HashConfigurationBlock(
	notarySet map[types.NodeID]struct{},
	config *types.Config,
	snapshotHash common.Hash,
	prevHash common.Hash,
) common.Hash {
	notaryIDs := make(types.NodeIDs, 0, len(notarySet))
	for nID := range notarySet {
		notaryIDs = append(notaryIDs, nID)
	}
	sort.Sort(notaryIDs)
	notarySetBytes := make([]byte, 0, len(notarySet)*len(common.Hash{}))
	for _, nID := range notaryIDs {
		notarySetBytes = append(notarySetBytes, nID.Hash[:]...)
	}
	configBytes := config.Bytes()

	return crypto.Keccak256Hash(
		notarySetBytes[:],
		configBytes[:],
		snapshotHash[:],
		prevHash[:],
	)
}

// VerifyBlock verifies the signature of types.Block.
func VerifyBlock(b *types.Block) (err error) {
	hash, err := hashBlock(b)
	if err != nil {
		return
	}
	if hash != b.Hash {
		err = ErrIncorrectHash
		return
	}
	pubKey, err := crypto.SigToPub(b.Hash, b.Signature)
	if err != nil {
		return
	}
	if !b.ProposerID.Equal(types.NewNodeID(pubKey)) {
		err = ErrIncorrectSignature
		return
	}
	return
}

// VerifyAgreementResult perform sanity check against a types.AgreementResult
// instance.
func VerifyAgreementResult(
	res *types.AgreementResult, cache *utils.NodeSetCache) error {
	notarySet, err := cache.GetNotarySet(
		res.Position.Round, res.Position.ChainID)
	if err != nil {
		return err
	}
	if len(res.Votes) < len(notarySet)/3*2+1 {
		return ErrNotEnoughVotes
	}
	if len(res.Votes) > len(notarySet) {
		return ErrIncorrectVoteProposer
	}
	for _, vote := range res.Votes {
		if res.IsEmptyBlock {
			if (vote.BlockHash != common.Hash{}) {
				return ErrIncorrectVoteBlockHash
			}
		} else {
			if vote.BlockHash != res.BlockHash {
				return ErrIncorrectVoteBlockHash
			}
		}
		if vote.Type != types.VoteCom {
			return ErrIncorrectVoteType
		}
		if vote.Position != res.Position {
			return ErrIncorrectVotePosition
		}
		if _, exist := notarySet[vote.ProposerID]; !exist {
			return ErrIncorrectVoteProposer
		}
		ok, err := verifyVoteSignature(&vote)
		if err != nil {
			return err
		}
		if !ok {
			return ErrIncorrectVoteSignature
		}
	}
	return nil
}

// DiffUint64 calculates difference between two uint64.
func DiffUint64(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

func isCI() bool {
	return os.Getenv("CI") != ""
}

func isCircleCI() bool {
	return isCI() && os.Getenv("CIRCLECI") == "true"
}

func isTravisCI() bool {
	return isCI() && os.Getenv("TRAVIS") == "true"
}
