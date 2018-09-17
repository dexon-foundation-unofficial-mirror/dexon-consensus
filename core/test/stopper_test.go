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
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/blockdb"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type StopperTestSuite struct {
	suite.Suite
}

func (s *StopperTestSuite) TestStopByConfirmedBlocks() {
	// This test case makes sure this stopper would stop when
	// all validators confirmed at least 'x' count of blocks produced
	// by themselves.
	var (
		req = s.Require()
	)

	apps := make(map[types.ValidatorID]*App)
	dbs := make(map[types.ValidatorID]blockdb.BlockDatabase)
	validators := GenerateRandomValidatorIDs(2)
	db, err := blockdb.NewMemBackedBlockDB()
	req.Nil(err)
	for _, vID := range validators {
		apps[vID] = NewApp()
		dbs[vID] = db
	}
	deliver := func(blocks []*types.Block) {
		hashes := common.Hashes{}
		for _, b := range blocks {
			hashes = append(hashes, b.Hash)
			req.Nil(db.Put(*b))
		}
		for _, vID := range validators {
			app := apps[vID]
			for _, h := range hashes {
				app.StronglyAcked(h)
			}
			app.TotalOrderingDeliver(hashes, false)
			for _, h := range hashes {
				app.DeliverBlock(h, time.Time{})
			}
		}
	}
	stopper := NewStopByConfirmedBlocks(2, apps, dbs)
	b00 := &types.Block{
		ProposerID: validators[0],
		Hash:       common.NewRandomHash(),
	}
	deliver([]*types.Block{b00})
	b10 := &types.Block{
		ProposerID: validators[1],
		Hash:       common.NewRandomHash(),
	}
	b11 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b10.Hash,
		Hash:       common.NewRandomHash(),
	}
	deliver([]*types.Block{b10, b11})
	req.False(stopper.ShouldStop(validators[1]))
	b12 := &types.Block{
		ProposerID: validators[1],
		ParentHash: b11.Hash,
		Hash:       common.NewRandomHash(),
	}
	deliver([]*types.Block{b12})
	req.False(stopper.ShouldStop(validators[1]))
	b01 := &types.Block{
		ProposerID: validators[0],
		ParentHash: b00.Hash,
		Hash:       common.NewRandomHash(),
	}
	deliver([]*types.Block{b01})
	req.True(stopper.ShouldStop(validators[0]))
}

func TestStopper(t *testing.T) {
	suite.Run(t, new(StopperTestSuite))
}
