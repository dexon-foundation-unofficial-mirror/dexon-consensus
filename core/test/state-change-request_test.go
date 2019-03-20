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
	"testing"

	typesDKG "github.com/dexon-foundation/dexon-consensus/core/types/dkg"
	"github.com/stretchr/testify/suite"
)

type StateChangeRequestTestSuite struct {
	suite.Suite
}

func (s *StateChangeRequestTestSuite) TestEqual() {
	// Basically, only the cloned one would be equal.
	st00 := NewStateChangeRequest(StateChangeNotarySetSize, uint32(4))
	st01 := NewStateChangeRequest(StateChangeNotarySetSize, uint32(4))
	s.Error(ErrStatePendingChangesNotEqual, st00.Equal(st01))
	// Even with identical payload, they would be different.
	mKey := typesDKG.NewMasterPublicKey()
	st10 := NewStateChangeRequest(StateAddDKGMasterPublicKey, mKey)
	st11 := NewStateChangeRequest(StateAddDKGMasterPublicKey, mKey)
	s.Error(ErrStatePendingChangesNotEqual, st10.Equal(st11))
}

func (s *StateChangeRequestTestSuite) TestClone() {
	// The cloned one should be no error when compared with 'Equal' method.
	st00 := NewStateChangeRequest(StateChangeNotarySetSize, uint32(7))
	s.NoError(st00.Equal(st00.Clone()))
	st10 := NewStateChangeRequest(
		StateAddDKGMasterPublicKey, typesDKG.NewMasterPublicKey())
	s.NoError(st10.Equal(st10.Clone()))
}

func TestStateChangeRequest(t *testing.T) {
	suite.Run(t, new(StateChangeRequestTestSuite))
}
