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

package blockdb

import (
	"fmt"
	"testing"
	"time"

	"os"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus-core/common"
	"github.com/dexon-foundation/dexon-consensus-core/core/types"
)

type LevelDBTestSuite struct {
	suite.Suite
}

func (s *LevelDBTestSuite) TestBasicUsage() {
	dbName := fmt.Sprintf("test-db-%v.db", time.Now().UTC())
	db, err := NewLevelDBBackedBlockDB(dbName)
	s.Require().Nil(err)
	defer func(dbName string) {
		err = db.Close()
		s.Nil(err)
		err = os.RemoveAll(dbName)
		s.Nil(err)
	}(dbName)

	// Queried something from an empty database.
	hash1 := common.NewRandomHash()
	_, err = db.Get(hash1)
	s.Equal(ErrBlockDoesNotExist, err)

	// Update on an empty database should not success.
	node1 := types.NodeID{Hash: common.NewRandomHash()}
	block1 := types.Block{
		ProposerID: node1,
		Hash:       hash1,
		Position: types.Position{
			Height: 1,
		},
	}
	err = db.Update(block1)
	s.Equal(ErrBlockDoesNotExist, err)

	// Put to create a new record should just work fine.
	err = db.Put(block1)
	s.Nil(err)

	// Get it back should work fine.
	queried, err := db.Get(block1.Hash)
	s.Nil(err)
	s.Equal(queried.ProposerID, block1.ProposerID)

	// Test Update.
	now := time.Now().UTC()
	queried.Timestamp = now

	err = db.Update(queried)
	s.Nil(err)

	// Try to get it back via NodeID and height.
	queried, err = db.Get(block1.Hash)

	s.Nil(err)
	s.Equal(now, queried.Timestamp)
}

func (s *LevelDBTestSuite) TestSyncIndex() {
	dbName := fmt.Sprintf("test-db-%v-si.db", time.Now().UTC())
	db, err := NewLevelDBBackedBlockDB(dbName)
	s.Require().Nil(err)
	defer func(dbName string) {
		err = db.Close()
		s.Nil(err)
		err = os.RemoveAll(dbName)
		s.Nil(err)
	}(dbName)

	// Create some blocks.
	blocks := [10]types.Block{}
	for i := range blocks {
		block := types.Block{
			ProposerID: types.NodeID{Hash: common.NewRandomHash()},
			Hash:       common.NewRandomHash(),
			Position: types.Position{
				Height: uint64(i),
			},
		}
		db.Put(block)
		blocks[i] = block
	}

	// Save blocks to db.
	err = db.Close()
	s.Nil(err)

	// Load back blocks(syncIndex is called).
	db, err = NewLevelDBBackedBlockDB(dbName)
	s.Require().Nil(err)

	// Verify result.
	for _, block := range blocks {
		queried, err := db.Get(block.Hash)
		s.Nil(err)
		s.Equal(block.ProposerID, queried.ProposerID)
		s.Equal(block.Position.Height, queried.Position.Height)
	}
}

func TestLevelDB(t *testing.T) {
	suite.Run(t, new(LevelDBTestSuite))
}
