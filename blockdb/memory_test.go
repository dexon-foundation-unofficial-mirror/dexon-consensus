// Copyright 2018 The dexon-consensus-core Authors
// This file is part of the dexon-consensus-core library.
//
// The dexon-consensus-core library is free software: you can redistribute it and/or
// modify it under the terms of the GNU Lesser General Public License as
// published by the Free Software Foundation, either version 3 of the License,
// or (at your option) any later version.
//
// The dexon-consensus-core library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the dexon-consensus-core library. If not, see
// <http://www.gnu.org/licenses/>.

package blockdb

import (
	"os"
	"testing"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
	"github.com/stretchr/testify/suite"
)

type MemBackedBlockDBTestSuite struct {
	suite.Suite

	v0            types.ValidatorID
	b00, b01, b02 *types.Block
}

func (s *MemBackedBlockDBTestSuite) SetupSuite() {
	s.v0 = types.ValidatorID{Hash: common.NewRandomHash()}

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

func (s *MemBackedBlockDBTestSuite) TestSaveAndLoad() {
	// Make sure we are able to save/load from file.
	dbPath := "test-save-and-load.db"

	// Make sure the file pointed by 'dbPath' doesn't exist.
	_, err := os.Stat(dbPath)
	s.Require().NotNil(err)

	db, err := NewMemBackedBlockDB(dbPath)
	s.Require().Nil(err)
	s.Require().NotNil(db)
	defer func() {
		if db != nil {
			s.Nil(os.Remove(dbPath))
			db = nil
		}
	}()

	s.Nil(db.Put(*s.b00))
	s.Nil(db.Put(*s.b01))
	s.Nil(db.Put(*s.b02))
	s.Nil(db.Close())

	// Load the json file back to check if all inserted blocks
	// exists.
	db, err = NewMemBackedBlockDB(dbPath)
	s.Require().Nil(err)
	s.Require().NotNil(db)
	s.True(db.Has(s.b00.Hash))
	s.True(db.Has(s.b01.Hash))
	s.True(db.Has(s.b02.Hash))
	s.Nil(db.Close())
}

func (s *MemBackedBlockDBTestSuite) TestIteration() {
	// Make sure the file pointed by 'dbPath' doesn't exist.
	db, err := NewMemBackedBlockDB()
	s.Require().Nil(err)
	s.Require().NotNil(db)

	// Setup database.
	s.Nil(db.Put(*s.b00))
	s.Nil(db.Put(*s.b01))
	s.Nil(db.Put(*s.b02))

	// Check if we can iterate all 3 blocks.
	iter, err := db.GetAll()
	s.Require().Nil(err)
	touched := common.Hashes{}
	for {
		b, err := iter.Next()
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

func TestMemBackedBlockDB(t *testing.T) {
	suite.Run(t, new(MemBackedBlockDBTestSuite))
}
