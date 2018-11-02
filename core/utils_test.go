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

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/ecdsa"
	"github.com/dexon-foundation/dexon-consensus/core/types"
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

func (s *UtilsTestSuite) TestVerifyBlock() {
	prv, err := ecdsa.NewPrivateKey()
	s.Require().NoError(err)
	auth := NewAuthenticator(prv)
	block := &types.Block{}
	auth.SignBlock(block)
	s.NoError(VerifyBlock(block))

	hash := block.Hash
	block.Hash = common.NewRandomHash()
	s.Equal(ErrIncorrectHash, VerifyBlock(block))

	block.Hash = hash
	block.Signature, err = prv.Sign(common.NewRandomHash())
	s.Require().NoError(err)
	s.Equal(ErrIncorrectSignature, VerifyBlock(block))
}

func TestUtils(t *testing.T) {
	suite.Run(t, new(UtilsTestSuite))
}
