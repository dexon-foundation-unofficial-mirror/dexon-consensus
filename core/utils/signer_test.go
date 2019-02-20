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

package utils

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type SignerTestSuite struct {
	suite.Suite
}

func (s *SignerTestSuite) setupSigner() *Signer {
	k, err := ecdsa.NewPrivateKey()
	s.NoError(err)
	return NewSigner(k)
}

func (s *SignerTestSuite) TestBlock() {
	k := s.setupSigner()
	b := &types.Block{
		ParentHash: common.NewRandomHash(),
		Position: types.Position{
			Round:  2,
			Height: 3,
		},
		Timestamp: time.Now().UTC(),
	}
	s.NoError(k.SignBlock(b))
	s.NoError(VerifyBlockSignature(b))
}

func (s *SignerTestSuite) TestVote() {
	k := s.setupSigner()
	v := types.NewVote(types.VoteCom, common.NewRandomHash(), 123)
	v.Position = types.Position{
		Round:  4,
		Height: 6,
	}
	v.ProposerID = types.NodeID{Hash: common.NewRandomHash()}
	s.NoError(k.SignVote(v))
	ok, err := VerifyVoteSignature(v)
	s.True(ok)
	s.NoError(err)
}

func (s *SignerTestSuite) TestCRS() {
	k := s.setupSigner()
	b := &types.Block{
		ParentHash: common.NewRandomHash(),
		Position: types.Position{
			Round:  8,
			Height: 9,
		},
		Timestamp: time.Now().UTC(),
	}
	crs := common.NewRandomHash()
	s.Error(k.SignCRS(b, crs))
	// Hash block before hash CRS.
	s.NoError(k.SignBlock(b))
	s.NoError(k.SignCRS(b, crs))
	ok, err := VerifyCRSSignature(b, crs)
	s.True(ok)
	s.NoError(err)
}

func TestSigner(t *testing.T) {
	suite.Run(t, new(SignerTestSuite))
}
