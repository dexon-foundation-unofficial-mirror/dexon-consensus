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

package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/test"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/dexon-foundation/dexon-consensus/core/utils"
)

type UtilsTestSuite struct {
	suite.Suite
}

func (s *UtilsTestSuite) TestRemoveFromSortedUint32Slice() {
	// Remove something exists.
	xs := []uint32{1, 2, 3, 4, 5}
	s.Equal(
		removeFromSortedUint32Slice(xs, 3),
		[]uint32{1, 2, 4, 5})
	// Remove something not exists.
	s.Equal(removeFromSortedUint32Slice(xs, 6), xs)
	// Remove from empty slice, should not panic.
	s.Equal([]uint32{}, removeFromSortedUint32Slice([]uint32{}, 1))
}

func (s *UtilsTestSuite) TestVerifyAgreementResult() {
	prvKeys, pubKeys, err := test.NewKeys(4)
	s.Require().NoError(err)
	gov, err := test.NewGovernance(test.NewState(DKGDelayRound,
		pubKeys, time.Second, &common.NullLogger{}, true), ConfigRoundShift)
	s.Require().NoError(err)
	cache := utils.NewNodeSetCache(gov)
	hash := common.NewRandomHash()
	signers := make([]*utils.Signer, 0, len(prvKeys))
	for _, prvKey := range prvKeys {
		signers = append(signers, utils.NewSigner(prvKey))
	}
	pos := types.Position{
		Round:  0,
		Height: 20,
	}
	baResult := &types.AgreementResult{
		BlockHash: hash,
		Position:  pos,
	}
	for _, signer := range signers {
		vote := types.NewVote(types.VoteCom, hash, 0)
		vote.Position = pos
		s.Require().NoError(signer.SignVote(vote))
		baResult.Votes = append(baResult.Votes, *vote)
	}
	s.Require().NoError(VerifyAgreementResult(baResult, cache))

	// Test negative case.
	// All period should be the same.
	baResult.Votes[1].Period++
	s.Equal(ErrIncorrectVotePeriod, VerifyAgreementResult(baResult, cache))
	baResult.Votes[1].Period--

	// Blockhash should match the one in votes.
	baResult.BlockHash = common.NewRandomHash()
	s.Equal(ErrIncorrectVoteBlockHash, VerifyAgreementResult(baResult, cache))
	baResult.BlockHash = hash

	// Position should match.
	baResult.Position.Height++
	s.Equal(ErrIncorrectVotePosition, VerifyAgreementResult(baResult, cache))
	baResult.Position = pos

	// types.VotePreCom is not accepted in agreement result.
	baResult.Votes[0].Type = types.VotePreCom
	s.Equal(ErrIncorrectVoteType, VerifyAgreementResult(baResult, cache))
	baResult.Votes[0].Type = types.VoteCom

	// Vote type should be the same.
	baResult.Votes[1].Type = types.VoteFastCom
	s.Equal(ErrIncorrectVoteType, VerifyAgreementResult(baResult, cache))
	baResult.Votes[1].Type = types.VoteCom

	// Only vote proposed by notarySet is valid.
	baResult.Votes[0].ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	s.Equal(ErrIncorrectVoteProposer, VerifyAgreementResult(baResult, cache))
	baResult.Votes[0].ProposerID = types.NewNodeID(pubKeys[0])

	// Vote shuold have valid signature.
	baResult.Votes[0].Signature, err = prvKeys[0].Sign(common.NewRandomHash())
	s.Require().NoError(err)
	s.Equal(ErrIncorrectVoteSignature, VerifyAgreementResult(baResult, cache))
	s.Require().NoError(signers[0].SignVote(&baResult.Votes[0]))

	// Unique votes shuold be more than threshold.
	baResult.Votes = baResult.Votes[:1]
	s.Equal(ErrNotEnoughVotes, VerifyAgreementResult(baResult, cache))
	for range signers {
		baResult.Votes = append(baResult.Votes, baResult.Votes[0])
	}
	s.Equal(ErrNotEnoughVotes, VerifyAgreementResult(baResult, cache))
}

func TestUtils(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
