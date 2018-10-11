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
	"math/rand"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/crypto"
	"github.com/stretchr/testify/suite"
)

type BlockTestSuite struct {
	suite.Suite
}

func (s *BlockTestSuite) randomBytes() []byte {
	h := common.NewRandomHash()
	return h[:]
}

func (s *BlockTestSuite) createRandomBlock() *Block {
	b := &Block{
		ProposerID: NodeID{common.NewRandomHash()},
		ParentHash: common.NewRandomHash(),
		Hash:       common.NewRandomHash(),
		Position: Position{
			ChainID: rand.Uint32(),
			Height:  rand.Uint64(),
		},
		Acks: common.NewSortedHashes(common.Hashes{
			common.NewRandomHash(),
			common.NewRandomHash(),
		}),
		Timestamp: time.Now().Add(time.Duration(rand.Int())),
		Witness: Witness{
			Height:    rand.Uint64(),
			Timestamp: time.Now().Add(time.Duration(rand.Int())),
			Data:      s.randomBytes(),
		},
		Finalization: FinalizationResult{
			Timestamp:  time.Now().Add(time.Duration(rand.Int())),
			Height:     rand.Uint64(),
			Randomness: s.randomBytes(),
		},
		Payload:      s.randomBytes(),
		Signature:    crypto.Signature{Signature: s.randomBytes()},
		CRSSignature: crypto.Signature{Signature: s.randomBytes()},
	}
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

func (s *BlockTestSuite) TestSortByHeight() {
	b0 := &Block{Position: Position{Height: 0}}
	b1 := &Block{Position: Position{Height: 1}}
	b2 := &Block{Position: Position{Height: 2}}
	b3 := &Block{Position: Position{Height: 3}}

	blocks := []*Block{b3, b2, b1, b0}
	sort.Sort(ByHeight(blocks))
	s.Equal(blocks[0].Hash, b0.Hash)
	s.Equal(blocks[1].Hash, b1.Hash)
	s.Equal(blocks[2].Hash, b2.Hash)
	s.Equal(blocks[3].Hash, b3.Hash)
}

func (s *BlockTestSuite) TestGenesisBlock() {
	b0 := &Block{
		Position: Position{
			Height: 0,
		},
		ParentHash: common.Hash{},
	}
	s.True(b0.IsGenesis())
	b1 := &Block{
		Position: Position{
			Height: 1,
		},
		ParentHash: common.Hash{},
	}
	s.False(b1.IsGenesis())
	b2 := &Block{
		Position: Position{
			Height: 0,
		},
		ParentHash: common.NewRandomHash(),
	}
	s.False(b2.IsGenesis())
}

func (s *BlockTestSuite) TestIsAcking() {
	// This test case would check if types.Block.IsAcking works
	ack0 := common.NewRandomHash()
	acks0 := common.Hashes{
		ack0,
		common.NewRandomHash(),
		common.NewRandomHash(),
	}
	b0 := &Block{Acks: common.NewSortedHashes(acks0)}
	s.True(b0.IsAcking(ack0))
	s.False(b0.IsAcking(common.Hash{}))
	s.False(b0.IsAcking(common.NewRandomHash()))
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

func TestBlock(t *testing.T) {
	suite.Run(t, new(BlockTestSuite))
}
