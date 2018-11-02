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

package ecdsa

import (
	"testing"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/stretchr/testify/suite"
)

type ETHCryptoTestSuite struct {
	suite.Suite
}

func (s *ETHCryptoTestSuite) TestSignature() {
	prv1, err := NewPrivateKey()
	s.Require().Nil(err)
	hash1 := common.NewRandomHash()
	hash2 := common.NewRandomHash()

	// Test that same private key should produce same signature.
	sig11, err := prv1.Sign(hash1)
	s.Require().Nil(err)
	sig112, err := prv1.Sign(hash1)
	s.Require().Nil(err)
	s.Equal(sig11, sig112)

	// Test that different private key should produce different signature.
	prv2, err := NewPrivateKey()
	s.Require().Nil(err)
	sig21, err := prv2.Sign(hash1)
	s.Require().Nil(err)
	s.NotEqual(sig11, sig21)

	// Test that different hash should produce different signature.
	sig12, err := prv1.Sign(hash2)
	s.Require().Nil(err)
	s.NotEqual(sig11, sig12)

	// Test VerifySignature with correct public key.
	pub1, ok := prv1.PublicKey().(*PublicKey)
	s.Require().True(ok)
	s.True(pub1.VerifySignature(hash1, sig11))

	// Test VerifySignature with wrong hash.
	s.False(pub1.VerifySignature(hash2, sig11))
	// Test VerifySignature with wrong signature.
	s.False(pub1.VerifySignature(hash1, sig21))
	// Test VerifySignature with wrong public key.
	pub2 := prv2.PublicKey()
	s.False(pub2.VerifySignature(hash1, sig11))
}

func (s *ETHCryptoTestSuite) TestSigToPub() {
	prv, err := NewPrivateKey()
	s.Require().Nil(err)
	data := "DEXON is infinitely scalable and low-latency."
	hash := crypto.Keccak256Hash([]byte(data))
	sigmsg, err := prv.Sign(hash)
	s.Require().Nil(err)

	pubkey, err := SigToPub(hash, sigmsg)
	s.Require().Nil(err)
	s.Equal(pubkey, prv.PublicKey())
}

func TestCrypto(t *testing.T) {
	suite.Run(t, new(ETHCryptoTestSuite))
}
