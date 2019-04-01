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

package types

import (
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto"
	"github.com/dexon-foundation/dexon/rlp"
)

type BlockTestSuite struct {
	suite.Suite
}

func (s *BlockTestSuite) noZeroInStruct(v reflect.Value) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tf := t.Field(i)
		vf := v.FieldByName(tf.Name)
		if vf.Type().Kind() == reflect.Struct {
			s.noZeroInStruct(vf)
			continue
		}
		if !vf.CanInterface() {
			s.T().Log("unable to check private field", tf.Name)
			continue
		}
		if reflect.DeepEqual(
			vf.Interface(), reflect.Zero(vf.Type()).Interface()) {
			s.Failf("", "should not be zero value %s", tf.Name)
		}
	}
}

func (s *BlockTestSuite) createRandomBlock() *Block {
	payload := common.GenerateRandomBytes()
	b := &Block{
		ProposerID: NodeID{common.NewRandomHash()},
		ParentHash: common.NewRandomHash(),
		Hash:       common.NewRandomHash(),
		Position: Position{
			Round:  rand.Uint64(),
			Height: rand.Uint64(),
		},
		Timestamp: time.Now().UTC(),
		Witness: Witness{
			Height: rand.Uint64(),
			Data:   common.GenerateRandomBytes(),
		},
		Randomness:  common.GenerateRandomBytes(),
		Payload:     payload,
		PayloadHash: crypto.Keccak256Hash(payload),
		Signature: crypto.Signature{
			Type:      "some type",
			Signature: common.GenerateRandomBytes()},
		CRSSignature: crypto.Signature{
			Type:      "some type",
			Signature: common.GenerateRandomBytes()},
	}
	// Check if all fields are initialized with non zero values.
	s.noZeroInStruct(reflect.ValueOf(*b))
	return b
}

func (s *BlockTestSuite) TestCreateRandomBlock() {
	b1 := *s.createRandomBlock()
	b2 := *s.createRandomBlock()

	v1 := reflect.ValueOf(b1)
	v2 := reflect.ValueOf(b2)
	for i := 0; i < v1.NumField(); i++ {
		f1 := v1.Field(i)
		f2 := v2.Field(i)
		if reflect.DeepEqual(f1.Interface(), f2.Interface()) {
			s.Failf("Non randomized field found", "Field %s is not randomized\n",
				v1.Type().Field(i).Name)
		}
	}
}

func (s *BlockTestSuite) TestSortByHash() {
	hash := common.Hash{}
	copy(hash[:], "aaaaaa")
	b0 := &Block{Hash: hash}
	copy(hash[:], "bbbbbb")
	b1 := &Block{Hash: hash}
	copy(hash[:], "cccccc")
	b2 := &Block{Hash: hash}
	copy(hash[:], "dddddd")
	b3 := &Block{Hash: hash}

	blocks := []*Block{b3, b2, b1, b0}
	sort.Sort(ByHash(blocks))
	s.Equal(blocks[0].Hash, b0.Hash)
	s.Equal(blocks[1].Hash, b1.Hash)
	s.Equal(blocks[2].Hash, b2.Hash)
	s.Equal(blocks[3].Hash, b3.Hash)
}

func (s *BlockTestSuite) TestSortBlocksByPosition() {
	b00 := &Block{Hash: common.NewRandomHash(), Position: Position{Height: 0}}
	b01 := &Block{Hash: common.NewRandomHash(), Position: Position{Height: 1}}
	b02 := &Block{Hash: common.NewRandomHash(), Position: Position{Height: 2}}
	b10 := &Block{Hash: common.NewRandomHash(),
		Position: Position{Round: 1, Height: 0}}
	b11 := &Block{Hash: common.NewRandomHash(),
		Position: Position{Round: 1, Height: 1}}
	b12 := &Block{Hash: common.NewRandomHash(),
		Position: Position{Round: 1, Height: 2}}

	blocks := []*Block{b12, b11, b10, b02, b01, b00}
	sort.Sort(BlocksByPosition(blocks))
	s.Equal(blocks[0].Hash, b00.Hash)
	s.Equal(blocks[1].Hash, b01.Hash)
	s.Equal(blocks[2].Hash, b02.Hash)
	s.Equal(blocks[3].Hash, b10.Hash)
	s.Equal(blocks[4].Hash, b11.Hash)
	s.Equal(blocks[5].Hash, b12.Hash)
}

func (s *BlockTestSuite) TestGenesisBlock() {
	b0 := &Block{
		Position: Position{
			Height: GenesisHeight,
		},
		ParentHash: common.Hash{},
	}
	s.True(b0.IsGenesis())
	b1 := &Block{
		Position: Position{
			Height: GenesisHeight + 1,
		},
		ParentHash: common.Hash{},
	}
	s.False(b1.IsGenesis())
	b2 := &Block{
		Position: Position{
			Height: GenesisHeight,
		},
		ParentHash: common.NewRandomHash(),
	}
	s.False(b2.IsGenesis())
}

func (s *BlockTestSuite) TestClone() {
	b1 := *s.createRandomBlock()
	b2 := *b1.Clone()

	// Use reflect here to better understand the error message.
	v1 := reflect.ValueOf(b1)
	v2 := reflect.ValueOf(b2)
	for i := 0; i < v1.NumField(); i++ {
		f1 := v1.Field(i)
		f2 := v2.Field(i)
		if !reflect.DeepEqual(f1.Interface(), f2.Interface()) {
			s.Failf("Field Not Equal", "Field %s is not equal.\n-%v\n+%v\n",
				v1.Type().Field(i).Name,
				f1, f2)
		}
	}
}

func (s *BlockTestSuite) TestRLPEncodeDecode() {
	block := s.createRandomBlock()
	b, err := rlp.EncodeToBytes(block)
	s.Require().NoError(err)

	var dec Block
	err = rlp.DecodeBytes(b, &dec)
	s.Require().NoError(err)

	s.Require().True(reflect.DeepEqual(block, &dec))
}

func TestBlock(t *testing.T) {
	suite.Run(t, new(BlockTestSuite))
}
