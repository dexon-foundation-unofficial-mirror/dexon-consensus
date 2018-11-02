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

package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/suite"
)

type CryptoTestSuite struct {
	suite.Suite
}

func (s *CryptoTestSuite) TestHash() {
	cases := []struct {
		input  string
		output string
	}{
		{"DEXON ROCKS!",
			"1a3f3a424aaa464e51b693585bba3a0c439d5f1ad3b5868e46d9f830225983bd"},
		{"Dexon Foundation",
			"25ed4237aa978bfe706cc11c7a46a95de1a46302faea7ff6e900b03fa2b7b480"},
		{"INFINITELY SCALABLE AND LOW-LATENCY",
			"ed3384c58a434fbc0bc887a85659eddf997e7da978ab66565ac865f995b77cf1"},
	}
	for _, testcase := range cases {
		hash := Keccak256Hash([]byte(testcase.input))
		output := hex.EncodeToString(hash[:])
		s.Equal(testcase.output, output)
	}
}

func TestCrypto(t *testing.T) {
	suite.Run(t, new(CryptoTestSuite))
}
