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
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
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
		Early: false,
	}
	s.to2 = &AppTotalOrderRecord{
		BlockHashes: common.Hashes{
			common.NewRandomHash(),
			common.NewRandomHash(),
			common.NewRandomHash(),
		},
		Early: false,
	}
	s.to3 = &AppTotalOrderRecord{
		BlockHashes: common.Hashes{
			common.NewRandomHash(),
		},
		Early: false,
	}
}

func (s *AppTestSuite) setupAppByTotalOrderDeliver(
	app *App, to *AppTotalOrderRecord) {

	for _, h := range to.BlockHashes {
		app.StronglyAcked(h)
	}
	app.TotalOrderingDelivered(to.BlockHashes, to.Early)
	for _, h := range to.BlockHashes {
		// To make it simpler, use the index of hash sequence
		// as the time.
		s.deliverBlockWithTimeFromSequenceLength(app, h)
	}
}

func (s *AppTestSuite) deliverBlockWithTimeFromSequenceLength(
	app *App, hash common.Hash) {

	s.deliverBlock(app, hash, time.Time{}.Add(
		time.Duration(len(app.DeliverSequence))*time.Second))
}

func (s *AppTestSuite) deliverBlock(
	app *App, hash common.Hash, timestamp time.Time) {

	app.BlockDelivered(hash, types.FinalizationResult{
		Timestamp: timestamp,
	})
}

func (s *AppTestSuite) TestCompare() {
	req := s.Require()

	app1 := NewApp()
	s.setupAppByTotalOrderDeliver(app1, s.to1)
	s.setupAppByTotalOrderDeliver(app1, s.to2)
	s.setupAppByTotalOrderDeliver(app1, s.to3)
	// An App with different deliver sequence.
	app2 := NewApp()
	s.setupAppByTotalOrderDeliver(app2, s.to1)
	s.setupAppByTotalOrderDeliver(app2, s.to2)
	hash := common.NewRandomHash()
	app2.StronglyAcked(hash)
	app2.TotalOrderingDelivered(common.Hashes{hash}, false)
	s.deliverBlockWithTimeFromSequenceLength(app2, hash)
	req.Equal(ErrMismatchBlockHashSequence, app1.Compare(app2))
	// An App with different consensus time for the same block.
	app3 := NewApp()
	s.setupAppByTotalOrderDeliver(app3, s.to1)
	s.setupAppByTotalOrderDeliver(app3, s.to2)
	for _, h := range s.to3.BlockHashes {
		app3.StronglyAcked(h)
	}
	app3.TotalOrderingDelivered(s.to3.BlockHashes, s.to3.Early)
	wrongTime := time.Time{}.Add(
		time.Duration(len(app3.DeliverSequence)) * time.Second)
	wrongTime = wrongTime.Add(1 * time.Second)
	s.deliverBlock(app3, s.to3.BlockHashes[0], wrongTime)
	req.Equal(ErrMismatchConsensusTime, app1.Compare(app3))
	req.Equal(ErrMismatchConsensusTime, app3.Compare(app1))
	// An App without any delivered blocks.
	app4 := NewApp()
	req.Equal(ErrEmptyDeliverSequence, app4.Compare(app1))
	req.Equal(ErrEmptyDeliverSequence, app1.Compare(app4))
}

func (s *AppTestSuite) TestVerify() {
	req := s.Require()

	// An OK App instance.
	app1 := NewApp()
	s.setupAppByTotalOrderDeliver(app1, s.to1)
	s.setupAppByTotalOrderDeliver(app1, s.to2)
	s.setupAppByTotalOrderDeliver(app1, s.to3)
	req.Nil(app1.Verify())
	// A delivered block without strongly ack
	s.deliverBlock(app1, common.NewRandomHash(), time.Time{})
	req.Equal(ErrDeliveredBlockNotAcked, app1.Verify())
	// The consensus time is out of order.
	app2 := NewApp()
	s.setupAppByTotalOrderDeliver(app2, s.to1)
	for _, h := range s.to2.BlockHashes {
		app2.StronglyAcked(h)
	}
	app2.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Early)
	s.deliverBlock(app2, s.to2.BlockHashes[0], time.Time{})
	req.Equal(ErrConsensusTimestampOutOfOrder, app2.Verify())
	// A delivered block is not found in total ordering delivers.
	app3 := NewApp()
	s.setupAppByTotalOrderDeliver(app3, s.to1)
	hash := common.NewRandomHash()
	app3.StronglyAcked(hash)
	s.deliverBlockWithTimeFromSequenceLength(app3, hash)
	req.Equal(ErrMismatchTotalOrderingAndDelivered, app3.Verify())
	// A delivered block is not found in total ordering delivers.
	app4 := NewApp()
	s.setupAppByTotalOrderDeliver(app4, s.to1)
	for _, h := range s.to2.BlockHashes {
		app4.StronglyAcked(h)
	}
	app4.TotalOrderingDelivered(s.to2.BlockHashes, s.to2.Early)
	hash = common.NewRandomHash()
	app4.StronglyAcked(hash)
	app4.TotalOrderingDelivered(common.Hashes{hash}, false)
	s.deliverBlockWithTimeFromSequenceLength(app4, hash)
	// Witness ack on unknown block.
	app5 := NewApp()
	s.setupAppByTotalOrderDeliver(app5, s.to1)
}

func TestApp(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
