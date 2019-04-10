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
	"bytes"
	"os"
	"testing"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/crypto/dkg"
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
	}
	s.b01 = &types.Block{
		ProposerID: s.v0,
		ParentHash: s.b00.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height: 1,
		},
	}
	s.b02 = &types.Block{
		ProposerID: s.v0,
		ParentHash: s.b01.Hash,
		Hash:       common.NewRandomHash(),
		Position: types.Position{
			Height: 2,
		},
	}
}

func (s *MemBackedDBTestSuite) TestSaveAndLoad() {
	// Make sure we are able to save/load from file.
	dbPath := "test-save-and-load.db"

	// Make sure the file pointed by 'dbPath' doesn't exist.
	_, err := os.Stat(dbPath)
	s.Require().Error(err)

	dbInst, err := NewMemBackedDB(dbPath)
	s.Require().NoError(err)
	s.Require().NotNil(dbInst)
	defer func() {
		if dbInst != nil {
			s.NoError(os.Remove(dbPath))
			dbInst = nil
		}
	}()

	s.NoError(dbInst.PutBlock(*s.b00))
	s.NoError(dbInst.PutBlock(*s.b01))
	s.NoError(dbInst.PutBlock(*s.b02))
	s.NoError(dbInst.Close())

	// Load the json file back to check if all inserted blocks
	// exists.
	dbInst, err = NewMemBackedDB(dbPath)
	s.Require().NoError(err)
	s.Require().NotNil(dbInst)
	s.True(dbInst.HasBlock(s.b00.Hash))
	s.True(dbInst.HasBlock(s.b01.Hash))
	s.True(dbInst.HasBlock(s.b02.Hash))
	s.NoError(dbInst.Close())
}

func (s *MemBackedDBTestSuite) TestIteration() {
	// Make sure the file pointed by 'dbPath' doesn't exist.
	dbInst, err := NewMemBackedDB()
	s.Require().NoError(err)
	s.Require().NotNil(dbInst)

	// Setup database.
	s.NoError(dbInst.PutBlock(*s.b00))
	s.NoError(dbInst.PutBlock(*s.b01))
	s.NoError(dbInst.PutBlock(*s.b02))

	// Check if we can iterate all 3 blocks.
	iter, err := dbInst.GetAllBlocks()
	s.Require().NoError(err)
	touched := common.Hashes{}
	for {
		b, err := iter.NextBlock()
		if err == ErrIterationFinished {
			break
		}
		s.Require().NoError(err)
		touched = append(touched, b.Hash)
	}
	s.Len(touched, 3)
	s.Contains(touched, s.b00.Hash)
	s.Contains(touched, s.b01.Hash)
	s.Contains(touched, s.b02.Hash)
}

func (s *MemBackedDBTestSuite) TestCompactionChainTipInfo() {
	dbInst, err := NewMemBackedDB()
	s.Require().NoError(err)
	s.Require().NotNil(dbInst)
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

func (s *MemBackedDBTestSuite) TestDKGPrivateKey() {
	dbInst, err := NewMemBackedDB()
	s.Require().NoError(err)
	s.Require().NotNil(dbInst)
	p := dkg.NewPrivateKey()
	// We should be unable to get it.
	_, err = dbInst.GetDKGPrivateKey(1, 0)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyDoesNotExist.Error())
	// Put it.
	s.Require().NoError(dbInst.PutDKGPrivateKey(1, 0, *p))
	// We should be unable to get it because reset is different.
	_, err = dbInst.GetDKGPrivateKey(1, 1)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyDoesNotExist.Error())
	// Put it again, should not success.
	err = dbInst.PutDKGPrivateKey(1, 0, *p)
	s.Require().Equal(err.Error(), ErrDKGPrivateKeyExists.Error())
	// Get it back.
	tmpPrv, err := dbInst.GetDKGPrivateKey(1, 0)
	s.Require().NoError(err)
	s.Require().Equal(bytes.Compare(p.Bytes(), tmpPrv.Bytes()), 0)
	// Put it at different reset.
	p2 := dkg.NewPrivateKey()
	s.Require().NoError(dbInst.PutDKGPrivateKey(1, 1, *p2))
	// We should be unable to get it because reset is different.
	_, err = dbInst.GetDKGPrivateKey(1, 0)
	// Get it back.
	tmpPrv, err = dbInst.GetDKGPrivateKey(1, 1)
	s.Require().NoError(err)
	s.Require().Equal(bytes.Compare(p2.Bytes(), tmpPrv.Bytes()), 0)
	s.Require().NotEqual(bytes.Compare(p2.Bytes(), p.Bytes()), 0)
}

func TestMemBackedDB(t *testing.T) {
	suite.Run(t, new(MemBackedDBTestSuite))
}
