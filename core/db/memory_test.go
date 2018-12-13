// Copyright 2018 The dexon-consensus Authors
// This file is part of the dexon-consensus library.
//
// The dexon-consensus library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus library. If not, see
// <http://www.gnu.org/licenses/>.

package db

import (
	"os"
	"testing"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
	"github.com/stretchr/testify/suite"
)

type MemBackedDBTestSuite struct {
	suite.Suite

	v0            types.NodeID
	b00, b01, b02 *types.Block
}

func (s *MemBackedDBTestSuite) SetupSuite() {
	s.v0 = types.NodeID{Hash: common.NewRandomHash()}

	genesisHash := common.NewRandomHash()
	s.b00 = &types.Block{
		ProposerID: s.v0,
		ParentHash: genesisHash,
		Hash:       genesisHash,
		Position: types.Position{
			Height: 0,
		},
		Acks: common.NewSortedHashes(common.Hashes{}),
	}
	s.b01 = &types.Block{
		ProposerID: s.v0,
		ParentHash: s.b00.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height: 1,
		},
		Acks: common.NewSortedHashes(common.Hashes{s.b00.Hash}),
	}
	s.b02 = &types.Block{
		ProposerID: s.v0,
		ParentHash: s.b01.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height: 2,
		},
		Acks: common.NewSortedHashes(common.Hashes{s.b01.Hash}),
	}
}

func (s *MemBackedDBTestSuite) TestSaveAndLoad() {
	// Make sure we are able to save/load from file.
	dbPath := "test-save-and-load.db"

	// Make sure the file pointed by 'dbPath' doesn't exist.
	_, err := os.Stat(dbPath)
	s.Require().NotNil(err)

	dbInst, err := NewMemBackedDB(dbPath)
	s.Require().Nil(err)
	s.Require().NotNil(dbInst)
	defer func() {
		if dbInst != nil {
			s.Nil(os.Remove(dbPath))
			dbInst = nil
		}
	}()

	s.Nil(dbInst.PutBlock(*s.b00))
	s.Nil(dbInst.PutBlock(*s.b01))
	s.Nil(dbInst.PutBlock(*s.b02))
	s.Nil(dbInst.Close())

	// Load the json file back to check if all inserted blocks
	// exists.
	dbInst, err = NewMemBackedDB(dbPath)
	s.Require().Nil(err)
	s.Require().NotNil(dbInst)
	s.True(dbInst.HasBlock(s.b00.Hash))
	s.True(dbInst.HasBlock(s.b01.Hash))
	s.True(dbInst.HasBlock(s.b02.Hash))
	s.Nil(dbInst.Close())
}

func (s *MemBackedDBTestSuite) TestIteration() {
	// Make sure the file pointed by 'dbPath' doesn't exist.
	dbInst, err := NewMemBackedDB()
	s.Require().Nil(err)
	s.Require().NotNil(dbInst)

	// Setup database.
	s.Nil(dbInst.PutBlock(*s.b00))
	s.Nil(dbInst.PutBlock(*s.b01))
	s.Nil(dbInst.PutBlock(*s.b02))

	// Check if we can iterate all 3 blocks.
	iter, err := dbInst.GetAllBlocks()
	s.Require().Nil(err)
	touched := common.Hashes{}
	for {
		b, err := iter.NextBlock()
		if err == ErrIterationFinished {
			break
		}
		s.Require().Nil(err)
		touched = append(touched, b.Hash)
	}
	s.Len(touched, 3)
	s.Contains(touched, s.b00.Hash)
	s.Contains(touched, s.b01.Hash)
	s.Contains(touched, s.b02.Hash)
}

func TestMemBackedDB(t *testing.T) {
	suite.Run(t, new(MemBackedDBTestSuite))
}
