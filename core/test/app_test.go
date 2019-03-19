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
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

func getCRS(round, reset uint64) []byte {
	return []byte(fmt.Sprintf("r#%d,reset#%d", round, reset))
}

type evtParamToCheck struct {
	round  uint64
	reset  uint64
	height uint64
	crs    common.Hash
}

type AppTestSuite struct {
	suite.Suite

	pubKeys []crypto.PublicKey
	signers []*utils.Signer
	logger  common.Logger
}

func (s *AppTestSuite) SetupSuite() {
	prvKeys, pubKeys, err := NewKeys(4)
	s.Require().NoError(err)
	s.pubKeys = pubKeys
	for _, k := range prvKeys {
		s.signers = append(s.signers, utils.NewSigner(k))
	}
	s.logger = &common.NullLogger{}
}

func (s *AppTestSuite) prepareGov() *Governance {
	gov, err := NewGovernance(
		NewState(1, s.pubKeys, 100*time.Millisecond, s.logger, true),
		core.ConfigRoundShift)
	s.Require().NoError(err)
	return gov
}

func (s *AppTestSuite) proposeMPK(
	gov *Governance,
	round, reset uint64,
	count int) {
	for idx, pubKey := range s.pubKeys[:count] {
		_, pubShare := dkg.NewPrivateKeyShares(utils.GetDKGThreshold(
			gov.Configuration(round)))
		mpk := &typesDKG.MasterPublicKey{
			Round:           round,
			Reset:           reset,
			DKGID:           typesDKG.NewID(types.NewNodeID(pubKey)),
			PublicKeyShares: *pubShare,
		}
		s.Require().NoError(s.signers[idx].SignDKGMasterPublicKey(mpk))
		gov.AddDKGMasterPublicKey(mpk)
	}
}

func (s *AppTestSuite) proposeFinalize(
	gov *Governance,
	round, reset uint64,
	count int) {
	for idx, pubKey := range s.pubKeys[:count] {
		final := &typesDKG.Finalize{
			ProposerID: types.NewNodeID(pubKey),
			Round:      round,
			Reset:      reset,
		}
		s.Require().NoError(s.signers[idx].SignDKGFinalize(final))
		gov.AddDKGFinalize(final)
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
	var (
		now = time.Now().UTC()
		b0  = types.Block{Hash: common.Hash{}}
		b1  = types.Block{
			Hash:     common.NewRandomHash(),
			Position: types.Position{Height: 1},
		}
	)
	// Prepare an OK App instance.
	app1 := NewApp(0, nil, nil)
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
	app2 := NewApp(0, nil, nil)
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
	app2 = NewApp(0, nil, nil)
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
	app := NewApp(0, nil, nil)
	s.Require().Equal(ErrEmptyDeliverSequence.Error(), app.Verify().Error())
	app.BlockDelivered(b0.Hash, b0.Position, types.FinalizationResult{})
	app.BlockDelivered(b1.Hash, b1.Position, types.FinalizationResult{Height: 1})
	s.Require().Equal(
		ErrDeliveredBlockNotConfirmed.Error(), app.Verify().Error())
	app = NewApp(0, nil, nil)
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
	app = NewApp(0, nil, nil)
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
	app := NewApp(0, nil, nil)
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

func (s *AppTestSuite) TestAttachedWithRoundEvent() {
	// This test case is copied/modified from
	// integraion.RoundEventTestSuite.TestFromRoundN, the difference is the
	// calls to utils.RoundEvent.ValidateNextRound is not explicitly called but
	// triggered by App.BlockDelivered.
	gov := s.prepareGov()
	s.Require().NoError(gov.State().RequestChange(StateChangeRoundLength,
		uint64(100)))
	gov.CatchUpWithRound(22)
	for r := uint64(2); r <= uint64(20); r++ {
		gov.ProposeCRS(r, getCRS(r, 0))
	}
	// Reset round#20 twice, then make it done DKG preparation.
	gov.ResetDKG(getCRS(20, 1))
	gov.ResetDKG(getCRS(20, 2))
	s.proposeMPK(gov, 20, 2, 3)
	s.proposeFinalize(gov, 20, 2, 3)
	s.Require().Equal(gov.DKGResetCount(20), uint64(2))
	// Propose CRS for round#21, and it works without reset.
	gov.ProposeCRS(21, getCRS(21, 0))
	s.proposeMPK(gov, 21, 0, 3)
	s.proposeFinalize(gov, 21, 0, 3)
	// Propose CRS for round#22, and it works without reset.
	gov.ProposeCRS(22, getCRS(22, 0))
	s.proposeMPK(gov, 22, 0, 3)
	s.proposeFinalize(gov, 22, 0, 3)
	// Prepare utils.RoundEvent, starts from round#19, reset(for round#20)#1.
	rEvt, err := utils.NewRoundEvent(context.Background(), gov, s.logger, 19,
		1900, 2019, core.ConfigRoundShift)
	s.Require().NoError(err)
	// Register a handler to collects triggered events.
	var evts []evtParamToCheck
	rEvt.Register(func(params []utils.RoundEventParam) {
		for _, p := range params {
			evts = append(evts, evtParamToCheck{
				round:  p.Round,
				reset:  p.Reset,
				height: p.BeginHeight,
				crs:    p.CRS,
			})
		}
	})
	// Setup App instance.
	app := NewApp(19, gov, rEvt)
	deliver := func(round, start, end uint64) {
		for i := start; i <= end; i++ {
			b := &types.Block{
				Hash:         common.NewRandomHash(),
				Position:     types.Position{Round: round, Height: i},
				Finalization: types.FinalizationResult{Height: i},
			}
			app.BlockConfirmed(*b)
			app.BlockDelivered(b.Hash, b.Position, b.Finalization)
		}
	}
	// Deliver blocks from height=2020 to height=2081.
	deliver(0, 0, 2019)
	deliver(19, 2020, 2091)
	s.Require().Len(evts, 2)
	s.Require().Equal(evts[0], evtParamToCheck{19, 2, 2100, gov.CRS(19)})
	s.Require().Equal(evts[1], evtParamToCheck{20, 0, 2200, gov.CRS(20)})
	// Deliver blocks from height=2082 to height=2281.
	deliver(19, 2092, 2199)
	deliver(20, 2200, 2291)
	s.Require().Len(evts, 3)
	s.Require().Equal(evts[2], evtParamToCheck{21, 0, 2300, gov.CRS(21)})
	// Deliver blocks from height=2282 to height=2381.
	deliver(20, 2292, 2299)
	deliver(21, 2300, 2391)
	s.Require().Equal(evts[3], evtParamToCheck{22, 0, 2400, gov.CRS(22)})
}

func TestApp(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
