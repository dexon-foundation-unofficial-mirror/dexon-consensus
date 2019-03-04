// Copyright 2019 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it
// and/or modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.  //
// The dexon-consensus library is distributed in the hope that it will be
// useful, but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU Lesser
// General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package syncer

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
	"github.com/stretchr/testify/suite"
)

type AgreementTestSuite struct {
	suite.Suite

	signers []*utils.Signer
	pubKeys []crypto.PublicKey
	prvKeys []crypto.PrivateKey
}

func (s *AgreementTestSuite) SetupSuite() {
	var err error
	s.prvKeys, s.pubKeys, err = test.NewKeys(4)
	s.Require().NoError(err)
	for _, k := range s.prvKeys {
		s.signers = append(s.signers, utils.NewSigner(k))
	}
}

func (s *AgreementTestSuite) prepareAgreementResult(pos types.Position,
	hash common.Hash) *types.AgreementResult {
	votes := []types.Vote{}
	for _, signer := range s.signers {
		v := types.NewVote(types.VoteCom, hash, 0)
		v.Position = pos
		s.Require().NoError(signer.SignVote(v))
		votes = append(votes, *v)
	}
	return &types.AgreementResult{
		BlockHash: hash,
		Position:  pos,
		Votes:     votes,
	}
}

func (s *AgreementTestSuite) prepareBlock(pos types.Position) *types.Block {
	b := &types.Block{
		Position: pos,
	}
	s.Require().NoError(s.signers[0].SignBlock(b))
	return b
}

func (s *AgreementTestSuite) TestFutureAgreementResult() {
	// Make sure future types.AgreementResult could be processed correctly
	// when corresponding CRS is ready.
	var (
		futureRound = uint64(7)
		pos         = types.Position{Round: 7, Height: 1000}
	)
	gov, err := test.NewGovernance(
		test.NewState(1, s.pubKeys, time.Second, &common.NullLogger{}, true),
		core.ConfigRoundShift,
	)
	s.Require().NoError(err)
	// Make sure goverance is ready for some future round, including CRS.
	gov.CatchUpWithRound(futureRound)
	for i := uint64(2); i <= futureRound; i++ {
		gov.ProposeCRS(i, common.NewRandomHash().Bytes())
	}
	s.Require().NoError(err)
	blockChan := make(chan *types.Block, 10)
	agr := newAgreement(blockChan, make(chan common.Hash, 100),
		utils.NewNodeSetCache(gov), &common.SimpleLogger{})
	go agr.run()
	block := s.prepareBlock(pos)
	result := s.prepareAgreementResult(pos, block.Hash)
	agr.inputChan <- result
	agr.inputChan <- block
	agr.inputChan <- futureRound
	select {
	case confirmedBlock := <-blockChan:
		s.Require().Equal(block.Hash, confirmedBlock.Hash)
	case <-time.After(2 * time.Second):
		s.Require().True(false)
	}
}

func TestAgreement(t *testing.T) {
	suite.Run(t, new(AgreementTestSuite))
}
