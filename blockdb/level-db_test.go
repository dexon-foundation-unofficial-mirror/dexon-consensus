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
	db, err := NewLevelDBBackendBlockDB(dbName)
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
	validator1 := types.ValidatorID{Hash: common.NewRandomHash()}
	block1 := types.Block{
		ProposerID: validator1,
		Hash:       hash1,
		Height:     1,
		State:      types.BlockStatusInit,
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
	s.Equal(queried.State, block1.State)

	// Test Update.
	now := time.Now().UTC()
	queried.Timestamps = map[types.ValidatorID]time.Time{
		queried.ProposerID: now,
	}

	err = db.Update(queried)
	s.Nil(err)

	// Try to get it back via ValidatorID and height.
	queried, err = db.GetByValidatorAndHeight(block1.ProposerID, block1.Height)

	s.Nil(err)
	s.Equal(now, queried.Timestamps[queried.ProposerID])
}

func (s *LevelDBTestSuite) TestSyncIndex() {
	dbName := fmt.Sprintf("test-db-%v-si.db", time.Now().UTC())
	db, err := NewLevelDBBackendBlockDB(dbName)
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
			ProposerID: types.ValidatorID{Hash: common.NewRandomHash()},
			Hash:       common.NewRandomHash(),
			Height:     uint64(i),
			State:      types.BlockStatusInit,
		}
		db.Put(block)
		blocks[i] = block
	}

	// Save blocks to db.
	err = db.Close()
	s.Nil(err)

	// Load back blocks(syncIndex is called).
	db, err = NewLevelDBBackendBlockDB(dbName)
	s.Require().Nil(err)

	// Verify result.
	for _, block := range blocks {
		queried, err := db.Get(block.Hash)
		s.Nil(err)
		s.Equal(block.ProposerID, queried.ProposerID)
		s.Equal(block.Height, queried.Height)
	}

	// Verify result using GetByValidatorAndHeight().
	for _, block := range blocks {
		queried, err := db.GetByValidatorAndHeight(block.ProposerID, block.Height)
		s.Nil(err)
		s.Equal(block.Hash, queried.Hash)
	}
}

func TestLevelDB(t *testing.T) {
	suite.Run(t, new(LevelDBTestSuite))
}
