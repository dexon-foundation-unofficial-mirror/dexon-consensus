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
	"fmt"
	"math/big"
	"sync"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

// Errors for leader module.
var (
	ErrIncorrectCRSSignature = fmt.Errorf("incorrect CRS signature")
)

type validLeaderFn func(*types.Block) (bool, error)

// Some constant value.
var (
	maxHash *big.Int
	one     *big.Rat
)

func init() {
	hash := make([]byte, common.HashLength)
	for i := range hash {
		hash[i] = 0xff
	}
	maxHash = big.NewInt(0).SetBytes(hash)
	one = big.NewRat(1, 1)
}

type leaderSelector struct {
	hashCRS       common.Hash
	numCRS        *big.Int
	minCRSBlock   *big.Int
	minBlockHash  common.Hash
	pendingBlocks []*types.Block
	validLeader   validLeaderFn
	lock          sync.Mutex
	logger        common.Logger
}

func newLeaderSelector(
	validLeader validLeaderFn, logger common.Logger) *leaderSelector {
	return &leaderSelector{
		minCRSBlock: maxHash,
		validLeader: validLeader,
		logger:      logger,
	}
}

func (l *leaderSelector) distance(sig crypto.Signature) *big.Int {
	hash := crypto.Keccak256Hash(sig.Signature[:])
	num := big.NewInt(0)
	num.SetBytes(hash[:])
	num.Abs(num.Sub(l.numCRS, num))
	return num
}

func (l *leaderSelector) probability(sig crypto.Signature) float64 {
	dis := l.distance(sig)
	prob := big.NewRat(1, 1).SetFrac(dis, maxHash)
	p, _ := prob.Sub(one, prob).Float64()
	return p
}

func (l *leaderSelector) restart(crs common.Hash) {
	numCRS := big.NewInt(0)
	numCRS.SetBytes(crs[:])
	l.lock.Lock()
	defer l.lock.Unlock()
	l.numCRS = numCRS
	l.hashCRS = crs
	l.minCRSBlock = maxHash
	l.minBlockHash = common.Hash{}
	l.pendingBlocks = []*types.Block{}
}

func (l *leaderSelector) leaderBlockHash() common.Hash {
	l.lock.Lock()
	defer l.lock.Unlock()
	newPendingBlocks := []*types.Block{}
	for _, b := range l.pendingBlocks {
		ok, dist := l.potentialLeader(b)
		if !ok {
			continue
		}
		ok, err := l.validLeader(b)
		if err != nil {
			l.logger.Error("Error checking validLeader", "error", err, "block", b)
			continue
		}
		if ok {
			l.updateLeader(b, dist)
		} else {
			newPendingBlocks = append(newPendingBlocks, b)
		}
	}
	l.pendingBlocks = newPendingBlocks
	return l.minBlockHash
}

func (l *leaderSelector) processBlock(block *types.Block) error {
	ok, err := utils.VerifyCRSSignature(block, l.hashCRS)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectCRSSignature
	}
	l.lock.Lock()
	defer l.lock.Unlock()
	ok, dist := l.potentialLeader(block)
	if !ok {
		return nil
	}
	ok, err = l.validLeader(block)
	if err != nil {
		return err
	}
	if !ok {
		l.pendingBlocks = append(l.pendingBlocks, block)
		return nil
	}
	l.updateLeader(block, dist)
	return nil
}

func (l *leaderSelector) potentialLeader(block *types.Block) (bool, *big.Int) {
	dist := l.distance(block.CRSSignature)
	cmp := l.minCRSBlock.Cmp(dist)
	return (cmp > 0 || (cmp == 0 && block.Hash.Less(l.minBlockHash))), dist
}

func (l *leaderSelector) updateLeader(block *types.Block, dist *big.Int) {
	l.minCRSBlock = dist
	l.minBlockHash = block.Hash
}
