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
		app.StronglyAcked(h)
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

	app.BlockDelivered(hash, types.FinalizationResult{
		Timestamp: timestamp,
		Height:    height,
	})
}

func (s *AppTestSuite) TestCompare() {
	req := s.Require()

	app1 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app1, s.to1)
	s.setupAppByTotalOrderDeliver(app1, s.to2)
	s.setupAppByTotalOrderDeliver(app1, s.to3)
	// An App with different deliver sequence.
	app2 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app2, s.to1)
	s.setupAppByTotalOrderDeliver(app2, s.to2)
	hash := common.NewRandomHash()
	app2.StronglyAcked(hash)
	app2.TotalOrderingDelivered(common.Hashes{hash}, core.TotalOrderingModeNormal)
	s.deliverBlockWithTimeFromSequenceLength(app2, hash)
	req.Equal(ErrMismatchBlockHashSequence, app1.Compare(app2))
	// An App with different consensus time for the same block.
	app3 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app3, s.to1)
	s.setupAppByTotalOrderDeliver(app3, s.to2)
	for _, h := range s.to3.BlockHashes {
		app3.StronglyAcked(h)
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
	app4 := NewApp(nil)
	req.Equal(ErrEmptyDeliverSequence, app4.Compare(app1))
	req.Equal(ErrEmptyDeliverSequence, app1.Compare(app4))
}

func (s *AppTestSuite) TestVerify() {
	req := s.Require()

	// An OK App instance.
	app1 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app1, s.to1)
	s.setupAppByTotalOrderDeliver(app1, s.to2)
	s.setupAppByTotalOrderDeliver(app1, s.to3)
	req.NoError(app1.Verify())
	// A delivered block without strongly ack
	s.deliverBlock(app1, common.NewRandomHash(), time.Time{},
		uint64(len(app1.DeliverSequence)))
	req.Equal(ErrDeliveredBlockNotAcked, app1.Verify())
	// The consensus time is out of order.
	app2 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app2, s.to1)
	for _, h := range s.to2.BlockHashes {
		app2.StronglyAcked(h)
	}
	app2.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Mode)
	s.deliverBlock(app2, s.to2.BlockHashes[0], time.Time{},
		uint64(len(app2.DeliverSequence)+1))
	req.Equal(ErrConsensusTimestampOutOfOrder, app2.Verify())
	// A delivered block is not found in total ordering delivers.
	app3 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app3, s.to1)
	hash := common.NewRandomHash()
	app3.StronglyAcked(hash)
	s.deliverBlockWithTimeFromSequenceLength(app3, hash)
	req.Equal(ErrMismatchTotalOrderingAndDelivered, app3.Verify())
	// A delivered block is not found in total ordering delivers.
	app4 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app4, s.to1)
	for _, h := range s.to2.BlockHashes {
		app4.StronglyAcked(h)
	}
	app4.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Mode)
	hash = common.NewRandomHash()
	app4.StronglyAcked(hash)
	app4.TotalOrderingDelivered(common.Hashes{hash}, core.TotalOrderingModeNormal)
	s.deliverBlockWithTimeFromSequenceLength(app4, hash)
	// Witness ack on unknown block.
	app5 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app5, s.to1)
	// The conensus height is out of order.
	app6 := NewApp(nil)
	s.setupAppByTotalOrderDeliver(app6, s.to1)
	for _, h := range s.to2.BlockHashes {
		app6.StronglyAcked(h)
	}
	app6.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Mode)
	s.deliverBlock(app6, s.to2.BlockHashes[0], time.Time{}.Add(
		time.Duration(len(app6.DeliverSequence))*time.Second),
		uint64(len(app6.DeliverSequence)+2))
	req.Equal(ErrConsensusHeightOutOfOrder, app6.Verify())
}

func TestApp(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
