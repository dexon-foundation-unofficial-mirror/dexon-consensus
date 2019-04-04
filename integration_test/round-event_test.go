// Copyright 2019 The dexon-consensus Authors
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

package integration

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/test"
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

type RoundEventTestSuite struct {
	suite.Suite

	pubKeys []crypto.PublicKey
	signers []*utils.Signer
	logger  common.Logger
}

func (s *RoundEventTestSuite) SetupSuite() {
	prvKeys, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	s.pubKeys = pubKeys
	for _, k := range prvKeys {
		s.signers = append(s.signers, utils.NewSigner(k))
	}
	s.logger = &common.NullLogger{}
}

func (s *RoundEventTestSuite) prepareGov() *test.Governance {
	gov, err := test.NewGovernance(
		test.NewState(1, s.pubKeys, 100*time.Millisecond, s.logger, true),
		core.ConfigRoundShift)
	s.Require().NoError(err)
	return gov
}

func (s *RoundEventTestSuite) proposeMPK(
	gov *test.Governance,
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

func (s *RoundEventTestSuite) proposeFinalize(
	gov *test.Governance,
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

func (s *RoundEventTestSuite) TestFromRound0() {
	// Prepare test.Governance.
	gov := s.prepareGov()
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
		uint64(100)))
	gov.CatchUpWithRound(0)
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
		uint64(200)))
	gov.CatchUpWithRound(1)
	// Prepare utils.RoundEvent, starts from genesis.
	rEvt, err := utils.NewRoundEvent(context.Background(), gov, s.logger,
		types.Position{Height: types.GenesisHeight}, core.ConfigRoundShift)
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
			// Tricky part to make sure passed config is correct.
			s.Require().Equal((p.Round+1)*100, p.Config.RoundLength)
		}
	})
	// Reset round#1 twice, then make it ready.
	gov.ResetDKG([]byte("DKG round 1 reset 1"))
	gov.ResetDKG([]byte("DKG round 1 reset 2"))
	s.proposeMPK(gov, 1, 2, 3)
	s.proposeFinalize(gov, 1, 2, 3)
	s.Require().Equal(rEvt.ValidateNextRound(80), uint(3))
	// Check collected events.
	s.Require().Len(evts, 3)
	s.Require().Equal(evts[0], evtParamToCheck{0, 1, 101, gov.CRS(0)})
	s.Require().Equal(evts[1], evtParamToCheck{0, 2, 201, gov.CRS(0)})
	s.Require().Equal(evts[2], evtParamToCheck{1, 0, 301, gov.CRS(1)})
}

func (s *RoundEventTestSuite) TestFromRoundN() {
	// Prepare test.Governance.
	var (
		gov         = s.prepareGov()
		roundLength = uint64(100)
	)
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
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
	// Check for round#19, reset(for round#20)#2 at height=2080.
	s.Require().Equal(rEvt.ValidateNextRound(2080), uint(2))
	// Check collected events.
	s.Require().Len(evts, 2)
	s.Require().Equal(evts[0], evtParamToCheck{19, 2, 2101, gov.CRS(19)})
	s.Require().Equal(evts[1], evtParamToCheck{20, 0, 2201, gov.CRS(20)})
	// Round might exceed round-shift limitation would not be triggered.
	s.Require().Equal(rEvt.ValidateNextRound(2280), uint(1))
	s.Require().Len(evts, 3)
	s.Require().Equal(evts[2], evtParamToCheck{21, 0, 2301, gov.CRS(21)})
	s.Require().Equal(rEvt.ValidateNextRound(2380), uint(1))
	s.Require().Equal(evts[3], evtParamToCheck{22, 0, 2401, gov.CRS(22)})
}

func (s *RoundEventTestSuite) TestLastPeriod() {
	gov := s.prepareGov()
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
		uint64(100)))
	gov.CatchUpWithRound(0)
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
		uint64(200)))
	gov.CatchUpWithRound(1)
	// Prepare utils.RoundEvent, starts from genesis.
	rEvt, err := utils.NewRoundEvent(context.Background(), gov, s.logger,
		types.Position{Height: types.GenesisHeight}, core.ConfigRoundShift)
	s.Require().NoError(err)
	begin, length := rEvt.LastPeriod()
	s.Require().Equal(begin, uint64(1))
	s.Require().Equal(length, uint64(100))
	// Reset round#1 twice, then make it ready.
	gov.ResetDKG([]byte("DKG round 1 reset 1"))
	gov.ResetDKG([]byte("DKG round 1 reset 2"))
	rEvt.ValidateNextRound(80)
	begin, length = rEvt.LastPeriod()
	s.Require().Equal(begin, uint64(201))
	s.Require().Equal(length, uint64(100))
	s.proposeMPK(gov, 1, 2, 3)
	s.proposeFinalize(gov, 1, 2, 3)
	rEvt.ValidateNextRound(80)
	begin, length = rEvt.LastPeriod()
	s.Require().Equal(begin, uint64(301))
	s.Require().Equal(length, uint64(200))
}

func (s *RoundEventTestSuite) TestTriggerInitEvent() {
	gov := s.prepareGov()
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
		uint64(100)))
	gov.CatchUpWithRound(0)
	s.Require().NoError(gov.State().RequestChange(test.StateChangeRoundLength,
		uint64(200)))
	gov.CatchUpWithRound(1)
	// Prepare utils.RoundEvent, starts from genesis.
	rEvt, err := utils.NewRoundEvent(context.Background(), gov, s.logger,
		types.Position{Height: types.GenesisHeight}, core.ConfigRoundShift)
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
	rEvt.TriggerInitEvent()
	s.Require().Len(evts, 1)
	s.Require().Equal(evts[0], evtParamToCheck{0, 0, 1, gov.CRS(0)})
}

func TestRoundEvent(t *testing.T) {
	suite.Run(t, new(RoundEventTestSuite))
}
