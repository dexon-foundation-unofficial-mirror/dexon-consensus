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

package blockdb

import (
	"encoding/json"

	"github.com/syndtr/goleveldb/leveldb"

	"github.com/dexon-foundation/dexon-consensus/common"
	"github.com/dexon-foundation/dexon-consensus/core/types"
)

// LevelDBBackedBlockDB is a leveldb backed BlockDB implementation.
type LevelDBBackedBlockDB struct {
	db *leveldb.DB
}

// NewLevelDBBackedBlockDB initialize a leveldb-backed block database.
func NewLevelDBBackedBlockDB(
	path string) (lvl *LevelDBBackedBlockDB, err error) {

	db, err := leveldb.OpenFile(path, nil)
	if err != nil {
		return
	}
	lvl = &LevelDBBackedBlockDB{db: db}
	return
}

// Close implement Closer interface, which would release allocated resource.
func (lvl *LevelDBBackedBlockDB) Close() error {
	return lvl.db.Close()
}

// Has implements the Reader.Has method.
func (lvl *LevelDBBackedBlockDB) Has(hash common.Hash) bool {
	exists, err := lvl.db.Has([]byte(hash[:]), nil)
	if err != nil {
		// TODO(missionliao): Modify the interface to return error.
		panic(err)
	}
	return exists
}

// Get implements the Reader.Get method.
func (lvl *LevelDBBackedBlockDB) Get(
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

// Update implements the Writer.Update method.
func (lvl *LevelDBBackedBlockDB) Update(block types.Block) (err error) {
	// NOTE: we didn't handle changes of block hash (and it
	//       should not happen).
	marshaled, err := json.Marshal(&block)
	if err != nil {
		return
	}

	if !lvl.Has(block.Hash) {
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

// Put implements the Writer.Put method.
func (lvl *LevelDBBackedBlockDB) Put(block types.Block) (err error) {
	marshaled, err := json.Marshal(&block)
	if err != nil {
		return
	}
	if lvl.Has(block.Hash) {
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

// GetAll implements Reader.GetAll method, which allows callers
// to retrieve all blocks in DB.
func (lvl *LevelDBBackedBlockDB) GetAll() (BlockIterator, error) {
	// TODO (mission): Implement this part via goleveldb's iterator.
	return nil, ErrNotImplemented
}
