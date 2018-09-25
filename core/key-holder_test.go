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

package core

import (
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/dexon-foundation/dexon-consensus-core/crypto/eth"
	"github.com/stretchr/testify/suite"
)

type KeyHolderTestSuite struct {
	suite.Suite
}

func (s *KeyHolderTestSuite) setupKeyHolder() *keyHolder {
	k, err := eth.NewPrivateKey()
	s.NoError(err)
	return newKeyHolder(k, eth.SigToPub)
}

func (s *KeyHolderTestSuite) TestBlock() {
	k := s.setupKeyHolder()
	b := &types.Block{
		ParentHash: common.NewRandomHash(),
		Position: types.Position{
			ShardID: 1,
			ChainID: 2,
			Height:  3,
		},
		Timestamp: time.Now().UTC(),
	}
	s.NoError(k.SignBlock(b))
	s.NoError(k.VerifyBlock(b))
}

func (s *KeyHolderTestSuite) TestVote() {
	k := s.setupKeyHolder()
	v := &types.Vote{
		ProposerID: types.NodeID{Hash: common.NewRandomHash()},
		Type:       types.VoteConfirm,
		BlockHash:  common.NewRandomHash(),
		Period:     123,
		Position: types.Position{
			ShardID: 2,
			ChainID: 4,
			Height:  6,
		}}
	s.NoError(k.SignVote(v))
	ok, err := k.VerifyVote(v)
	s.True(ok)
	s.NoError(err)
}

func (s *KeyHolderTestSuite) TestCRS() {
	k := s.setupKeyHolder()
	b := &types.Block{
		ParentHash: common.NewRandomHash(),
		Position: types.Position{
			ShardID: 7,
			ChainID: 8,
			Height:  9,
		},
		Timestamp: time.Now().UTC(),
	}
	crs := common.NewRandomHash()
	s.Error(k.SignCRS(b, crs))
	// Hash block before hash CRS.
	s.NoError(k.SignBlock(b))
	s.NoError(k.SignCRS(b, crs))
	ok, err := k.VerifyCRS(b, crs)
	s.True(ok)
	s.NoError(err)
}

func TestKeyHolder(t *testing.T) {
	suite.Run(t, new(KeyHolderTestSuite))
}
