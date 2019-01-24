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
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type AppTestSuite struct {
	suite.Suite

	to1, to2, to3 *AppTotalOrderRecord
}

func (s *AppTestSuite) SetupSuite() {
	s.to1 = &AppTotalOrderRecord{
		BlockHashes: common.Hashes{
			common.NewRandomHash(),
			common.NewRandomHash(),
		},
		Mode: core.TotalOrderingModeNormal,
	}
	s.to2 = &AppTotalOrderRecord{
		BlockHashes: common.Hashes{
			common.NewRandomHash(),
			common.NewRandomHash(),
			common.NewRandomHash(),
		},
		Mode: core.TotalOrderingModeNormal,
	}
	s.to3 = &AppTotalOrderRecord{
		BlockHashes: common.Hashes{
			common.NewRandomHash(),
		},
		Mode: core.TotalOrderingModeNormal,
	}
}

func (s *AppTestSuite) setupAppByTotalOrderDeliver(
	app *App, to *AppTotalOrderRecord) {
	for _, h := range to.BlockHashes {
		app.BlockConfirmed(types.Block{Hash: h})
	}
	app.TotalOrderingDelivered(to.BlockHashes, to.Mode)
	for _, h := range to.BlockHashes {
		// To make it simpler, use the index of hash sequence
		// as the time.
		s.deliverBlockWithTimeFromSequenceLength(app, h)
	}
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
	req := s.Require()

	app1 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app1, s.to1)
	s.setupAppByTotalOrderDeliver(app1, s.to2)
	s.setupAppByTotalOrderDeliver(app1, s.to3)
	// An App with different deliver sequence.
	app2 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app2, s.to1)
	s.setupAppByTotalOrderDeliver(app2, s.to2)
	hash := common.NewRandomHash()
	app2.BlockConfirmed(types.Block{Hash: hash})
	app2.TotalOrderingDelivered(common.Hashes{hash}, core.TotalOrderingModeNormal)
	s.deliverBlockWithTimeFromSequenceLength(app2, hash)
	req.Equal(ErrMismatchBlockHashSequence, app1.Compare(app2))
	// An App with different consensus time for the same block.
	app3 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app3, s.to1)
	s.setupAppByTotalOrderDeliver(app3, s.to2)
	for _, h := range s.to3.BlockHashes {
		app3.BlockConfirmed(types.Block{Hash: h})
	}
	app3.TotalOrderingDelivered(s.to3.BlockHashes, s.to3.Mode)
	wrongTime := time.Time{}.Add(
		time.Duration(len(app3.DeliverSequence)) * time.Second)
	wrongTime = wrongTime.Add(1 * time.Second)
	s.deliverBlock(app3, s.to3.BlockHashes[0], wrongTime,
		uint64(len(app3.DeliverSequence)+1))
	req.Equal(ErrMismatchConsensusTime, app1.Compare(app3))
	req.Equal(ErrMismatchConsensusTime, app3.Compare(app1))
	// An App without any delivered blocks.
	app4 := NewApp(0, nil)
	req.Equal(ErrEmptyDeliverSequence, app4.Compare(app1))
	req.Equal(ErrEmptyDeliverSequence, app1.Compare(app4))
}

func (s *AppTestSuite) TestVerify() {
	req := s.Require()

	// An OK App instance.
	app1 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app1, s.to1)
	s.setupAppByTotalOrderDeliver(app1, s.to2)
	s.setupAppByTotalOrderDeliver(app1, s.to3)
	req.NoError(app1.Verify())
	// A delivered block without strongly ack
	s.deliverBlock(app1, common.NewRandomHash(), time.Time{},
		uint64(len(app1.DeliverSequence)))
	req.Equal(ErrDeliveredBlockNotConfirmed, app1.Verify())
	// The consensus time is out of order.
	app2 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app2, s.to1)
	for _, h := range s.to2.BlockHashes {
		app2.BlockConfirmed(types.Block{Hash: h})
	}
	app2.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Mode)
	s.deliverBlock(app2, s.to2.BlockHashes[0], time.Time{},
		uint64(len(app2.DeliverSequence)+1))
	req.Equal(ErrConsensusTimestampOutOfOrder, app2.Verify())
	// A delivered block is not found in total ordering delivers.
	app3 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app3, s.to1)
	hash := common.NewRandomHash()
	app3.BlockConfirmed(types.Block{Hash: hash})
	s.deliverBlockWithTimeFromSequenceLength(app3, hash)
	req.Equal(ErrMismatchTotalOrderingAndDelivered, app3.Verify())
	// A delivered block is not found in total ordering delivers.
	app4 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app4, s.to1)
	for _, h := range s.to2.BlockHashes {
		app4.BlockConfirmed(types.Block{Hash: h})
	}
	app4.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Mode)
	hash = common.NewRandomHash()
	app4.BlockConfirmed(types.Block{Hash: hash})
	app4.TotalOrderingDelivered(common.Hashes{hash}, core.TotalOrderingModeNormal)
	s.deliverBlockWithTimeFromSequenceLength(app4, hash)
	// Witness ack on unknown block.
	app5 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app5, s.to1)
	// The conensus height is out of order.
	app6 := NewApp(0, nil)
	s.setupAppByTotalOrderDeliver(app6, s.to1)
	for _, h := range s.to2.BlockHashes {
		app6.BlockConfirmed(types.Block{Hash: h})
	}
	app6.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Mode)
	s.deliverBlock(app6, s.to2.BlockHashes[0], time.Time{}.Add(
		time.Duration(len(app6.DeliverSequence))*time.Second),
		uint64(len(app6.DeliverSequence)+2))
	req.Equal(ErrConsensusHeightOutOfOrder, app6.Verify())
	// Test the acking block doesn't delivered.
	app7 := NewApp(0, nil)
	// Patch a block's acks.
	b7 := &types.Block{
		Hash: common.NewRandomHash(),
		Acks: common.NewSortedHashes(common.Hashes{common.NewRandomHash()}),
	}
	app7.BlockConfirmed(*b7)
	app7.TotalOrderingDelivered(
		common.Hashes{b7.Hash}, core.TotalOrderingModeNormal)
	app7.BlockDelivered(b7.Hash, types.Position{}, types.FinalizationResult{
		Timestamp: time.Now(),
		Height:    1,
	})
	req.Equal(ErrAckingBlockNotDelivered, app7.Verify())
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
	s.Require().IsType(err, ErrLowerPendingHeight)
	// It should be ok to prepare for height that already delivered.
	w, err := app.PrepareWitness(3)
	s.Require().NoError(err)
	s.Require().Equal(w.Height, b02.Finalization.Height)
	s.Require().Equal(0, bytes.Compare(w.Data, b02.Hash[:]))
}

func TestApp(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
