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
			PublicKeyShares: *pubShare.Move(),
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

func (s *AppTestSuite) TestCompare() {
	var (
		b0 = types.Block{
			Hash:       common.Hash{},
			Position:   types.Position{Height: types.GenesisHeight},
			Randomness: []byte("b0")}
		b1 = types.Block{
			Hash:       common.NewRandomHash(),
			Position:   types.Position{Height: types.GenesisHeight + 1},
			Randomness: []byte("b1"),
		}
	)
	// Prepare an OK App instance.
	app1 := NewApp(0, nil, nil)
	app1.BlockConfirmed(b0)
	app1.BlockConfirmed(b1)
	app1.BlockDelivered(b0.Hash, b0.Position, b0.Randomness)
	app1.BlockDelivered(b1.Hash, b1.Position, b1.Randomness)
	app2 := NewApp(0, nil, nil)
	s.Require().EqualError(ErrEmptyDeliverSequence,
		app1.Compare(app2).Error())
	app2.BlockConfirmed(b0)
	app2.BlockDelivered(b0.Hash, b0.Position, b0.Randomness)
	b1Bad := types.Block{
		Hash:       common.NewRandomHash(),
		Position:   types.Position{Height: types.GenesisHeight + 1},
		Randomness: []byte("b1Bad"),
	}
	app2.BlockConfirmed(b1Bad)
	app2.BlockDelivered(b1Bad.Hash, b1Bad.Position, b1Bad.Randomness)
	s.Require().EqualError(ErrMismatchBlockHashSequence,
		app1.Compare(app2).Error())
	app2 = NewApp(0, nil, nil)
	app2.BlockConfirmed(b0)
	app2.BlockDelivered(b0.Hash, b0.Position, []byte("b0-another"))
	s.Require().EqualError(ErrMismatchRandomness, app1.Compare(app2).Error())
}

func (s *AppTestSuite) TestVerify() {
	var (
		now = time.Now().UTC()
		b0  = types.Block{
			Hash:       common.Hash{},
			Position:   types.Position{Height: types.GenesisHeight},
			Randomness: []byte("b0"),
			Timestamp:  now,
		}
		b1 = types.Block{
			Hash:       common.NewRandomHash(),
			Position:   types.Position{Height: types.GenesisHeight + 1},
			Randomness: []byte("b1"),
			Timestamp:  now.Add(1 * time.Second),
		}
	)
	// ErrDeliveredBlockNotConfirmed
	app := NewApp(0, nil, nil)
	s.Require().Equal(ErrEmptyDeliverSequence.Error(), app.Verify().Error())
	app.BlockDelivered(b0.Hash, b0.Position, b0.Randomness)
	app.BlockDelivered(b1.Hash, b1.Position, b1.Randomness)
	s.Require().EqualError(ErrDeliveredBlockNotConfirmed, app.Verify().Error())
	// ErrTimestampOutOfOrder.
	app = NewApp(0, nil, nil)
	now = time.Now().UTC()
	b0Bad := *(b0.Clone())
	b0Bad.Timestamp = now
	b1Bad := *(b1.Clone())
	b1Bad.Timestamp = now.Add(-1 * time.Second)
	app.BlockConfirmed(b0Bad)
	app.BlockDelivered(b0Bad.Hash, b0Bad.Position, b0Bad.Randomness)
	app.BlockConfirmed(b1Bad)
	app.BlockDelivered(b1Bad.Hash, b1Bad.Position, b1Bad.Randomness)
	s.Require().EqualError(ErrTimestampOutOfOrder, app.Verify().Error())
	// ErrInvalidHeight.
	app = NewApp(0, nil, nil)
	b0Bad = *(b0.Clone())
	b0Bad.Position.Height = 0
	s.Require().Panics(func() { app.BlockConfirmed(b0Bad) })
	b0Bad.Position.Height = 2
	s.Require().Panics(func() { app.BlockConfirmed(b0Bad) })
	// ErrEmptyRandomness
	app = NewApp(0, nil, nil)
	app.BlockConfirmed(b0)
	app.BlockDelivered(b0.Hash, b0.Position, []byte{})
	s.Require().EqualError(ErrEmptyRandomness, app.Verify().Error())
	// OK.
	app = NewApp(0, nil, nil)
	app.BlockConfirmed(b0)
	app.BlockConfirmed(b1)
	app.BlockDelivered(b0.Hash, b0.Position, b0.Randomness)
	app.BlockDelivered(b1.Hash, b1.Position, b1.Randomness)
	s.Require().NoError(app.Verify())
}

func (s *AppTestSuite) TestWitness() {
	// Deliver several blocks, there is only one chain only.
	app := NewApp(0, nil, nil)
	deliver := func(b *types.Block) {
		app.BlockConfirmed(*b)
		app.BlockDelivered(b.Hash, b.Position, b.Randomness)
	}
	b00 := &types.Block{
		Hash:       common.NewRandomHash(),
		Position:   types.Position{Height: 1},
		Timestamp:  time.Now().UTC(),
		Randomness: common.GenerateRandomBytes(),
	}
	b01 := &types.Block{
		Hash:       common.NewRandomHash(),
		Position:   types.Position{Height: 2},
		Timestamp:  time.Now().UTC(),
		Randomness: common.GenerateRandomBytes(),
		Witness: types.Witness{
			Height: b00.Position.Height,
			Data:   b00.Hash.Bytes(),
		}}
	b02 := &types.Block{
		Hash:       common.NewRandomHash(),
		Position:   types.Position{Height: 3},
		Timestamp:  time.Now().UTC(),
		Randomness: common.GenerateRandomBytes(),
		Witness: types.Witness{
			Height: b00.Position.Height,
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
		Witness: types.Witness{
			Height: 1,
			Data:   b01.Hash.Bytes(),
		}}))
	// We can only verify a block followed last confirmed block.
	s.Require().Equal(types.VerifyRetryLater, app.VerifyBlock(&types.Block{
		Witness: types.Witness{
			Height: b01.Position.Height,
			Data:   b01.Hash.Bytes()},
		Position: types.Position{Height: 5}}))
	// It's the OK case.
	s.Require().Equal(types.VerifyOK, app.VerifyBlock(&types.Block{
		Witness: types.Witness{
			Height: b01.Position.Height,
			Data:   b01.Hash.Bytes()},
		Position: types.Position{Height: 4}}))
	// Check current last pending height.
	_, lastRec := app.LastDeliveredRecordNoLock()
	s.Require().Equal(lastRec.Pos.Height, uint64(3))
	// We can only prepare witness for what've delivered.
	_, err := app.PrepareWitness(4)
	s.Require().Equal(err.Error(), ErrLowerPendingHeight.Error())
	// It should be ok to prepare for height that already delivered.
	w, err := app.PrepareWitness(3)
	s.Require().NoError(err)
	s.Require().Equal(w.Height, b02.Position.Height)
	s.Require().Equal(0, bytes.Compare(w.Data, b02.Hash[:]))
}

func (s *AppTestSuite) TestAttachedWithRoundEvent() {
	// This test case is copied/modified from
	// integraion.RoundEventTestSuite.TestFromRoundN, the difference is the
	// calls to utils.RoundEvent.ValidateNextRound is not explicitly called but
	// triggered by App.BlockDelivered.
	var (
		gov         = s.prepareGov()
		roundLength = uint64(100)
	)
	s.Require().NoError(gov.State().RequestChange(StateChangeRoundLength,
		roundLength))
	for r := uint64(2); r <= uint64(20); r++ {
		gov.ProposeCRS(r, getCRS(r, 0))
	}
	for r := uint64(1); r <= uint64(19); r++ {
		gov.NotifyRound(r, utils.GetRoundHeight(gov, r-1)+roundLength)
	}
	gov.NotifyRound(20, 2201)
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
	rEvt, err := utils.NewRoundEvent(context.Background(), gov, s.logger,
		types.Position{Round: 19, Height: 2019}, core.ConfigRoundShift)
	s.Require().NoError(err)
	// Register a handler to collects triggered events.
	evts := make(chan evtParamToCheck, 3)
	rEvt.Register(func(params []utils.RoundEventParam) {
		for _, p := range params {
			evts <- evtParamToCheck{
				round:  p.Round,
				reset:  p.Reset,
				height: p.BeginHeight,
				crs:    p.CRS,
			}
		}
	})
	// Setup App instance.
	app := NewApp(19, gov, rEvt)
	deliver := func(round, start, end uint64) {
		for i := start; i <= end; i++ {
			b := &types.Block{
				Hash:       common.NewRandomHash(),
				Position:   types.Position{Round: round, Height: i},
				Randomness: common.GenerateRandomBytes(),
			}
			app.BlockConfirmed(*b)
			app.BlockDelivered(b.Hash, b.Position, b.Randomness)
		}
	}
	// Deliver blocks from height=2020 to height=2092.
	for r := uint64(0); r <= uint64(19); r++ {
		begin := utils.GetRoundHeight(gov, r)
		deliver(r, begin, begin+roundLength-1)
	}
	deliver(19, 2001, 2092)
	s.Require().Equal(<-evts, evtParamToCheck{19, 1, 2001, gov.CRS(19)})
	s.Require().Equal(<-evts, evtParamToCheck{19, 2, 2101, gov.CRS(19)})
	s.Require().Equal(<-evts, evtParamToCheck{20, 0, 2201, gov.CRS(20)})
	// Deliver blocks from height=2082 to height=2281.
	deliver(19, 2093, 2200)
	deliver(20, 2201, 2292)
	s.Require().Equal(<-evts, evtParamToCheck{21, 0, 2301, gov.CRS(21)})
	// Deliver blocks from height=2282 to height=2381.
	deliver(20, 2293, 2300)
	deliver(21, 2301, 2392)
	s.Require().Equal(<-evts, evtParamToCheck{22, 0, 2401, gov.CRS(22)})
}

func TestApp(t *testing.T) {
	suite.Run(t, new(AppTestSuite))
}
