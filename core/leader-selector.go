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

package core

import (
	"fmt"
	"math/big"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

// Errors for leader module.
var (
	ErrIncorrectCRSSignature = fmt.Errorf("incorrect CRS signature")
)

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
	hashCRS      common.Hash
	numCRS       *big.Int
	minCRSBlock  *big.Int
	minBlockHash common.Hash
}

func newLeaderSelector(
	crs common.Hash) *leaderSelector {
	numCRS := big.NewInt(0)
	numCRS.SetBytes(crs[:])
	return &leaderSelector{
		numCRS:      numCRS,
		hashCRS:     crs,
		minCRSBlock: maxHash,
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

func (l *leaderSelector) restart() {
	l.minCRSBlock = maxHash
	l.minBlockHash = common.Hash{}
}

func (l *leaderSelector) leaderBlockHash() common.Hash {
	return l.minBlockHash
}

func (l *leaderSelector) processBlock(block *types.Block) error {
	ok, err := verifyCRSSignature(block, l.hashCRS)
	if err != nil {
		return err
	}
	if !ok {
		return ErrIncorrectCRSSignature
	}
	dist := l.distance(block.CRSSignature)
	cmp := l.minCRSBlock.Cmp(dist)
	if cmp > 0 || (cmp == 0 && block.Hash.Less(l.minBlockHash)) {
		l.minCRSBlock = dist
		l.minBlockHash = block.Hash
	}
	return nil
}
