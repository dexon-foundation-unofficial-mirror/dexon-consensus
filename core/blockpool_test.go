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
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type BlockPoolTestSuite struct {
	suite.Suite
}

func (s *BlockPoolTestSuite) TestBasicUsage() {
	// This test case try this flow:
	//  - add some blocks into pool.
	//  - get tips, check if expected.
	//  - get tips, should be identical to previous call.
	//  - remove tips, and get tips again, check if expected.
	//  - purge one chain, check if expected.
	var (
		req  = s.Require()
		pool = newBlockPool(3)
	)
	addBlockWithPosition := func(chainID uint32, height uint64) {
		pool.addBlock(&types.Block{
			Position: types.Position{
				ChainID: chainID,
				Height:  height,
			}})
	}
	chkPos := func(b *types.Block, chainID uint32, height uint64) {
		req.Equal(b.Position.ChainID, chainID)
		req.Equal(b.Position.Height, height)
	}
	addBlockWithPosition(0, 0)
	addBlockWithPosition(0, 1)
	addBlockWithPosition(0, 2)
	addBlockWithPosition(0, 3)
	addBlockWithPosition(2, 0)
	addBlockWithPosition(2, 1)
	addBlockWithPosition(2, 2)
	// Check each tip.
	chkPos(pool.tip(0), 0, 0)
	chkPos(pool.tip(2), 2, 0)
	req.Nil(pool.tip(1))
	// Remove tips of chain#0, #1.
	pool.removeTip(0)
	pool.removeTip(1)
	// Get tips of chain#0, #2 back to check.
	chkPos(pool.tip(0), 0, 1)
	chkPos(pool.tip(2), 2, 0) // Chain#2 is untouched.
	// Purge with height lower than lowest height.
	pool.purgeBlocks(0, 0)
	chkPos(pool.tip(0), 0, 1) // Chain#0 is not affected.
	// Purge with height in range.
	pool.purgeBlocks(0, 2)
	chkPos(pool.tip(0), 0, 3) // Height = 1, 2 are purged.
	// Purge with height higher than highest height.
	pool.purgeBlocks(0, 4)
	req.Nil(pool.tip(0)) // Whole chain is purged.
}

func TestBlockPool(t *testing.T) {
	suite.Run(t, new(BlockPoolTestSuite))
}
