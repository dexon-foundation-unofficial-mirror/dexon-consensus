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

package db

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"os"

	"github.com/stretchr/testify/suite"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

type LevelDBTestSuite struct {
	suite.Suite
}

func (s *LevelDBTestSuite) TestBasicUsage() {
	dbName := fmt.Sprintf("test-db-%v.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)

	// Queried something from an empty database.
	hash1 := common.NewRandomHash()
	_, err = dbInst.GetBlock(hash1)
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
	err = dbInst.UpdateBlock(block1)
	s.Equal(ErrBlockDoesNotExist, err)

	// Put to create a new record should just work fine.
	err = dbInst.PutBlock(block1)
	s.NoError(err)

	// Get it back should work fine.
	queried, err := dbInst.GetBlock(block1.Hash)
	s.NoError(err)
	s.Equal(queried.ProposerID, block1.ProposerID)

	// Test Update.
	now := time.Now().UTC()
	queried.Timestamp = now

	err = dbInst.UpdateBlock(queried)
	s.NoError(err)

	// Try to get it back via NodeID and height.
	queried, err = dbInst.GetBlock(block1.Hash)

	s.NoError(err)
	s.Equal(now, queried.Timestamp)
}

func (s *LevelDBTestSuite) TestSyncIndex() {
	dbName := fmt.Sprintf("test-db-%v-si.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
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
		dbInst.PutBlock(block)
		blocks[i] = block
	}

	// Save blocks to db.
	err = dbInst.Close()
	s.NoError(err)

	// Load back blocks(syncIndex is called).
	dbInst, err = NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)

	// Verify result.
	for _, block := range blocks {
		queried, err := dbInst.GetBlock(block.Hash)
		s.NoError(err)
		s.Equal(block.ProposerID, queried.ProposerID)
		s.Equal(block.Position.Height, queried.Position.Height)
	}
}

func (s *LevelDBTestSuite) TestCompactionChainTipInfo() {
	dbName := fmt.Sprintf("test-db-%v-cc-tip.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)
	// Save some tip info.
	hash := common.NewRandomHash()
	s.Require().NoError(dbInst.PutCompactionChainTipInfo(hash, 1))
	// Get it back to check.
	hashBack, height := dbInst.GetCompactionChainTipInfo()
	s.Require().Equal(hash, hashBack)
	s.Require().Equal(height, uint64(1))
	// Unable to put compaction chain tip info with lower height.
	err = dbInst.PutCompactionChainTipInfo(hash, 0)
	s.Require().Equal(err.Error(), ErrInvalidCompactionChainTipHeight.Error())
	// Unable to put compaction chain tip info with height not incremental by 1.
	err = dbInst.PutCompactionChainTipInfo(hash, 3)
	s.Require().Equal(err.Error(), ErrInvalidCompactionChainTipHeight.Error())
	// It's OK to put compaction chain tip info with height incremental by 1.
	s.Require().NoError(dbInst.PutCompactionChainTipInfo(hash, 2))
}

func (s *LevelDBTestSuite) TestDKGPrivateKey() {
	dbName := fmt.Sprintf("test-db-%v-dkg-prv.db", time.Now().UTC())
	dbInst, err := NewLevelDBBackedDB(dbName)
	s.Require().NoError(err)
	defer func(dbName string) {
		err = dbInst.Close()
		s.NoError(err)
		err = os.RemoveAll(dbName)
		s.NoError(err)
	}(dbName)
	p := dkg.NewPrivateKey()
	// Check existence.
	exists, err := dbInst.HasDKGPrivateKey(1)
	s.Require().NoError(err)
	s.Require().False(exists)
	// We should be unable to get it, too.
	_, err = dbInst.GetDKGPrivateKey(1)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyDoesNotExist.Error())
	// Put it.
	s.Require().NoError(dbInst.PutDKGPrivateKey(1, *p))
	// Put it again, should not success.
	err = dbInst.PutDKGPrivateKey(1, *p)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyExists.Error())
	// Get it back.
	tmpPrv, err := dbInst.GetDKGPrivateKey(1)
	s.Require().NoError(err)
	s.Require().Equal(bytes.Compare(p.Bytes(), tmpPrv.Bytes()), 0)
}

func TestLevelDB(t *testing.T) {
	suite.Run(t, new(LevelDBTestSuite))
}
