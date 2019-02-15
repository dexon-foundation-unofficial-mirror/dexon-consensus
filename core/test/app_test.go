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
	"bytes"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type AppTestSuite struct {
	suite.Suite
}

func (s *AppTestSuite) deliverBlockWithTimeFromSequenceLength(
	app *App, hash common.Hash) {

	s.deliverBlock(app, hash, time.Time{}.Add(
		time.Duration(len(app.DeliverSequence))*time.Second),
		uint64(len(app.DeliverSequence)+1))
}

func (s *AppTestSuite) deliverBlock(
	app *App, hash common.Hash, timestamp time.Time, height uint64) {

	app.BlockDelivered(hash, types.Position{}, types.FinalizationResult{
		Timestamp: timestamp,
		Height:    height,
	})
}

func (s *AppTestSuite) TestCompare() {
	var (
		now = time.Now().UTC()
		b0  = types.Block{Hash: common.Hash{}}
		b1  = types.Block{
			Hash:     common.NewRandomHash(),
			Position: types.Position{Height: 1},
		}
	)
	// Prepare an OK App instance.
	app1 := NewApp(0, nil)
	app1.BlockConfirmed(b0)
	app1.BlockConfirmed(b1)
	app1.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now,
	})
	app1.BlockDelivered(b1.Hash, b1.Position, types.FinalizationResult{
		Height:    2,
		Timestamp: now.Add(1 * time.Second),
	})
	app2 := NewApp(0, nil)
	s.Require().Equal(ErrEmptyDeliverSequence.Error(),
		app1.Compare(app2).Error())
	app2.BlockConfirmed(b0)
	app2.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now,
	})
	b1Bad := types.Block{
		Hash:     common.NewRandomHash(),
		Position: types.Position{Height: 1},
	}
	app2.BlockConfirmed(b1Bad)
	app2.BlockDelivered(b1Bad.Hash, b1Bad.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now,
	})
	s.Require().Equal(ErrMismatchBlockHashSequence.Error(),
		app1.Compare(app2).Error())
	app2 = NewApp(0, nil)
	app2.BlockConfirmed(b0)
	app2.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now.Add(1 * time.Second),
	})
	s.Require().Equal(ErrMismatchConsensusTime.Error(),
		app1.Compare(app2).Error())
}

func (s *AppTestSuite) TestVerify() {
	var (
		now = time.Now().UTC()
		b0  = types.Block{Hash: common.Hash{}}
		b1  = types.Block{
			Hash:     common.NewRandomHash(),
			Position: types.Position{Height: 1},
		}
	)
	app := NewApp(0, nil)
	s.Require().Equal(ErrEmptyDeliverSequence.Error(), app.Verify().Error())
	app.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{})
	app.BlockDelivered(b1.Hash, b1.Position, types.FinalizationResult{Height: 1})
	s.Require().Equal(
		ErrDeliveredBlockNotConfirmed.Error(), app.Verify().Error())
	app = NewApp(0, nil)
	app.BlockConfirmed(b0)
	app.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now,
	})
	app.BlockConfirmed(b1)
	app.BlockDelivered(b1.Hash, b1.Position, types.FinalizationResult{
		Height:    2,
		Timestamp: now.Add(-1 * time.Second),
	})
	s.Require().Equal(ErrConsensusTimestampOutOfOrder.Error(),
		app.Verify().Error())
	app = NewApp(0, nil)
	app.BlockConfirmed(b0)
	app.BlockConfirmed(b1)
	app.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now,
	})
	app.BlockDelivered(b1.Hash, b1.Position, types.FinalizationResult{
		Height:    1,
		Timestamp: now.Add(1 * time.Second),
	})
}

func (s *AppTestSuite) TestWitness() {
	// Deliver several blocks, there is only one chain only.
	app := NewApp(0, nil)
	deliver := func(b *types.Block) {
		app.BlockConfirmed(*b)
		app.BlockDelivered(b.Hash, b.Position, b.Finalization)
	}
	b00 := &types.Block{
		Hash: common.NewRandomHash(),
		Finalization: types.FinalizationResult{
			Height:    1,
			Timestamp: time.Now().UTC(),
		}}
	b01 := &types.Block{
		Hash:     common.NewRandomHash(),
		Position: types.Position{Height: 1},
		Finalization: types.FinalizationResult{
			ParentHash: b00.Hash,
			Height:     2,
			Timestamp:  time.Now().UTC(),
		},
		Witness: types.Witness{
			Height: 1,
			Data:   b00.Hash.Bytes(),
		}}
	b02 := &types.Block{
		Hash:     common.NewRandomHash(),
		Position: types.Position{Height: 2},
		Finalization: types.FinalizationResult{
			ParentHash: b01.Hash,
			Height:     3,
			Timestamp:  time.Now().UTC(),
		},
		Witness: types.Witness{
			Height: 1,
			Data:   b00.Hash.Bytes(),
		}}
	deliver(b00)
	deliver(b01)
	deliver(b02)
	// A block with higher witness height, should retry later.
	s.Require().Equal(types.VerifyRetryLater, app.VerifyBlock(&types.Block{
		Witness: types.Witness{Height: 4}}))
	// Mismatched witness height and data, should return invalid.
	s.Require().Equal(types.VerifyInvalidBlock, app.VerifyBlock(&types.Block{
		Witness: types.Witness{Height: 1, Data: b01.Hash.Bytes()}}))
	// We can only verify a block followed last confirmed block.
	s.Require().Equal(types.VerifyRetryLater, app.VerifyBlock(&types.Block{
		Witness:  types.Witness{Height: 2, Data: b01.Hash.Bytes()},
		Position: types.Position{Height: 4}}))
	// It's the OK case.
	s.Require().Equal(types.VerifyOK, app.VerifyBlock(&types.Block{
		Witness:  types.Witness{Height: 2, Data: b01.Hash.Bytes()},
		Position: types.Position{Height: 3}}))
	// Check current last pending height.
	s.Require().Equal(app.LastPendingHeight, uint64(3))
	// We can only prepare witness for what've delivered.
	_, err := app.PrepareWitness(4)
	s.Require().Equal(err.Error(), ErrLowerPendingHeight.Error())
	// It should be ok to prepare for height that already delivered.
	w, err := app.PrepareWitness(3)
	s.Require().NoError(err)
	s.Require().Equal(w.Height, b02.Finalization.Height)
	s.Require().Equal(0, bytes.Compare(w.Data, b02.Hash[:]))
}

func TestApp(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
