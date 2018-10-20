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

package types

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/dexon-foundation/dexon/rlp"
)

type DKGTestSuite struct {
	suite.Suite
}

func (s *DKGTestSuite) TestRLPEncodeDecode() {
	d := DKGMasterPublicKey{
		ProposerID: NodeID{common.Hash{1, 2, 3}},
		Round:      10,
		Signature: crypto.Signature{
			Type:      "123",
			Signature: []byte{4, 5, 6},
		},
	}

	b, err := rlp.EncodeToBytes(&d)
	s.Require().NoError(err)

	var dd DKGMasterPublicKey
	err = rlp.DecodeBytes(b, &dd)
	s.Require().NoError(err)

	bb, err := rlp.EncodeToBytes(&dd)
	s.Require().NoError(err)
	s.Require().True(reflect.DeepEqual(b, bb))
	s.Require().True(d.ProposerID.Equal(dd.ProposerID))
	s.Require().True(d.Round == dd.Round)
	s.Require().True(reflect.DeepEqual(d.Signature, dd.Signature))
}

func TestDKG(t *testing.T) {
	suite.Run(t, new(DKGTestSuite))
}
