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
	"encoding/json"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// LevelDBBackedDB is a leveldb backed DB implementation.
type LevelDBBackedDB struct {
	db *leveldb.DB
}

// NewLevelDBBackedDB initialize a leveldb-backed database.
func NewLevelDBBackedDB(
	path string) (lvl *LevelDBBackedDB, err error) {

	dbInst, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return
	}
	lvl = &LevelDBBackedDB{db: dbInst}
	return
}

// Close implement Closer interface, which would release allocated resource.
func (lvl *LevelDBBackedDB) Close() error {
	return lvl.db.Close()
}

// HasBlock implements the Reader.Has method.
func (lvl *LevelDBBackedDB) HasBlock(hash common.Hash) bool {
	exists, err := lvl.db.Has([]byte(hash[:]), nil)
	if err != nil {
		// TODO(missionliao): Modify the interface to return error.
		panic(err)
	}
	return exists
}

// GetBlock implements the Reader.GetBlock method.
func (lvl *LevelDBBackedDB) GetBlock(
	hash common.Hash) (block types.Block, err error) {

	queried, err := lvl.db.Get([]byte(hash[:]), nil)
	if err != nil {
		if err == leveldb.ErrNotFound {
			err = ErrBlockDoesNotExist
		}
		return
	}
	err = json.Unmarshal(queried, &block)
	if err != nil {
		return
	}
	return
}

// UpdateBlock implements the Writer.UpdateBlock method.
func (lvl *LevelDBBackedDB) UpdateBlock(block types.Block) (err error) {
	// NOTE: we didn't handle changes of block hash (and it
	//       should not happen).
	marshaled, err := json.Marshal(&block)
	if err != nil {
		return
	}

	if !lvl.HasBlock(block.Hash) {
		err = ErrBlockDoesNotExist
		return
	}
	err = lvl.db.Put(
		[]byte(block.Hash[:]),
		marshaled,
		nil)
	if err != nil {
		return
	}
	return
}

// PutBlock implements the Writer.PutBlock method.
func (lvl *LevelDBBackedDB) PutBlock(block types.Block) (err error) {
	marshaled, err := json.Marshal(&block)
	if err != nil {
		return
	}
	if lvl.HasBlock(block.Hash) {
		err = ErrBlockExists
		return
	}
	err = lvl.db.Put(
		[]byte(block.Hash[:]),
		marshaled,
		nil)
	if err != nil {
		return
	}
	return
}

// GetAllBlocks implements Reader.GetAllBlocks method, which allows callers
// to retrieve all blocks in DB.
func (lvl *LevelDBBackedDB) GetAllBlocks() (BlockIterator, error) {
	// TODO (mission): Implement this part via goleveldb's iterator.
	return nil, ErrNotImplemented
}
